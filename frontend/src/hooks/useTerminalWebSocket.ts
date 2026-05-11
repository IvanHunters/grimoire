import { useEffect, useRef, useCallback } from 'react'
import type { Note } from '../types/note'

const WS_URL = import.meta.env.VITE_WS_URL ||
  `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/claude-chat`

export interface TaskContextPayload {
  id: string
  title: string
  status: string
  priority: string
  description?: string
  folderPath?: string
  projectPath?: string
}

interface UseTerminalWebSocketProps {
  sessionId: string
  dangerousMode?: boolean
  currentNote?: Note | null
  taskContext?: TaskContextPayload | null
  onOutput: (data: Uint8Array) => void
}

export function useTerminalWebSocket({ sessionId, dangerousMode = true, currentNote, taskContext, onOutput }: UseTerminalWebSocketProps) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const onOutputRef = useRef(onOutput)
  const isConnectingRef = useRef(false)
  const intentionalCloseRef = useRef(false)
  const currentNoteRef = useRef(currentNote)
  const taskContextRef = useRef(taskContext)
  const sendResizeRef = useRef<((cols: number, rows: number) => void) | null>(null)

  // Keep onOutput callback ref up to date
  useEffect(() => {
    onOutputRef.current = onOutput
  }, [onOutput])

  // Keep currentNote ref up to date
  useEffect(() => {
    currentNoteRef.current = currentNote
  }, [currentNote])

  // Keep taskContext ref up to date
  useEffect(() => {
    taskContextRef.current = taskContext
  }, [taskContext])

  // Connect to WebSocket
  useEffect(() => {
    // Prevent multiple simultaneous connections
    if (isConnectingRef.current) {
      return
    }

    const connect = () => {
      // Prevent reconnect if already connecting
      if (isConnectingRef.current) {
        return
      }
      isConnectingRef.current = true

      const ws = new WebSocket(WS_URL)

      ws.onopen = () => {
        isConnectingRef.current = false

        // Send init message with currentNote or taskContext from refs
        const note = currentNoteRef.current
        const tc = taskContextRef.current
        ws.send(JSON.stringify({
          type: 'init',
          sessionId,
          dangerousMode,
          taskContext: tc ?? null,
          currentNote: note ? {
            name: note.title,
            folder: note.folder || '',
            content: note.content,
            type: note.type || '',
            projectPath: note.projectPath || '',
          } : null,
        }))
      }

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data)

          if (message.type === 'terminal_output' && message.content) {
            // Decode base64-encoded PTY bytes into Uint8Array.
            // The backend sends raw PTY bytes as base64 to preserve all byte values
            // (including ANSI escape sequences) through the JSON transport layer.
            const binaryStr = atob(message.content)
            const bytes = new Uint8Array(binaryStr.length)
            for (let i = 0; i < binaryStr.length; i++) {
              bytes[i] = binaryStr.charCodeAt(i)
            }
            onOutputRef.current(bytes)
          }
        } catch (error) {
          console.error('Failed to parse WebSocket message:', error)
        }
      }

      ws.onerror = (error) => {
        console.error('Terminal WebSocket error:', error)
        isConnectingRef.current = false
      }

      ws.onclose = () => {
        wsRef.current = null
        isConnectingRef.current = false

        // Only reconnect if not intentionally closed
        if (!intentionalCloseRef.current) {
          reconnectTimeoutRef.current = window.setTimeout(() => {
            connect()
          }, 1000)
        }
      }

      wsRef.current = ws
    }

    // Reset intentional close flag
    intentionalCloseRef.current = false
    connect()

    return () => {
      // Mark as intentional close
      intentionalCloseRef.current = true
      isConnectingRef.current = false

      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
        reconnectTimeoutRef.current = null
      }

      if (wsRef.current) {
        // Remove event handlers before closing to prevent race conditions
        const ws = wsRef.current
        ws.onopen = null
        ws.onmessage = null
        ws.onerror = null
        ws.onclose = null
        ws.close()
        wsRef.current = null
      }
    }
  }, [sessionId, dangerousMode]) // Removed currentNote from dependencies

  // Send input to terminal
  const sendInput = useCallback((data: string) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'terminal_input',
        sessionId,
        content: data,
      }))
    } else {
      console.warn(`[WS] Cannot send input - WebSocket not ready (state: ${wsRef.current?.readyState})`)
    }
  }, [sessionId])

  // Send restart message to backend
  const sendRestart = useCallback(() => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      const note = currentNoteRef.current
      wsRef.current.send(JSON.stringify({
        type: 'restart_session',
        sessionId,
        dangerousMode,
        currentNote: note ? {
          name: note.title,
          folder: note.folder || '',
          content: note.content,
          type: note.type || '',
          projectPath: note.projectPath || '',
        } : null,
      }))
    }
  }, [sessionId, dangerousMode])

  // Send PTY resize signal to backend so the process knows the actual terminal dimensions
  const sendResize = useCallback((cols: number, rows: number) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'terminal_resize',
        sessionId,
        cols,
        rows,
      }))
    }
  }, [sessionId])

  // Keep sendResize accessible via ref (used in TerminalChat before ws is ready)
  useEffect(() => {
    sendResizeRef.current = sendResize
  }, [sendResize])

  return { sendInput, sendRestart, sendResize }
}
