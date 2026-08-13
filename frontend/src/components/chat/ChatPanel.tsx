import { useState, useCallback, useRef, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { X, RotateCcw, RefreshCw, Trash2, MoreVertical, FileJson, GitFork, Pencil, Archive } from 'lucide-react'
import { TerminalChat, type TerminalChatHandle } from './TerminalChat'
import { sessionsAPI, type SessionStatus } from '../../api/sessions'
import { useNotes } from '../../contexts/NotesContext'
import { useSessionStatus } from '../../hooks/useSessionStatus'
import type { TaskContextPayload } from '../../hooks/useTerminalWebSocket'

interface ChatPanelProps {
  visible: boolean
  onClose: () => void
  /** Note-bound mode: derives sessionId = `note-${noteId}`. */
  noteId?: string
  taskContext?: TaskContextPayload | null
  onCloseMobileSidebar?: () => void
  /** Free-form mode: caller supplies an explicit session id (UUID, global-*, etc).
   *  Takes precedence over noteId when set. Used for sidebar / sessions modal
   *  attach flows where the session isn't bound to a note. */
  customSessionId?: string
  /** Display name for header when customSessionId is set (sidebar passes the
   *  session's friendly label). Ignored in note-bound mode. */
  customSessionName?: string
  /** Resume an on-disk JSONL — daemon spawns `claude --resume <uuid>`. */
  resumeFromSessionId?: string
  /** When resuming, fork the transcript instead of continuing. */
  resumeFork?: boolean
  /** Attach to an externally-spawned live daemon worker. */
  attachToSessionId?: string
  /** Called when user picks "Fork" from the kebab menu. Parent should
   *  switch its attached-session state to the new (id, name) — that
   *  unmounts the current TerminalChat and mounts a fresh one which
   *  sends the resumeFromSessionId+resumeFork init for the new id. */
  onForked?: (newSessionId: string, newSessionName: string, sourceSessionId: string) => void
}

function ChatPanel({
  visible,
  onClose,
  noteId,
  taskContext,
  onCloseMobileSidebar,
  customSessionId,
  customSessionName,
  resumeFromSessionId,
  resumeFork,
  attachToSessionId,
  onForked,
}: ChatPanelProps) {
  const { currentNote } = useNotes()
  const [sessionKey, setSessionKey] = useState(0)
  const [showKeyboard, setShowKeyboard] = useState(() => localStorage.getItem('terminal.showKeyboard') !== 'false')
  const [pasteOpen, setPasteOpen] = useState(false)
  const [sessionName, setSessionName] = useState<string>(customSessionName || '')
  const [kebabOpen, setKebabOpen] = useState(false)
  const [kebabPos, setKebabPos] = useState<{ top: number; right: number } | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const terminalRef = useRef<TerminalChatHandle>(null)
  const kebabRef = useRef<HTMLDivElement>(null)
  const kebabBtnRef = useRef<HTMLButtonElement>(null)

  // Close kebab on outside-click. Cheap pointerdown listener while open.
  // Note: outside-click checks BOTH the trigger ref AND the portaled
  // dropdown (matched by data attribute) since the dropdown lives in
  // document.body and isn't a DOM descendant of the trigger.
  useEffect(() => {
    if (!kebabOpen) return
    const handler = (e: PointerEvent) => {
      const target = e.target as Node
      const inTrigger = kebabRef.current?.contains(target)
      const inDropdown = (target as HTMLElement).closest?.('[data-chat-kebab-dropdown]')
      if (!inTrigger && !inDropdown) {
        setKebabOpen(false)
      }
    }
    window.addEventListener('pointerdown', handler)
    return () => window.removeEventListener('pointerdown', handler)
  }, [kebabOpen])

  // Recompute kebab dropdown position whenever it opens. Uses the
  // button's bounding rect so the menu sticks to the trigger even
  // when terminal panel is mobile-fullscreen vs desktop side-panel.
  useEffect(() => {
    if (!kebabOpen || !kebabBtnRef.current) return
    const r = kebabBtnRef.current.getBoundingClientRect()
    setKebabPos({ top: r.bottom + 4, right: window.innerWidth - r.right })
  }, [kebabOpen])

  // customSessionId beats noteId. Falls back to empty when neither set —
  // the parent should gate `visible` so this never renders without ID.
  const sessionId = customSessionId || (noteId ? `note-${noteId}` : '')

  // Poll the daemon-backed status while the panel is open. Subprocess
  // sessions get synthesized state=running on the backend, so this is
  // safe to enable unconditionally.
  const { status } = useSessionStatus(sessionId, { enabled: visible, intervalMs: 2000 })

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

  // Restart throttle: rapid clicks would otherwise spawn N daemon
  // workers (each killing the previous), polluting JSONL on disk and
  // burning daemon spare-worker capacity. 2-second cooldown is enough
  // to settle one full restart cycle (dispatch ~30ms + ready ~500ms +
  // PTY first paint ~1s) before allowing the next.
  const restartingRef = useRef(false)
  const [restarting, setRestarting] = useState(false)
  const handleRestart = useCallback(() => {
    if (restartingRef.current) return
    restartingRef.current = true
    setRestarting(true)
    terminalRef.current?.restart()
    setTimeout(() => {
      restartingRef.current = false
      setRestarting(false)
    }, 2000)
  }, [])

  // Listen for external restart triggers (Sidebar Compact dispatches
  // this after the backend JSONL shrink so the live worker reloads
  // from the compacted transcript). Matches by session id — events for
  // other panels are ignored.
  useEffect(() => {
    const onRestartReq = (e: Event) => {
      const ce = e as CustomEvent<{ sessionId: string }>
      if (ce.detail?.sessionId === sessionId) handleRestart()
    }
    window.addEventListener('claude-session-restart-request', onRestartReq)
    return () => window.removeEventListener('claude-session-restart-request', onRestartReq)
  }, [sessionId, handleRestart])

  const handleKill = useCallback(async () => {
    // Kill = backend deletes the session + JSONL, AND we close the
    // panel so the WS doesn't reconnect-and-respawn. The old behaviour
    // (deleteSession + sessionKey bump) re-mounted TerminalChat which
    // immediately sent a new init → backend GetOrCreate spawned a
    // replacement worker → "kill" appeared to do nothing.
    // Also clean up the localStorage tab so Quick Terminal doesn't
    // restore this id on next panel open.
    try {
      await sessionsAPI.deleteSession(sessionId, { deleteTranscript: true })
    } catch (e) {
      console.error('kill session failed', e)
    }
    try {
      const raw = localStorage.getItem('global-terminal-tabs')
      if (raw) {
        const tabs = JSON.parse(raw) as Array<{ sessionId: string; label: string }>
        const filtered = tabs.filter(t => t.sessionId !== sessionId)
        if (filtered.length !== tabs.length) {
          localStorage.setItem('global-terminal-tabs', JSON.stringify(filtered))
          window.dispatchEvent(new CustomEvent('global-terminal-tabs-changed'))
        }
      }
    } catch {}
    onClose()
  }, [sessionId, onClose])

  const sendKey = useCallback((data: string) => {
    terminalRef.current?.sendKey(data)
  }, [])

  // Repaint = kick the claude TUI to redraw itself without killing
  // the session. Sends Ctrl+L (form feed, the conventional "redraw"
  // signal in unix TUIs) to claude over PTY, then refits xterm so
  // it sends a resize event that triggers claude's own SIGWINCH
  // handler (defensive — most claude versions repaint on \x0c
  // alone but a resize forces a re-layout from authoritative dims).
  // Use when display is broken from a mid-stream attach or
  // resize-race issue.
  const handleRepaint = useCallback(() => {
    setKebabOpen(false)
    // \x0c = Ctrl+L
    terminalRef.current?.sendKey('\x0c')
    // Small delay so claude processes Ctrl+L first, then refit
    // triggers a fresh resize → SIGWINCH → full re-layout.
    setTimeout(() => terminalRef.current?.refit(), 80)
  }, [])

  const blurTerminal = useCallback(() => {
    terminalRef.current?.blur()
  }, [])

  // Export: simple anchor click triggering the backend's Content-
  // Disposition stream. Keeps big transcripts out of the JS heap.
  const handleExport = useCallback(() => {
    if (!sessionId) return
    setKebabOpen(false)
    const a = document.createElement('a')
    a.href = `/api/sessions/${sessionId}/jsonl`
    a.download = `${sessionId}.jsonl`
    document.body.appendChild(a)
    a.click()
    a.remove()
  }, [sessionId])

  // Fork: spawn a daemon child via `claude --resume --fork-session`
  // against the current session's JSONL. Generates a new client-side
  // UUID as the grimoireID for the fork so it's a separate row in
  // every listing (and won't smart-attach back into the parent).
  // Caller (HomePage) gets onForked(newId, newName, sourceId) and
  // re-targets the panel.
  const handleFork = useCallback(() => {
    if (!sessionId || !onForked) {
      setKebabOpen(false)
      return
    }
    const proposed = window.prompt('Имя для форка (опционально):', sessionName || 'fork')
    if (proposed === null) {
      setKebabOpen(false)
      return
    }
    const newId = crypto.randomUUID()
    onForked(newId, proposed.trim() || sessionName || 'fork', sessionId)
    setKebabOpen(false)
  }, [sessionId, sessionName, onForked])

  const beginRename = useCallback(() => {
    setRenameValue(sessionName || '')
    setRenaming(true)
    setKebabOpen(false)
  }, [sessionName])

  // Compact: trigger backend deterministic eviction THEN restart so the
  // shrunken JSONL is what claude loads. Without the restart the live
  // worker still holds the full in-memory transcript and the user sees
  // no benefit until the next manual restart.
  const [compacting, setCompacting] = useState(false)
  const handleCompact = useCallback(async () => {
    if (!sessionId || compacting) return
    setKebabOpen(false)
    setCompacting(true)
    try {
      const r = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/compact`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ generate_ledger: true }),
      })
      if (r.status === 404) {
        // No on-disk transcript yet (fresh session, zero turns).
        // Nothing to compact — silently skip and just restart.
        // User intent ≈ "give me a clean state for this session",
        // which a plain restart already provides.
        console.info('[compact] skipped: no transcript on disk yet')
      } else if (!r.ok) {
        throw new Error(`HTTP ${r.status}`)
      } else {
        const j = await r.json()
        console.info('[compact]', {
          bytes: `${j.bytes_before} to ${j.bytes_after}`,
          tokens: `${j.approx_tokens_before} to ${j.approx_tokens_after}`,
          evicted: `${j.tool_results_evicted}/${j.tool_results}`,
          archive: j.archive_path, ledger: j.ledger_path,
        })
        // Visible result — without this the user can't tell whether
        // Compact did anything, which read as "compact doesn't work"
        // (it was a silent no-op on already-compacted sessions).
        const mb = (b: number) => `${(b / 1e6).toFixed(2)} MB`
        if (j.no_change) {
          alert('Nothing to compact: this session is already minimal. Its size is conversation text, not evictable tool output.')
        } else {
          alert(`Compacted: ${mb(j.bytes_before)} to ${mb(j.bytes_after)} (${j.tool_results_evicted}/${j.tool_results} tool results evicted).`)
        }
      }
      // Restart via the same path the manual Restart button uses —
      // throttle is shared so a fast double-fire is safe.
      handleRestart()
    } catch (err) {
      console.error('compact failed', err)
      alert('Compact failed: ' + (err as Error).message)
    } finally {
      setCompacting(false)
    }
  }, [sessionId, compacting, handleRestart])

  const commitRename = useCallback(async () => {
    const trimmed = renameValue.trim()
    if (!trimmed || trimmed === sessionName) {
      setRenaming(false)
      return
    }
    try {
      await sessionsAPI.renameSession(sessionId, trimmed)
      setSessionName(trimmed)
    } catch (err) {
      console.error('rename failed', err)
    } finally {
      setRenaming(false)
    }
  }, [renameValue, sessionName, sessionId])

  return (
    <div
      // Mobile: cover the whole viewport (top:0) so the page header
      // doesn't waste 56px when the terminal is open. Desktop keeps
      // the side-panel layout (top:14, w:680px).
      className="fixed right-0 flex flex-col z-30 top-0 md:top-14 w-full md:w-[680px] terminal-panel"
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
            {renaming ? (
              <input
                autoFocus
                value={renameValue}
                onChange={(e) => setRenameValue(e.target.value)}
                onBlur={commitRename}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') { e.preventDefault(); commitRename() }
                  if (e.key === 'Escape') { e.preventDefault(); setRenaming(false) }
                }}
                className="font-mono bg-transparent border-b border-cyan-500/50 text-cyan-300 outline-none mt-0.5 px-0 py-0"
                style={{ fontSize: 9 }}
              />
            ) : taskContext ? (
              <div className="font-mono text-slate-700 truncate mt-0.5" style={{ fontSize: 9 }}>
                task: {taskContext.title}
              </div>
            ) : customSessionId ? (
              <div className="font-mono text-slate-700 truncate mt-0.5" style={{ fontSize: 9 }}>
                {sessionName || customSessionName || sessionId.slice(0, 8)}
              </div>
            ) : currentNote && (
              <div className="font-mono text-slate-700 truncate mt-0.5" style={{ fontSize: 9 }}>
                {currentNote.folder ? `${currentNote.folder}/` : ''}{currentNote.title}
              </div>
            )}
          </div>
          <StatusBadge status={status} />
        </div>

        <div className="flex items-center gap-1.5 md:gap-0.5 flex-shrink-0">
          {/* Repaint + Kill are desktop-only in the toolbar — on mobile
              they collapse into the kebab menu so the header doesn't
              overflow the narrow viewport (the user reported "верхних
              кнопок не видно совсем"). */}
          <button
            onClick={handleRepaint}
            className="terminal-btn hidden md:inline-flex"
            title="Repaint (Ctrl+L into claude, fixes garbled display)"
          >
            <RotateCcw className="w-3 h-3" />
          </button>
          {/* Kebab trigger. The dropdown itself is rendered via portal
              into document.body (see below) so it isn't clipped by
              xterm's stacking context. On mobile this is the primary
              access point for ALL toolbar actions. */}
          <div className="relative" ref={kebabRef}>
            <button
              ref={kebabBtnRef}
              onClick={() => setKebabOpen(v => !v)}
              className={`terminal-btn ${kebabOpen ? 'bg-white/5 text-slate-300' : ''}`}
              title="More actions"
              aria-label="Open actions menu"
            >
              <MoreVertical className="w-3 h-3" />
            </button>
          </div>
          <button
            onClick={handleKill}
            className="terminal-btn terminal-btn-kill hidden md:inline-flex"
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

      <BlockedBanner status={status} />

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
          resumeFromSessionId={resumeFromSessionId}
          resumeFork={resumeFork}
          attachToSessionId={attachToSessionId}
          hideInternalHeader
          onReady={() => {
            // Auto-repaint after the WS comes up. ONLY refit — never
            // send Ctrl+L automatically because claude's TUI binds it
            // (and similar control keys) to slash actions that would
            // wipe the conversation. The resize alone is enough: it
            // sends SIGWINCH to the PTY and claude re-lays out the
            // current screen at the authoritative xterm dims. The
            // 800ms delay lets claude's first frame land first so the
            // resize re-flow has something to redraw.
            window.setTimeout(() => terminalRef.current?.refit(), 800)
          }}
        />
      </div>

      {/* Gap between terminal output and keyboard bar on mobile */}
      {showKeyboard && <div className="md:hidden flex-shrink-0 h-2" style={{ background: '#06080e' }} />}

      {/* ── Mobile Virtual Keyboard (hidden on md+) ─────────────── */}
      {showKeyboard && <div className="md:hidden flex-shrink-0 relative z-10 terminal-keyboard">
        <div className="terminal-key-row">
          <TermKey variant="danger" label="esc"  onPress={() => sendKey('\x1b')}   />
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

      {/* Kebab dropdown rendered via portal so it floats above xterm's
          stacking context (xterm sets its own z-index and would clip
          an absolutely-positioned in-flow dropdown). Position is
          recomputed from the trigger button's bounding rect. */}
      {kebabOpen && kebabPos && createPortal(
        <div
          data-chat-kebab-dropdown
          className="fixed z-[100] min-w-[180px] bg-[#0a0b10] border border-white/[0.09] rounded shadow-2xl py-1"
          style={{ top: kebabPos.top, right: kebabPos.right }}
        >
          {/* Mobile-only entries — duplicate Repaint and Kill which are
              hidden from the header toolbar on narrow viewports. The
              md:hidden wrapper ensures these only appear on mobile (on
              desktop the same actions live in the always-visible toolbar
              right next to the kebab). */}
          <button
            onClick={() => { setKebabOpen(false); handleRepaint() }}
            className="md:hidden w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-slate-300 hover:bg-white/5 transition-colors"
          >
            <RotateCcw className="w-3 h-3 text-cyan-400/80" />
            Repaint
          </button>
          <button
            onClick={() => { setKebabOpen(false); handleRestart() }}
            disabled={restarting}
            className={`w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-slate-300 hover:bg-white/5 transition-colors ${restarting ? 'opacity-40 cursor-not-allowed' : ''}`}
          >
            <RefreshCw className={`w-3 h-3 text-amber-400/80 ${restarting ? 'animate-spin' : ''}`} />
            {restarting ? 'Restarting…' : 'Restart session'}
          </button>
          <button
            onClick={handleExport}
            className="w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-slate-300 hover:bg-white/5 transition-colors"
          >
            <FileJson className="w-3 h-3 text-amber-400/80" />
            Export .jsonl
          </button>
          {onForked && (
            <button
              onClick={handleFork}
              className="w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-slate-300 hover:bg-white/5 transition-colors"
            >
              <GitFork className="w-3 h-3 text-violet-400/80" />
              Fork…
            </button>
          )}
          <button
            onClick={beginRename}
            className="w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-slate-300 hover:bg-white/5 transition-colors"
          >
            <Pencil className="w-3 h-3 text-cyan-400/80" />
            Rename…
          </button>
          <button
            onClick={handleCompact}
            disabled={compacting}
            className={`w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-slate-300 hover:bg-white/5 transition-colors ${compacting ? 'opacity-40 cursor-not-allowed' : ''}`}
            title="Evict bulky tool_result payloads from older turns. Original archived. Effect lands on next resume."
          >
            <Archive className={`w-3 h-3 text-emerald-400/80 ${compacting ? 'animate-pulse' : ''}`} />
            {compacting ? 'Compacting…' : 'Compact'}
          </button>
          {/* Mobile-only Kill (destructive). Separator + red color cue
              so the user doesn't tap it by accident when scanning the
              menu. Desktop access stays via the standalone toolbar
              button next to the kebab. */}
          <div className="md:hidden h-px bg-white/[0.06] my-1" />
          <button
            onClick={() => { setKebabOpen(false); handleKill() }}
            className="md:hidden w-full flex items-center gap-2 px-3 py-1.5 text-[11px] font-mono text-rose-300 hover:bg-rose-500/10 transition-colors"
          >
            <Trash2 className="w-3 h-3 text-rose-400/80" />
            Kill session
          </button>
        </div>,
        document.body,
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

// Tiny presence indicator next to the chat title. Colour reflects what
// the session is doing right now; tooltip shows the Haiku-generated detail.
// Daemon-backed sessions have rich state; subprocess sessions show a
// neutral "running" dot.
function StatusBadge({ status }: { status: SessionStatus | null }) {
  if (!status) return null
  const { color, label } = badgeMeta(status)
  const tooltip = status.detail
    ? `${label} · ${status.detail}`
    : label
  return (
    <div
      className="flex items-center gap-1 flex-shrink-0 ml-2"
      title={tooltip}
      style={{ fontSize: 9 }}
    >
      <span
        className="inline-block rounded-full"
        style={{
          width: 8,
          height: 8,
          background: color,
          boxShadow: status.tempo === 'active' ? `0 0 6px ${color}` : 'none',
        }}
      />
      <span className="font-mono uppercase tracking-wider text-slate-500">
        {label}
      </span>
    </div>
  )
}

// Banner shown above the terminal when the session is waiting for the
// user's input (state=blocked + needs has the question). The chat is
// still usable; this is a hint that scrolling up will reveal what claude
// is asking.
function BlockedBanner({ status }: { status: SessionStatus | null }) {
  if (!status || status.state !== 'blocked' || !status.needs) return null
  return (
    <div className="relative z-10 flex-shrink-0 px-3 py-2 border-b border-amber-500/30 bg-amber-500/10">
      <div className="font-mono text-amber-300/90" style={{ fontSize: 10, letterSpacing: '0.08em' }}>
        <span className="uppercase tracking-wider mr-2">⏳ needs you:</span>
        {status.needs}
      </div>
    </div>
  )
}

function badgeMeta(s: SessionStatus): { color: string; label: string } {
  if (s.state === 'failed') return { color: '#ef4444', label: 'failed' }
  if (s.state === 'stopped') return { color: '#6b7280', label: 'stopped' }
  if (s.tempo === 'active') return { color: '#22d3ee', label: 'working' }
  if (s.tempo === 'blocked' || s.state === 'blocked') {
    return { color: '#fb923c', label: 'needs you' }
  }
  if (s.state === 'done' || s.state === 'working' || s.state === 'running') {
    return { color: '#22c55e', label: 'ready' }
  }
  return { color: '#64748b', label: 'unknown' }
}

export default ChatPanel
