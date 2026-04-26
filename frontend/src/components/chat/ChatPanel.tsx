import { useState, useRef, useEffect } from 'react'
import { X, Send, User, Bot, AlertCircle, Plus, Trash2, Settings, FileText, Circle } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'
import { useClaude } from '../../contexts/ClaudeContext'

interface ChatPanelProps {
  visible: boolean
  onClose: () => void
  noteId?: string | null // Note ID for chat context
}

/**
 * ChatPanel - AI assistant chat interface integrated with Claude WebSocket
 *
 * Features:
 * - Real WebSocket connection to Claude backend
 * - Multiple sessions support
 * - Dangerous mode toggle
 * - Tool use visualization
 * - Session management (create, switch, delete, restart)
 * - Note context support (sends current note content to Claude)
 * - Auto-refresh on real-time events
 */
function ChatPanel({ visible, onClose, noteId }: ChatPanelProps) {
  const { notes } = useNotes()
  const {
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
  } = useClaude()

  const [input, setInput] = useState('')
  const [showSessionManager, setShowSessionManager] = useState(false)
  const [newSessionName, setNewSessionName] = useState('')
  const [newSessionDangerousMode, setNewSessionDangerousMode] = useState(false)
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Get current note (if noteId provided)
  const currentNote = noteId ? notes.find(n => n.id === noteId) : null

  // Create initial session if none exists
  useEffect(() => {
    if (sessions.length === 0) {
      createSession('Main Session', '~', false)
    }
  }, [sessions.length, createSession])

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [currentSession?.messages])

  // Focus input when panel opens
  useEffect(() => {
    if (visible && inputRef.current) {
      inputRef.current.focus()
    }
  }, [visible])

  const handleSendMessage = async () => {
    const text = input.trim()
    if (!text || connectionStatus === 'generating') return

    setInput('')

    // Prepare current note context if available
    const noteContext = currentNote
      ? {
          name: currentNote.title,
          content: currentNote.content,
          type: currentNote.type,
          projectPath: currentNote.projectPath,
        }
      : undefined

    // Send message via ClaudeContext
    sendMessage(text, noteContext)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSendMessage()
    }
  }

  const handleCreateSession = () => {
    const name = newSessionName.trim() || `Session ${sessions.length + 1}`
    const workingDir = currentNote?.projectPath || '~'

    createSession(name, workingDir, newSessionDangerousMode)

    setNewSessionName('')
    setNewSessionDangerousMode(false)
    setShowSessionManager(false)
  }

  const handleDeleteSession = (sessionId: string) => {
    if (sessions.length <= 1) return // Need at least one session
    closeSession(sessionId)
  }

  const handleStopGeneration = () => {
    stopGeneration()
  }

  const getConnectionStatusColor = () => {
    switch (connectionStatus) {
      case 'ready':
        return 'text-green-500'
      case 'connecting':
        return 'text-yellow-500'
      case 'generating':
        return 'text-blue-500 animate-pulse'
      case 'error':
        return 'text-red-500'
      case 'disconnected':
        return 'text-gray-400'
      default:
        return 'text-gray-400'
    }
  }

  const getConnectionStatusText = () => {
    switch (connectionStatus) {
      case 'ready':
        return 'Ready'
      case 'connecting':
        return 'Connecting...'
      case 'generating':
        return 'Generating...'
      case 'error':
        return 'Connection Error'
      case 'disconnected':
        return 'Disconnected'
      default:
        return 'Unknown'
    }
  }

  if (!visible) return null

  const messages = currentSession?.messages || []

  return (
    <div className="fixed right-0 top-14 bottom-0 w-96 bg-white border-l border-gray-200 flex flex-col shadow-lg z-10">
      {/* Header */}
      <div className="h-14 border-b border-gray-200 flex items-center justify-between px-4 bg-gradient-to-r from-purple-50 to-blue-50">
        <div className="flex items-center gap-2">
          <Bot className="w-5 h-5 text-purple-600" />
          <span className="font-semibold text-gray-900">Claude Assistant</span>
        </div>
        <div className="flex items-center gap-2">
          {/* Connection Status */}
          <div className="flex items-center gap-1.5 text-xs">
            <Circle className={`w-2 h-2 fill-current ${getConnectionStatusColor()}`} />
            <span className="text-gray-600">{getConnectionStatusText()}</span>
          </div>

          {/* Session Manager Toggle */}
          <button
            onClick={() => setShowSessionManager(!showSessionManager)}
            className="p-1.5 hover:bg-white/60 rounded"
            title="Session Manager"
          >
            <Settings className="w-4 h-4 text-gray-600" />
          </button>

          {/* Close Button */}
          <button
            onClick={onClose}
            className="p-1.5 hover:bg-white/60 rounded"
            title="Close Chat"
          >
            <X className="w-4 h-4 text-gray-600" />
          </button>
        </div>
      </div>

      {/* Session Manager (collapsible) */}
      {showSessionManager && (
        <div className="border-b border-gray-200 bg-gray-50 p-3">
          <div className="text-xs font-semibold text-gray-600 mb-2">SESSIONS</div>

          {/* Session List */}
          <div className="space-y-1 mb-3 max-h-32 overflow-y-auto">
            {sessions.map((session) => (
              <div
                key={session.id}
                className={`flex items-center justify-between p-2 rounded text-sm cursor-pointer ${
                  session.id === currentSessionId
                    ? 'bg-purple-100 text-purple-900'
                    : 'hover:bg-gray-200'
                }`}
                onClick={() => switchSession(session.id)}
              >
                <div className="flex items-center gap-2 flex-1 overflow-hidden">
                  <FileText className="w-3.5 h-3.5 flex-shrink-0" />
                  <span className="truncate">{session.name}</span>
                  {session.dangerousMode && (
                    <span title="Dangerous Mode">
                      <AlertCircle className="w-3.5 h-3.5 text-orange-500 flex-shrink-0" />
                    </span>
                  )}
                </div>
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    handleDeleteSession(session.id)
                  }}
                  className="p-1 hover:bg-red-100 rounded opacity-0 group-hover:opacity-100"
                  title="Delete Session"
                >
                  <Trash2 className="w-3 h-3 text-red-600" />
                </button>
              </div>
            ))}
          </div>

          {/* Create New Session */}
          <div className="space-y-2">
            <input
              type="text"
              placeholder="New session name (optional)"
              value={newSessionName}
              onChange={(e) => setNewSessionName(e.target.value)}
              className="w-full px-2 py-1.5 text-sm border border-gray-300 rounded focus:outline-none focus:ring-1 focus:ring-purple-500"
            />
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={newSessionDangerousMode}
                onChange={(e) => setNewSessionDangerousMode(e.target.checked)}
                className="w-4 h-4"
              />
              <span className="text-gray-700">Dangerous Mode</span>
              <span title="Allows destructive operations">
                <AlertCircle className="w-3.5 h-3.5 text-orange-500" />
              </span>
            </label>
            <button
              onClick={handleCreateSession}
              className="w-full flex items-center justify-center gap-2 px-3 py-1.5 bg-purple-600 text-white text-sm rounded hover:bg-purple-700"
            >
              <Plus className="w-4 h-4" />
              Create Session
            </button>
          </div>

          {/* Current Session Actions */}
          {currentSession && (
            <div className="mt-3 pt-3 border-t border-gray-300">
              <button
                onClick={() => currentSessionId && restartSession(currentSessionId)}
                className="w-full px-3 py-1.5 bg-gray-200 text-gray-700 text-sm rounded hover:bg-gray-300"
              >
                Restart Current Session
              </button>
            </div>
          )}
        </div>
      )}

      {/* Current Note Context Banner */}
      {currentNote && (
        <div className="border-b border-gray-200 bg-blue-50 px-4 py-2 text-sm">
          <div className="flex items-center gap-2">
            <FileText className="w-4 h-4 text-blue-600" />
            <span className="text-gray-700">
              Context: <span className="font-semibold">{currentNote.title}</span>
            </span>
            {currentNote.type === 'project' && (
              <span className="text-xs px-2 py-0.5 bg-purple-100 text-purple-700 rounded">PROJECT</span>
            )}
          </div>
        </div>
      )}

      {/* Messages Area */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.map((message) => (
          <div
            key={message.id}
            className={`flex gap-3 ${
              message.role === 'user' ? 'justify-end' : 'justify-start'
            }`}
          >
            {message.role !== 'user' && (
              <div className="flex-shrink-0 w-8 h-8 rounded-full bg-gradient-to-br from-purple-500 to-blue-500 flex items-center justify-center">
                {message.role === 'system' ? (
                  <AlertCircle className="w-5 h-5 text-white" />
                ) : (
                  <Bot className="w-5 h-5 text-white" />
                )}
              </div>
            )}

            <div
              className={`max-w-[75%] rounded-2xl px-4 py-2 ${
                message.role === 'user'
                  ? 'bg-purple-600 text-white'
                  : message.role === 'system'
                  ? 'bg-orange-50 text-orange-900 border border-orange-200'
                  : 'bg-gray-100 text-gray-900'
              }`}
            >
              {message.toolUse && (
                <div className="text-xs mb-2 opacity-75 font-mono">
                  🔧 {message.toolUse}
                </div>
              )}
              <div className="text-sm whitespace-pre-wrap break-words">
                {message.content}
              </div>
              <div className="text-xs opacity-60 mt-1">
                {message.timestamp.toLocaleTimeString()}
              </div>
            </div>

            {message.role === 'user' && (
              <div className="flex-shrink-0 w-8 h-8 rounded-full bg-gradient-to-br from-green-500 to-teal-500 flex items-center justify-center">
                <User className="w-5 h-5 text-white" />
              </div>
            )}
          </div>
        ))}

        {/* Auto-scroll anchor */}
        <div ref={messagesEndRef} />
      </div>

      {/* Input Area */}
      <div className="border-t border-gray-200 p-4 bg-gray-50">
        <div className="flex gap-2">
          <input
            ref={inputRef}
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={
              connectionStatus === 'generating'
                ? 'Generating...'
                : currentNote
                ? `Ask about ${currentNote.title}...`
                : 'Ask Claude anything...'
            }
            disabled={connectionStatus === 'generating' || connectionStatus === 'disconnected'}
            className="flex-1 px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-purple-500 disabled:bg-gray-200 disabled:cursor-not-allowed"
          />

          {connectionStatus === 'generating' ? (
            <button
              onClick={handleStopGeneration}
              className="px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 flex items-center gap-2"
              title="Stop Generation"
            >
              <X className="w-5 h-5" />
            </button>
          ) : (
            <button
              onClick={handleSendMessage}
              disabled={!input.trim() || connectionStatus === 'disconnected'}
              className="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:bg-gray-300 disabled:cursor-not-allowed flex items-center gap-2"
              title="Send Message (Enter)"
            >
              <Send className="w-5 h-5" />
            </button>
          )}
        </div>

        {/* Dangerous Mode Warning */}
        {currentSession?.dangerousMode && (
          <div className="mt-2 text-xs text-orange-600 flex items-center gap-1">
            <AlertCircle className="w-3.5 h-3.5" />
            <span>Dangerous mode enabled - destructive operations allowed</span>
          </div>
        )}
      </div>
    </div>
  )
}

export default ChatPanel
