import { useEffect, useRef, useState } from 'react'
import { X, User, Bot, AlertCircle, Wrench, Play, GitFork, FileJson } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import { sessionsAPI, type Transcript, type TranscriptMessage } from '../../api/sessions'

interface TranscriptViewerProps {
  visible: boolean
  onClose: () => void
  sessionId: string | null
  /** Source JSONL line number to scroll to (from a search hit). */
  scrollToLine?: number
  /**
   * Called when user clicks "Continue". The parent should open a chat
   * panel that resumes this session. Omit to hide the button (e.g. when
   * we know the daemon backend isn't available).
   */
  onContinue?: (sessionId: string, sessionName: string) => void
  /**
   * Called when user clicks "Fork". Same as onContinue but the parent
   * should pass fork=true to ResumeChatModal so claude adds
   * --fork-session and the original session is preserved.
   */
  onFork?: (sessionId: string, sessionName: string) => void
  /**
   * Called when the user clicks "Attach to live" in the empty-state.
   * Used when the session has no JSONL on disk yet but might be alive
   * in the daemon — attach instead of trying to resume.
   */
  onAttachLive?: (sessionId: string, sessionName: string) => void
}

/**
 * TranscriptViewer is a read-only render of a session's chat history.
 * Backend filters JSONL metadata events; we just display user/assistant
 * messages as bubbles. Markdown is rendered for assistant text since
 * claude often outputs code blocks and lists.
 *
 * scrollToLine, when set, scrolls the matching message into view on open.
 * It maps directly to the source JSONL line number stored on each
 * TranscriptMessage.
 */
function TranscriptViewer({ visible, onClose, sessionId, scrollToLine, onContinue, onFork, onAttachLive }: TranscriptViewerProps) {
  const [transcript, setTranscript] = useState<Transcript | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // notFound is the "transcript doesn't exist on disk yet" state —
  // distinct from a generic backend error. Common for freshly-spawned
  // live sessions that haven't completed their first turn.
  const [notFound, setNotFound] = useState(false)

  useEffect(() => {
    if (!visible || !sessionId) {
      setTranscript(null)
      setError(null)
      setNotFound(false)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    setNotFound(false)
    sessionsAPI
      .getTranscript(sessionId)
      .then((tr) => {
        if (!cancelled) setTranscript(tr)
      })
      .catch((e) => {
        if (cancelled) return
        // axios puts the HTTP status under e.response.status. A 404
        // means the JSONL isn't on disk yet — friendly empty state
        // rather than red "error: …".
        if (e?.response?.status === 404) {
          setNotFound(true)
        } else {
          setError(String(e?.message ?? e))
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [visible, sessionId])

  // After messages render, scroll the target line into view if requested.
  useEffect(() => {
    if (!transcript || !scrollToLine || !containerRef.current) return
    // Defer to next frame so the DOM has the new messages.
    const id = requestAnimationFrame(() => {
      const el = containerRef.current?.querySelector(
        `[data-line="${scrollToLine}"]`,
      )
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' })
    })
    return () => cancelAnimationFrame(id)
  }, [transcript, scrollToLine])

  if (!visible) return null

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose()
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }

  const header = transcript?.header

  // Raw JSONL download — lossless format that can be re-imported on
  // another machine for a perfect roundtrip. Goes through the backend
  // streaming endpoint so we never load big transcripts into JS heap.
  const handleDownloadJSONL = () => {
    if (!sessionId) return
    // We can't trigger Content-Disposition via fetch + blob easily
    // for cross-origin scenarios, but the API is same-origin via the
    // Vite proxy / production reverse proxy, so a plain anchor works.
    const a = document.createElement('a')
    a.href = `/api/sessions/${sessionId}/jsonl`
    a.download = `${sessionId}.jsonl`
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  return (
    <div
      className="fixed inset-0 bg-black/80 backdrop-blur-sm flex items-start justify-center z-[2100] pt-8"
      onClick={handleOverlayClick}
      onKeyDown={handleKeyDown}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-4xl mx-4 max-h-[90vh] flex flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 px-5 py-3 border-b border-white/[0.06] flex-shrink-0">
          <div className="min-w-0 flex-1">
            <div className="text-[10px] font-mono font-semibold tracking-widest text-cyan-400 uppercase truncate">
              {header?.name || 'transcript'}
            </div>
            {header && (
              <div className="font-mono text-[10px] text-slate-500 mt-0.5 truncate">
                {sessionId?.slice(0, 8)} · {header.cwd}
                {header.gitBranch ? ` · ${header.gitBranch}` : ''}
                {header.claudeVersion ? ` · cli ${header.claudeVersion}` : ''}
              </div>
            )}
          </div>
          <div className="flex items-center gap-1 flex-shrink-0">
            {/* Export + Continue + Fork all require the JSONL on disk;
                hide the whole cluster when the transcript is missing
                (live-but-no-turns-yet sessions). */}
            {!notFound && (
              <>
                <button
                  onClick={handleDownloadJSONL}
                  disabled={!sessionId}
                  className="p-1.5 text-slate-500 hover:text-amber-400 transition-colors rounded disabled:opacity-30"
                  title="Download as .jsonl (raw, importable back to another grimoire)"
                >
                  <FileJson className="w-3.5 h-3.5" />
                </button>
                {onContinue && sessionId && (
                  <button
                    onClick={() => onContinue(sessionId, header?.name || 'session')}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-[10px] font-mono uppercase tracking-wider bg-amber-500/10 text-amber-400 border border-amber-500/30 rounded hover:bg-amber-500/20 transition-colors"
                    title="Continue this conversation (requires USE_DAEMON_BACKEND=1)"
                  >
                    <Play className="w-3 h-3" />
                    continue
                  </button>
                )}
                {onFork && sessionId && (
                  <button
                    onClick={() => onFork(sessionId, header?.name || 'session')}
                    className="flex items-center gap-1.5 px-2.5 py-1 text-[10px] font-mono uppercase tracking-wider bg-violet-500/10 text-violet-400 border border-violet-500/30 rounded hover:bg-violet-500/20 transition-colors"
                    title="Fork: branch off a copy (original chat stays untouched)"
                  >
                    <GitFork className="w-3 h-3" />
                    fork
                  </button>
                )}
              </>
            )}
            <button
              onClick={onClose}
              className="p-1.5 text-slate-500 hover:text-slate-300 transition-colors rounded"
              title="Close (Esc)"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        <div
          ref={containerRef}
          className="flex-1 overflow-y-auto px-4 py-4 space-y-4"
        >
          {error && (
            <div className="px-4 py-3 text-xs font-mono text-red-400">
              error: {error}
            </div>
          )}
          {notFound && (
            <div className="px-4 py-12 text-center font-mono text-slate-500 flex flex-col items-center gap-4">
              <div className="text-xs uppercase tracking-widest text-slate-600">
                no transcript on disk
              </div>
              <div className="text-xs text-slate-500 max-w-md">
                This session has no on-disk transcript yet — likely a
                live worker that hasn&apos;t completed its first turn.
                Attach to interact with it directly.
              </div>
              {onAttachLive && sessionId && (
                <button
                  onClick={() => onAttachLive(sessionId, 'live session')}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-mono uppercase tracking-wider bg-emerald-500/10 text-emerald-400 border border-emerald-500/30 rounded hover:bg-emerald-500/20 transition-colors"
                  title="Attach to live daemon worker by UUID"
                >
                  <Play className="w-3 h-3" />
                  attach to live
                </button>
              )}
            </div>
          )}
          {loading && !transcript && !notFound && (
            <div className="px-4 py-8 text-center text-xs font-mono text-slate-500">
              loading transcript…
            </div>
          )}
          {transcript &&
            transcript.messages.map((m) => (
              <MessageBubble key={m.uuid || `${m.lineNumber}`} message={m} highlight={m.lineNumber === scrollToLine} />
            ))}
        </div>

        {header && (
          <div className="border-t border-white/[0.06] px-5 py-2 flex-shrink-0">
            <span className="text-[10px] font-mono text-slate-600">
              {header.messageCount} message{header.messageCount === 1 ? '' : 's'} · started{' '}
              {header.startedAt ? new Date(header.startedAt).toLocaleString() : '—'}
            </span>
          </div>
        )}
      </div>
    </div>
  )
}

function MessageBubble({ message, highlight }: { message: TranscriptMessage; highlight: boolean }) {
  const isUser = message.role === 'user'
  const Icon = isUser ? User : Bot
  const accentColour = message.isError ? 'border-red-500/30 bg-red-500/5' : (isUser ? 'border-cyan-500/15 bg-cyan-500/5' : 'border-slate-700/40 bg-slate-800/20')
  const highlightCls = highlight ? 'ring-2 ring-amber-500/50' : ''
  const ts = message.timestamp ? new Date(message.timestamp) : null

  return (
    <div
      data-line={message.lineNumber}
      className={`rounded-lg border ${accentColour} ${highlightCls} px-4 py-3`}
    >
      <div className="flex items-center gap-2 mb-1.5">
        <Icon className={`w-3 h-3 ${isUser ? 'text-cyan-400' : 'text-slate-400'}`} />
        <span className={`text-[10px] font-mono uppercase tracking-wider ${isUser ? 'text-cyan-400' : 'text-slate-400'}`}>
          {message.role}
        </span>
        {message.isError && (
          <span className="text-[10px] font-mono text-red-400 inline-flex items-center gap-1">
            <AlertCircle className="w-3 h-3" /> error
          </span>
        )}
        {message.hasTools && message.toolUses && (
          <span className="text-[10px] font-mono text-amber-400/80 inline-flex items-center gap-1">
            <Wrench className="w-3 h-3" /> {message.toolUses.join(', ')}
          </span>
        )}
        <span className="text-[10px] font-mono text-slate-600 ml-auto flex-shrink-0">
          {ts ? ts.toLocaleTimeString() : `line ${message.lineNumber}`}
        </span>
      </div>
      <div className="text-sm text-slate-200 font-mono whitespace-pre-wrap break-words leading-relaxed">
        {isUser ? (
          message.text
        ) : (
          <div className="prose prose-invert prose-sm max-w-none prose-pre:my-2 prose-code:text-cyan-300">
            <ReactMarkdown>{message.text}</ReactMarkdown>
          </div>
        )}
      </div>
    </div>
  )
}

export default TranscriptViewer
