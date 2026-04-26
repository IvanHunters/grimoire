import { useMemo, useState, useCallback } from 'react'
import { X, Bot, RotateCcw, Trash2 } from 'lucide-react'
import { TerminalChat } from './TerminalChat'

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
 * - Dangerous mode enabled by default
 */
function ChatPanel({ visible, onClose, noteId }: ChatPanelProps) {
  const [sessionKey, setSessionKey] = useState(0)

  // Generate session ID based on context:
  // - If noteId provided → session per note
  // - Otherwise → global session (unique per browser session)
  const sessionId = useMemo(() => {
    if (noteId) {
      return `note-${noteId}`
    }
    // Global session - generate UUID once and keep it
    const globalId = sessionStorage.getItem('claude-global-session')
    if (globalId) {
      return globalId
    }
    const newId = `global-${crypto.randomUUID()}`
    sessionStorage.setItem('claude-global-session', newId)
    return newId
  }, [noteId])

  // Restart session - force remount TerminalChat with new key
  const handleRestart = useCallback(() => {
    setSessionKey(prev => prev + 1)
  }, [])

  // Kill session - close WebSocket and clear from backend
  const handleKill = useCallback(() => {
    // Force remount to close WebSocket
    setSessionKey(prev => prev + 1)
    // If global session, clear from sessionStorage
    if (!noteId) {
      sessionStorage.removeItem('claude-global-session')
    }
  }, [noteId])

  if (!visible) return null

  return (
    <div className="fixed right-0 top-14 bottom-0 w-[800px] bg-gray-900 border-l border-gray-700 flex flex-col shadow-2xl z-10">
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
  )
}

export default ChatPanel
