import { useState, useCallback, useRef, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { X, RotateCcw, Trash2 } from 'lucide-react'
import { TerminalChat, type TerminalChatHandle } from './TerminalChat'
import { sessionsAPI } from '../../api/sessions'
import { useNotes } from '../../contexts/NotesContext'
import type { TaskContextPayload } from '../../hooks/useTerminalWebSocket'

interface ChatPanelProps {
  visible: boolean
  onClose: () => void
  noteId: string
  taskContext?: TaskContextPayload | null
  onCloseMobileSidebar?: () => void
}

function ChatPanel({ visible, onClose, noteId, taskContext, onCloseMobileSidebar }: ChatPanelProps) {
  const { currentNote } = useNotes()
  const [sessionKey, setSessionKey] = useState(0)
  const [showKeyboard, setShowKeyboard] = useState(() => localStorage.getItem('terminal.showKeyboard') !== 'false')
  const [pasteOpen, setPasteOpen] = useState(false)
  const [sessionName, setSessionName] = useState<string>('')
  const terminalRef = useRef<TerminalChatHandle>(null)

  const sessionId = `note-${noteId}`

  useEffect(() => {
    let cancelled = false
    sessionsAPI.listActiveSessions().then(sessions => {
      if (cancelled) return
      const s = sessions.find(s => s.id === sessionId)
      setSessionName(s?.name ?? '')
    }).catch(() => {})
    return () => { cancelled = true }
  }, [sessionId])
  const [keyboardOffset, setKeyboardOffset] = useState(0)

  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const update = () => {
      const kb = Math.max(0, window.innerHeight - vv.height - vv.offsetTop)
      setKeyboardOffset(kb)
    }
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    return () => {
      vv.removeEventListener('resize', update)
      vv.removeEventListener('scroll', update)
    }
  }, [])

  // Refit after panel height settles following keyboard change
  useEffect(() => {
    const t = setTimeout(() => terminalRef.current?.refit(), 80)
    return () => clearTimeout(t)
  }, [keyboardOffset])

  // Refit after panel opens so xterm accounts for the keyboard bar height
  useEffect(() => {
    if (!visible) return
    const t = setTimeout(() => terminalRef.current?.refit(), 350)
    return () => clearTimeout(t)
  }, [visible])

  const handleRestart = useCallback(() => {
    terminalRef.current?.restart()
  }, [])

  const handleKill = useCallback(async () => {
    try {
      await sessionsAPI.deleteSession(sessionId)
      setSessionKey(prev => prev + 1)
    } catch {
      setSessionKey(prev => prev + 1)
    }
  }, [sessionId])

  const sendKey = useCallback((data: string) => {
    terminalRef.current?.sendKey(data)
  }, [])

  const blurTerminal = useCallback(() => {
    terminalRef.current?.blur()
  }, [])

  return (
    <div
      className="fixed right-0 flex flex-col z-20 top-14 w-full md:w-[680px] terminal-panel"
      style={{ bottom: `${keyboardOffset}px`, display: visible ? undefined : 'none' }}
    >

      {/* Scanline texture — visual atmosphere only */}
      <div className="terminal-scanlines" />

      {/* ── Header ─────────────────────────────────────────────── */}
      <div className="terminal-header relative z-10 flex-shrink-0 px-3 py-2 flex items-center justify-between">
        <div className="flex items-center gap-2.5 min-w-0 flex-1">
          <span className="terminal-led" />
          <div className="min-w-0">
            <div
              className="font-mono font-semibold text-cyan-400/80"
              style={{ fontSize: 10, letterSpacing: '0.18em' }}
            >
              CLAUDE / TERMINAL
            </div>
            {taskContext ? (
              <div className="font-mono text-slate-700 truncate mt-0.5" style={{ fontSize: 9 }}>
                task: {taskContext.title}
              </div>
            ) : currentNote && (
              <div className="font-mono text-slate-700 truncate mt-0.5" style={{ fontSize: 9 }}>
                {currentNote.folder ? `${currentNote.folder}/` : ''}{currentNote.title}
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center gap-0.5 flex-shrink-0">
          <button
            onClick={handleRestart}
            className="terminal-btn"
            title="Restart Session"
          >
            <RotateCcw className="w-3 h-3" />
          </button>
          <button
            onClick={handleKill}
            className="terminal-btn terminal-btn-kill"
            title="Kill Session"
          >
            <Trash2 className="w-3 h-3" />
          </button>
          <button
            onClick={() => { setShowKeyboard(v => { const next = !v; localStorage.setItem('terminal.showKeyboard', String(next)); return next }); setTimeout(() => terminalRef.current?.refit(), 50) }}
            className={`terminal-btn md:hidden ${showKeyboard ? 'opacity-100' : 'opacity-40'}`}
            title="Toggle keyboard"
          >
            <i className="fas fa-keyboard text-[10px]" />
          </button>
          <button
            onClick={onClose}
            className="terminal-btn"
            title="Close"
          >
            <X className="w-3 h-3" />
          </button>
        </div>
      </div>

      {/* ── Terminal ────────────────────────────────────────────── */}
      <div className="flex-1 min-h-0 relative z-10 flex flex-col">
        <TerminalChat
          ref={terminalRef}
          key={`${sessionId}-${sessionKey}`}
          sessionId={sessionId}
          sessionName={sessionName}
          dangerousMode={true}
          taskContext={taskContext}
          onFocus={onCloseMobileSidebar}
        />
      </div>

      {/* Gap between terminal output and keyboard bar on mobile */}
      {showKeyboard && <div className="md:hidden flex-shrink-0 h-2" style={{ background: '#06080e' }} />}

      {/* ── Mobile Virtual Keyboard (hidden on md+) ─────────────── */}
      {showKeyboard && <div className="md:hidden flex-shrink-0 relative z-10 terminal-keyboard">
        <div className="terminal-key-row">
          <TermKey variant="nav"    label="↑"    onPress={() => sendKey('\x1b[A')} />
          <TermKey variant="nav"    label="↓"    onPress={() => sendKey('\x1b[B')} />
          <TermKey variant="nav"    label="←"    onPress={() => sendKey('\x1b[D')} />
          <TermKey variant="nav"    label="→"    onPress={() => sendKey('\x1b[C')} />
        </div>
        <div className="terminal-key-row">
          <TermKey variant="danger" label="^C"    onPress={() => sendKey('\x03')} />
          <TermKey variant="default" label="Paste" onPress={async () => {
            try {
              const t = await navigator.clipboard.readText()
              if (t) { sendKey(t); return }
            } catch {}
            setPasteOpen(true)
          }} />
          <TermKey variant="enter"  label="↵"    onPress={() => sendKey('\r')}   />
          <TermKey variant="nav"    label="done" onPress={blurTerminal}          />
        </div>
      </div>}

      {pasteOpen && (
        <PasteOverlay
          onCancel={() => setPasteOpen(false)}
          onPaste={(text) => { if (text) sendKey(text); setPasteOpen(false) }}
        />
      )}
    </div>
  )
}

/* ── Key component ──────────────────────────────────────────────── */

type KeyVariant = 'default' | 'danger' | 'nav' | 'enter'

interface TermKeyProps {
  label: string
  onPress: () => void
  variant: KeyVariant
}

function TermKey({ label, onPress, variant }: TermKeyProps) {
  return (
    <button
      onPointerDown={(e) => { e.preventDefault(); onPress() }}
      className={`term-key term-key-${variant}`}
    >
      {label}
    </button>
  )
}

function PasteOverlay({ onCancel, onPaste }: { onCancel: () => void; onPaste: (text: string) => void }) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const [kbOffset, setKbOffset] = useState(0)
  useEffect(() => {
    ref.current?.focus()
    const vv = window.visualViewport
    if (!vv) return
    const update = () => setKbOffset(Math.max(0, window.innerHeight - vv.height - vv.offsetTop))
    update()
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    return () => {
      vv.removeEventListener('resize', update)
      vv.removeEventListener('scroll', update)
    }
  }, [])
  return createPortal(
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        right: 0,
        bottom: kbOffset,
        zIndex: 9999,
        background: 'rgba(0,0,0,0.65)',
        display: 'flex',
        alignItems: 'flex-end',
        boxSizing: 'border-box',
      }}
      onClick={onCancel}
    >
      <div
        style={{
          boxSizing: 'border-box',
          width: '100%',
          padding: 12,
          background: '#1a1d24',
          borderTop: '1px solid #2a2e38',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ color: '#9ca3af', fontSize: 12, marginBottom: 8 }}>
          Долгий тап в поле → выбери «Вставить»
        </div>
        <textarea
          ref={ref}
          autoFocus
          rows={3}
          onPaste={(e) => {
            const t = e.clipboardData.getData('text')
            e.preventDefault()
            onPaste(t)
          }}
          onChange={(e) => {
            const t = e.target.value
            if (t) onPaste(t)
          }}
          style={{
            boxSizing: 'border-box',
            display: 'block',
            width: '100%',
            background: '#0a0c12',
            color: '#fff',
            border: '1px solid #333',
            borderRadius: 6,
            padding: 8,
            fontFamily: 'monospace',
            fontSize: 16,
            resize: 'none',
          }}
        />
        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 8 }}>
          <button
            onClick={onCancel}
            style={{ color: '#9ca3af', fontSize: 14, padding: '6px 12px', background: 'transparent', border: 'none' }}
          >
            Отмена
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

export default ChatPanel
