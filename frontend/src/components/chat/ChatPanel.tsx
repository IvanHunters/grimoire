import { useMemo, useState, useCallback, useEffect } from 'react'
import { X, Bot, RotateCcw, Trash2, Plus, Terminal } from 'lucide-react'
import { TerminalChat } from './TerminalChat'
import { sessionsAPI } from '../../api/sessions'
import type { ClaudeSession } from '../../types/claude'

interface ChatPanelProps {
  visible: boolean
  onClose: () => void
  noteId?: string | null
}

/**
 * ChatPanel - Terminal emulator for Claude Code CLI
 *
 * Features:
 * - Full xterm.js terminal emulator
 * - Raw PTY streaming via WebSocket
 * - Complete TUI interaction (like KVM)
 * - Session per note or global session
 * - List of active sessions with ability to switch
 * - Dangerous mode enabled by default
 */
function ChatPanel({ visible, onClose, noteId }: ChatPanelProps) {
  const [sessionKey, setSessionKey] = useState(0)
  const [activeSessions, setActiveSessions] = useState<ClaudeSession[]>([])
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)

  // Generate session ID based on context:
  // - If noteId provided → session per note
  // - Otherwise → use selected session or create new global session
  const sessionId = useMemo(() => {
    if (noteId) {
      return `note-${noteId}`
    }
    if (selectedSessionId) {
      return selectedSessionId
    }
    // Global session - generate UUID once and keep it
    const globalId = sessionStorage.getItem('claude-global-session')
    if (globalId) {
      return globalId
    }
    const newId = `global-${crypto.randomUUID()}`
    sessionStorage.setItem('claude-global-session', newId)
    return newId
  }, [noteId, selectedSessionId])

  // Load active sessions
  const loadSessions = useCallback(async () => {
    try {
      const sessions = await sessionsAPI.listActiveSessions()
      setActiveSessions(sessions)

      // If no session selected and we have sessions, select the first one
      if (!selectedSessionId && sessions.length > 0 && !noteId) {
        setSelectedSessionId(sessions[0].id)
      }
    } catch (error) {
      console.error('Failed to load sessions:', error)
    }
  }, [selectedSessionId, noteId])

  // Load sessions on mount and when panel becomes visible
  useEffect(() => {
    if (visible) {
      loadSessions()
      // Refresh sessions list every 5 seconds
      const interval = window.setInterval(loadSessions, 5000)
      return () => clearInterval(interval)
    }
  }, [visible, loadSessions])

  // Restart session - force remount TerminalChat with new key
  const handleRestart = useCallback(() => {
    setSessionKey(prev => prev + 1)
  }, [])

  // Kill session - close WebSocket and clear from backend
  const handleKill = useCallback(() => {
    // Force remount to close WebSocket
    setSessionKey(prev => prev + 1)
    // If global session, clear from sessionStorage
    if (!noteId && sessionId === sessionStorage.getItem('claude-global-session')) {
      sessionStorage.removeItem('claude-global-session')
      setSelectedSessionId(null)
    }
    // Reload sessions after a delay
    window.setTimeout(loadSessions, 1000)
  }, [noteId, sessionId, loadSessions])

  // Create new session
  const handleNewSession = useCallback(() => {
    const newId = `global-${crypto.randomUUID()}`
    sessionStorage.setItem('claude-global-session', newId)
    setSelectedSessionId(newId)
    setSessionKey(prev => prev + 1)
    window.setTimeout(loadSessions, 1000)
  }, [loadSessions])

  // Switch to different session
  const handleSwitchSession = useCallback((newSessionId: string) => {
    setSelectedSessionId(newSessionId)
    setSessionKey(prev => prev + 1)
  }, [])

  // Format session name for display
  const getSessionDisplayName = (session: ClaudeSession) => {
    if (session.name && session.name !== 'Terminal Session') {
      return session.name
    }
    return session.id.startsWith('note-') ? 'Note Session' : 'Global Session'
  }

  if (!visible) return null

  return (
    <div className="fixed right-0 top-14 bottom-0 w-[1000px] bg-gray-900 border-l border-gray-700 flex shadow-2xl z-10">
      {/* Sessions Sidebar */}
      {!noteId && (
        <div className="w-64 border-r border-gray-700 flex flex-col bg-gray-800">
          {/* Sidebar Header */}
          <div className="h-12 border-b border-gray-700 flex items-center justify-between px-3">
            <span className="text-sm font-semibold text-gray-300">Sessions</span>
            <button
              onClick={handleNewSession}
              className="p-1 hover:bg-gray-700 rounded text-gray-400 hover:text-gray-200"
              title="New Session"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>

          {/* Sessions List */}
          <div className="flex-1 overflow-y-auto">
            {activeSessions.length === 0 ? (
              <div className="p-4 text-center text-gray-500 text-sm">
                No active sessions
              </div>
            ) : (
              <div className="py-2">
                {activeSessions.map((session) => (
                  <button
                    key={session.id}
                    onClick={() => handleSwitchSession(session.id)}
                    className={`w-full px-3 py-2 text-left hover:bg-gray-700 transition-colors flex items-start gap-2 ${
                      session.id === sessionId ? 'bg-gray-700' : ''
                    }`}
                  >
                    <Terminal className="w-4 h-4 text-purple-400 flex-shrink-0 mt-0.5" />
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-medium text-gray-200 truncate">
                        {getSessionDisplayName(session)}
                      </div>
                      <div className="text-xs text-gray-500 truncate">
                        {session.workingDir}
                      </div>
                      <div className="text-xs text-gray-600 mt-0.5">
                        {new Date(session.lastActivity).toLocaleTimeString()}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Main Terminal Area */}
      <div className="flex-1 flex flex-col">
        {/* Header */}
        <div className="h-12 border-b border-gray-700 flex items-center justify-between px-4 bg-gray-800">
          <div className="flex items-center gap-2">
            <Bot className="w-5 h-5 text-purple-400" />
            <span className="font-semibold text-gray-100">
              Claude Terminal
              {noteId && <span className="text-xs text-gray-500 ml-2">(linked to note)</span>}
            </span>
          </div>
          <div className="flex items-center gap-2">
            {/* Restart Button */}
            <button
              onClick={handleRestart}
              className="p-1.5 hover:bg-gray-700 rounded text-gray-400 hover:text-gray-200"
              title="Restart Session (preserves history)"
            >
              <RotateCcw className="w-4 h-4" />
            </button>
            {/* Kill Button */}
            <button
              onClick={handleKill}
              className="p-1.5 hover:bg-red-900 rounded text-gray-400 hover:text-red-400"
              title="Kill Session (clears history)"
            >
              <Trash2 className="w-4 h-4" />
            </button>
            {/* Close Button */}
            <button
              onClick={onClose}
              className="p-1.5 hover:bg-gray-700 rounded text-gray-400 hover:text-gray-200"
              title="Close Terminal (keeps session alive)"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Terminal Area */}
        <div className="flex-1">
          <TerminalChat
            key={sessionKey}
            sessionId={sessionId}
            dangerousMode={true}
          />
        </div>
      </div>
    </div>
  )
}

export default ChatPanel
