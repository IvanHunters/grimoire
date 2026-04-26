import { createContext, useContext, useState, useCallback } from 'react'
import type { ReactNode } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import type {
  ClaudeSession,
  Message,
  ConnectionStatus,
  WSMessage,
} from '../types/claude'
import type { RealtimeEvent } from '../types/events'

interface ClaudeContextValue {
  sessions: ClaudeSession[]
  currentSessionId: string | null
  currentSession: ClaudeSession | null
  connectionStatus: ConnectionStatus

  // Session management
  createSession: (name: string, workingDir: string, dangerousMode: boolean) => void
  switchSession: (sessionId: string) => void
  closeSession: (sessionId: string) => void
  restartSession: (sessionId: string) => void

  // Messaging
  sendMessage: (content: string, currentNote?: { name: string; content: string; type?: string; projectPath?: string }) => void
  stopGeneration: () => void

  // Real-time events
  onRealtimeEvent?: (event: RealtimeEvent) => void
}

const ClaudeContext = createContext<ClaudeContextValue | undefined>(undefined)

const WS_URL = import.meta.env.VITE_WS_URL || 'ws://localhost:3000/claude-chat'

interface ClaudeProviderProps {
  children: ReactNode
  onRealtimeEvent?: (event: RealtimeEvent) => void
}

export function ClaudeProvider({ children, onRealtimeEvent }: ClaudeProviderProps) {
  const [sessions, setSessions] = useState<ClaudeSession[]>([])
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null)

  const currentSession = sessions.find((s) => s.id === currentSessionId) || null

  const handleMessage = useCallback((message: WSMessage) => {
    switch (message.type) {
      case 'session_started':
        // Session initialized successfully
        console.log('Session started:', message.sessionId)
        break

      case 'message_start':
        // Claude started generating response
        if (currentSessionId && message.sessionId === currentSessionId) {
          setSessions((prev) =>
            prev.map((s) =>
              s.id === currentSessionId
                ? { ...s, isActive: true }
                : s
            )
          )
        }
        break

      case 'content_delta':
        // Streaming content chunk
        if (currentSessionId && message.sessionId === currentSessionId && message.content) {
          setSessions((prev) =>
            prev.map((s) => {
              if (s.id !== currentSessionId) return s

              const lastMessage = s.messages[s.messages.length - 1]
              if (lastMessage && lastMessage.role === 'assistant') {
                // Append to existing assistant message
                return {
                  ...s,
                  messages: [
                    ...s.messages.slice(0, -1),
                    {
                      ...lastMessage,
                      content: lastMessage.content + (message.content || ''),
                    },
                  ],
                  lastActivity: new Date(),
                }
              } else {
                // Create new assistant message
                return {
                  ...s,
                  messages: [
                    ...s.messages,
                    {
                      id: crypto.randomUUID(),
                      role: 'assistant' as const,
                      content: message.content || '',
                      timestamp: new Date(),
                    },
                  ],
                  lastActivity: new Date(),
                }
              }
            })
          )
        }
        break

      case 'tool_use':
        // Claude is using a tool
        if (currentSessionId && message.sessionId === currentSessionId) {
          setSessions((prev) =>
            prev.map((s) => {
              if (s.id !== currentSessionId) return s

              const lastMessage = s.messages[s.messages.length - 1]
              if (lastMessage && lastMessage.role === 'assistant') {
                // Update last message with tool use info
                return {
                  ...s,
                  messages: [
                    ...s.messages.slice(0, -1),
                    {
                      ...lastMessage,
                      toolUse: `🔧 ${message.tool_name}: ${message.tool_args}`,
                    },
                  ],
                }
              }
              return s
            })
          )
        }
        break

      case 'message_complete':
        // Claude finished generating response
        if (currentSessionId && message.sessionId === currentSessionId) {
          setSessions((prev) =>
            prev.map((s) =>
              s.id === currentSessionId
                ? { ...s, isActive: false, lastActivity: new Date() }
                : s
            )
          )
        }
        break

      case 'error':
        // Error occurred
        console.error('Claude error:', message.error)
        if (currentSessionId && message.sessionId === currentSessionId) {
          setSessions((prev) =>
            prev.map((s) => {
              if (s.id !== currentSessionId) return s
              return {
                ...s,
                messages: [
                  ...s.messages,
                  {
                    id: crypto.randomUUID(),
                    role: 'assistant' as const,
                    content: `❌ Error: ${message.error}`,
                    timestamp: new Date(),
                  },
                ],
                isActive: false,
              }
            })
          )
        }
        break

      case 'stopped':
        // Generation stopped by user
        if (currentSessionId && message.sessionId === currentSessionId) {
          setSessions((prev) =>
            prev.map((s) =>
              s.id === currentSessionId
                ? { ...s, isActive: false }
                : s
            )
          )
        }
        break

      case 'session_history':
        // Received session history (when switching to existing session)
        if (message.sessionId && message.messages) {
          setSessions((prev) => {
            const exists = prev.find((s) => s.id === message.sessionId)
            if (exists) {
              return prev.map((s) =>
                s.id === message.sessionId
                  ? { ...s, messages: message.messages as Message[] }
                  : s
              )
            }
            return prev
          })
        }
        break
    }
  }, [currentSessionId])

  const { connectionStatus, sendMessage: wsSendMessage } = useWebSocket({
    url: WS_URL,
    onMessage: handleMessage,
    onRealtimeEvent,
  })

  const createSession = useCallback(
    (name: string, workingDir: string, dangerousMode: boolean) => {
      const sessionId = crypto.randomUUID()

      const newSession: ClaudeSession = {
        id: sessionId,
        name,
        workingDir,
        dangerousMode,
        messages: [],
        isActive: false,
        lastActivity: new Date(),
      }

      setSessions((prev) => [...prev, newSession])
      setCurrentSessionId(sessionId)

      // Send init message to backend
      wsSendMessage({
        type: 'init',
        sessionId,
        dangerousMode,
      })
    },
    [wsSendMessage]
  )

  const switchSession = useCallback(
    (sessionId: string) => {
      setCurrentSessionId(sessionId)

      // Request session history from backend
      wsSendMessage({
        type: 'switch_session',
        sessionId,
      })
    },
    [wsSendMessage]
  )

  const closeSession = useCallback(
    (sessionId: string) => {
      wsSendMessage({
        type: 'close_session',
        sessionId,
      })

      setSessions((prev) => prev.filter((s) => s.id !== sessionId))

      if (currentSessionId === sessionId) {
        setCurrentSessionId(null)
      }
    },
    [wsSendMessage, currentSessionId]
  )

  const restartSession = useCallback(
    (sessionId: string) => {
      wsSendMessage({
        type: 'restart_session',
        sessionId,
      })

      setSessions((prev) =>
        prev.map((s) =>
          s.id === sessionId
            ? { ...s, messages: [], isActive: false }
            : s
        )
      )
    },
    [wsSendMessage]
  )

  const sendMessage = useCallback(
    (content: string, currentNote?: { name: string; content: string; type?: string; projectPath?: string }) => {
      if (!currentSessionId) return

      // Add user message to session
      const userMessage: Message = {
        id: crypto.randomUUID(),
        role: 'user',
        content,
        timestamp: new Date(),
      }

      setSessions((prev) =>
        prev.map((s) =>
          s.id === currentSessionId
            ? { ...s, messages: [...s.messages, userMessage], isActive: true }
            : s
        )
      )

      // Send to backend
      wsSendMessage({
        type: 'message',
        sessionId: currentSessionId,
        content,
        currentNote,
      })
    },
    [currentSessionId, wsSendMessage]
  )

  const stopGeneration = useCallback(() => {
    if (!currentSessionId) return

    wsSendMessage({
      type: 'stop',
      sessionId: currentSessionId,
    })
  }, [currentSessionId, wsSendMessage])

  const value: ClaudeContextValue = {
    sessions,
    currentSessionId,
    currentSession,
    connectionStatus,
    createSession,
    switchSession,
    closeSession,
    restartSession,
    sendMessage,
    stopGeneration,
    onRealtimeEvent,
  }

  return <ClaudeContext.Provider value={value}>{children}</ClaudeContext.Provider>
}

export function useClaude() {
  const context = useContext(ClaudeContext)
  if (context === undefined) {
    throw new Error('useClaude must be used within a ClaudeProvider')
  }
  return context
}
