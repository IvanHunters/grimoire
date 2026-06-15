import { useEffect, useRef } from 'react'
import { X, GitBranch } from 'lucide-react'
import { TerminalChat, type TerminalChatHandle } from '../chat/TerminalChat'

interface ResumeChatModalProps {
  visible: boolean
  onClose: () => void
  /** Full UUID of the session to open. */
  sessionId: string | null
  /** Display name for the modal header (from listing/transcript). */
  sessionName?: string
  /**
   * Open mode:
   *   - 'resume' (default): claude --resume <uuid> from on-disk JSONL
   *   - 'fork': adds --fork-session (new UUID, original untouched)
   *   - 'attach': connect to a live external daemon worker — used
   *     when the session was spawned outside our manager (e.g. by
   *     kvaps via claude --bg). Backend resolves cwd from daemon
   *     record and calls daemon.Attach directly.
   *   - 'open': open an existing in-manager session — fast path
   *     where GetOrCreate finds it already in the map. Used by the
   *     sidebar "click on running session" flow.
   */
  mode?: 'resume' | 'fork' | 'attach' | 'open'
}

/**
 * ResumeChatModal hosts a live chat that continues a historical session.
 * The backend resolves the session's cwd from its JSONL header and
 * dispatches `claude --resume <uuid>`. Once the daemon worker is up
 * the TerminalChat renders the live PTY just like a regular chat.
 *
 * The grimoire-side sessionId we use is the historical UUID itself —
 * keeps things simple, no double identity.
 *
 * Requires USE_DAEMON_BACKEND=1 on the backend. With subprocess backend
 * the spawn fails with an explicit error message in the terminal.
 */
function ResumeChatModal({ visible, onClose, sessionId, sessionName, mode = 'resume' }: ResumeChatModalProps) {
  const fork = mode === 'fork'
  const attach = mode === 'attach'
  const open = mode === 'open'
  const terminalRef = useRef<TerminalChatHandle>(null)

  // When the modal becomes visible, refit so xterm gets accurate
  // dimensions for the modal viewport (it may have been mounted at
  // 0x0 before).
  useEffect(() => {
    if (!visible) return
    const id = setTimeout(() => terminalRef.current?.refit(), 100)
    return () => clearTimeout(id)
  }, [visible])

  useEffect(() => {
    if (!visible) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [visible, onClose])

  if (!sessionId) return null

  const accent = fork ? 'text-violet-400' : (attach || open) ? 'text-emerald-400' : 'text-amber-400'

  return (
    <div
      className="fixed right-0 top-14 bottom-0 w-full md:w-[680px] z-[2200] flex flex-col bg-[#0a0b10] border-l border-white/[0.09] shadow-2xl terminal-panel"
      style={{ display: visible ? undefined : 'none' }}
    >
      <div className="terminal-scanlines pointer-events-none" />

      <div className="relative z-10 flex items-center justify-between gap-3 px-4 py-3 border-b border-white/[0.06] flex-shrink-0">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <GitBranch className={`w-3.5 h-3.5 flex-shrink-0 ${accent}`} />
          <div className="min-w-0">
            <div className={`text-[10px] font-mono font-semibold tracking-widest uppercase truncate ${accent}`}>
              {fork ? 'forking' : open ? 'open' : attach ? 'attached to' : 'resuming'} · {sessionName || 'session'}
            </div>
            <div className="font-mono text-[10px] text-slate-500 mt-0.5 truncate">
              #{sessionId.slice(0, 8)} · {open ? 'connected' : attach ? 'op:attach (live)' : `claude --resume${fork ? ' --fork-session' : ''}`}
            </div>
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 text-slate-500 hover:text-slate-300 transition-colors rounded flex-shrink-0"
          title="Detach (session keeps running)"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      <div className="relative z-10 flex-1 min-h-0 flex flex-col">
        <TerminalChat
          ref={terminalRef}
          sessionId={sessionId}
          sessionName={sessionName}
          dangerousMode={true}
          resumeFromSessionId={open || attach ? undefined : sessionId}
          resumeFork={fork}
          attachToSessionId={attach ? sessionId : undefined}
        />
      </div>
    </div>
  )
}

export default ResumeChatModal
