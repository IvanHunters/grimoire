import { useState, useCallback, useRef, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { X, RotateCcw, Trash2 } from 'lucide-react'
import { TerminalChat, type TerminalChatHandle } from './TerminalChat'
import { sessionsAPI, type SessionStatus } from '../../api/sessions'
import { useSessionStatus } from '../../hooks/useSessionStatus'

interface GlobalTab {
  sessionId: string
  label: string
  sessionKey: number
}

const STORAGE_KEY = 'global-terminal-tabs'

function loadTabs(): GlobalTab[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as Array<{ sessionId: string; label: string }>
      if (Array.isArray(parsed) && parsed.length > 0) {
        return parsed.map(t => ({ sessionId: t.sessionId, label: t.label, sessionKey: 0 }))
      }
    }
  } catch {}
  return [newTab(1)]
}

function newTab(n: number): GlobalTab {
  return {
    sessionId: `global-${Math.random().toString(36).slice(2, 8)}`,
    label: `terminal ${n}`,
    sessionKey: 0,
  }
}

function saveTabs(tabs: GlobalTab[]) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(
    tabs.map(({ sessionId, label }) => ({ sessionId, label }))
  ))
}

interface GlobalTerminalPanelProps {
  visible: boolean
  onClose: () => void
  onMobileSidebarClose?: () => void
}

type KeyVariant = 'default' | 'danger' | 'nav' | 'enter'

function TermKey({ label, onPress, variant }: { label: string; onPress: () => void; variant: KeyVariant }) {
  return (
    <button onPointerDown={(e) => { e.preventDefault(); onPress() }} className={`term-key term-key-${variant}`}>
      {label}
    </button>
  )
}

export default function GlobalTerminalPanel({ visible, onClose, onMobileSidebarClose }: GlobalTerminalPanelProps) {
  const [tabs, setTabs] = useState<GlobalTab[]>(loadTabs)
  const [activeIdx, setActiveIdx] = useState(0)

  // Poll status of the active tab's session. The hook handles all the
  // start/stop polling logic via `enabled`. When the user switches tabs
  // the sessionId arg changes and the hook restarts on the new session.
  const activeSessionId = tabs[activeIdx]?.sessionId
  const { status: activeStatus } = useSessionStatus(activeSessionId, {
    enabled: visible && !!activeSessionId,
    intervalMs: 2000,
  })
  // Defer mounting terminals until panel is first opened — prevents phantom WS sessions on load
  const [everOpened, setEverOpened] = useState(false)
  useEffect(() => { if (visible) setEverOpened(true) }, [visible])
  const [showKeyboard, setShowKeyboard] = useState(() => localStorage.getItem('terminal.showKeyboard') !== 'false')
  const [pasteOpen, setPasteOpen] = useState(false)
  const [keyboardOffset, setKeyboardOffset] = useState(0)
  const terminalRefs = useRef<Map<string, TerminalChatHandle>>(new Map())

  useEffect(() => { saveTabs(tabs) }, [tabs])

  // Listen for external tabs-changed events — sidebar dispatches one
  // when it deletes a global-* session. Re-read localStorage so the
  // panel doesn't keep a stale tab that respawns the dead worker on
  // next open. If the active tab vanished, fall back to index 0.
  useEffect(() => {
    const handler = () => {
      const fresh = loadTabs()
      setTabs(fresh)
      setActiveIdx(idx => Math.min(idx, Math.max(0, fresh.length - 1)))
    }
    window.addEventListener('global-terminal-tabs-changed', handler)
    return () => window.removeEventListener('global-terminal-tabs-changed', handler)
  }, [])

  // On first mount, reconcile cached tabs against the backend's live
  // session list. Tabs whose sessionId no longer corresponds to a
  // daemon worker get dropped — without this, opening Quick Terminal
  // after a backend restart respawns dead "global-*" workers because
  // the active tab's TerminalChat would init against the stale id.
  // Always keeps at least one tab (creates a fresh one if all dead)
  // so the panel never opens empty.
  useEffect(() => {
    let cancelled = false
    sessionsAPI.listByProject().then((items) => {
      if (cancelled) return
      const live = new Set(items.map((s) => s.sessionId))
      setTabs((prev) => {
        const alive = prev.filter((t) => live.has(t.sessionId))
        if (alive.length === prev.length) return prev
        if (alive.length === 0) return [newTab(1)]
        return alive
      })
      setActiveIdx((idx) => {
        // Defer correction to next paint so setTabs settles first.
        return idx
      })
    }).catch(() => {})
    return () => { cancelled = true }
  }, [])

  // Visual viewport for mobile keyboard — update immediately in both directions
  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const update = () => {
      const kb = Math.max(0, window.innerHeight - vv.height - vv.offsetTop)
      setKeyboardOffset(kb)
    }
    vv.addEventListener('resize', update)
    vv.addEventListener('scroll', update)
    return () => { vv.removeEventListener('resize', update); vv.removeEventListener('scroll', update) }
  }, [])

  // Refit after panel height settles following keyboard change
  useEffect(() => {
    const id = tabs[activeIdx]?.sessionId
    if (!id) return
    const t = setTimeout(() => terminalRefs.current.get(id)?.refit(), 80)
    return () => clearTimeout(t)
  }, [keyboardOffset, activeIdx, tabs])

  // Refit when panel becomes visible
  useEffect(() => {
    if (!visible) return
    const id = tabs[activeIdx]?.sessionId
    const t = setTimeout(() => { if (id) terminalRefs.current.get(id)?.refit() }, 350)
    return () => clearTimeout(t)
  }, [visible])

  const handleAddTab = useCallback(() => {
    setTabs(prev => {
      const tab = newTab(prev.length + 1)
      setActiveIdx(prev.length)
      return [...prev, tab]
    })
  }, [])

  const handleCloseTab = useCallback((idx: number, e: React.MouseEvent) => {
    e.stopPropagation()
    // Only remove the tab visually — DO NOT delete the session. In
    // daemon-backed mode sessions live independently of UI; the user
    // can re-attach later via Sidebar or SessionsModal. Explicit kill
    // is the Trash button (handleKillActive) and SessionsModal row.
    setTabs(prev => {
      const next = prev.filter((_, i) => i !== idx)
      if (next.length === 0) {
        const fresh = newTab(1)
        setActiveIdx(0)
        onClose()
        return [fresh]
      }
      setActiveIdx(cur => (cur >= idx && cur > 0) ? cur - 1 : Math.min(cur, next.length - 1))
      return next
    })
  }, [onClose])

  const handleKillActive = useCallback(async () => {
    const tab = tabs[activeIdx]
    if (!tab) return
    try { await sessionsAPI.deleteSession(tab.sessionId, { deleteTranscript: true }) } catch {}
    setTabs(prev => prev.map((t, i) => i === activeIdx ? { ...t, sessionKey: t.sessionKey + 1 } : t))
  }, [tabs, activeIdx])

  const handleRestartActive = useCallback(() => {
    const id = tabs[activeIdx]?.sessionId
    if (id) terminalRefs.current.get(id)?.restart()
  }, [tabs, activeIdx])

  // Same external restart trigger ChatPanel listens to — Sidebar
  // dispatches after a successful Compact so the live daemon worker
  // reloads from the shrunken JSONL. We match against ANY tab, not
  // just the active one, so background tabs reload too.
  useEffect(() => {
    const onRestartReq = (e: Event) => {
      const ce = e as CustomEvent<{ sessionId: string }>
      const target = ce.detail?.sessionId
      if (!target) return
      const handle = terminalRefs.current.get(target)
      if (handle) handle.restart()
    }
    window.addEventListener('claude-session-restart-request', onRestartReq)
    return () => window.removeEventListener('claude-session-restart-request', onRestartReq)
  }, [])

  const sendKey = useCallback((data: string) => {
    const id = tabs[activeIdx]?.sessionId
    if (id) terminalRefs.current.get(id)?.sendKey(data)
  }, [tabs, activeIdx])

  const blurTerminal = useCallback(() => {
    const id = tabs[activeIdx]?.sessionId
    if (id) terminalRefs.current.get(id)?.blur()
  }, [tabs, activeIdx])

  // Keep mounted to preserve WS connections; use display:none on the outer div
  return (
    <div
      className="fixed right-0 flex flex-col z-20 top-14 w-full md:w-[680px] terminal-panel"
      style={{ bottom: `${keyboardOffset}px`, display: visible ? undefined : 'none' }}
    >
      <div className="terminal-scanlines" />

      {/* Header */}
      <div className="terminal-header relative z-10 flex-shrink-0 px-3 py-2 flex items-center justify-between">
        <div className="flex items-center gap-2.5 min-w-0 flex-1">
          <span className="terminal-led" />
          <div className="font-mono font-semibold text-cyan-400/80" style={{ fontSize: 10, letterSpacing: '0.18em' }}>
            CLAUDE / TERMINAL
          </div>
          <StatusBadge status={activeStatus} />
        </div>
        <div className="flex items-center gap-0.5 flex-shrink-0">
          <button onClick={handleRestartActive} className="terminal-btn" title="Restart Session">
            <RotateCcw className="w-3 h-3" />
          </button>
          <button onClick={handleKillActive} className="terminal-btn terminal-btn-kill" title="Kill Session">
            <Trash2 className="w-3 h-3" />
          </button>
          <button
            onClick={() => {
              setShowKeyboard(v => {
                const next = !v
                localStorage.setItem('terminal.showKeyboard', String(next))
                return next
              })
              setTimeout(() => {
                const id = tabs[activeIdx]?.sessionId
                if (id) terminalRefs.current.get(id)?.refit()
              }, 50)
            }}
            className={`terminal-btn md:hidden ${showKeyboard ? 'opacity-100' : 'opacity-40'}`}
            title="Toggle keyboard"
          >
            <i className="fas fa-keyboard text-[10px]" />
          </button>
          <button onClick={onClose} className="terminal-btn" title="Close">
            <X className="w-3 h-3" />
          </button>
        </div>
      </div>

      {/* Tab bar */}
      <div
        className="relative z-10 flex items-center flex-shrink-0 overflow-x-auto"
        style={{ background: '#040608', borderBottom: '1px solid rgba(255,255,255,0.04)' }}
      >
        {tabs.map((tab, idx) => (
          <button
            key={tab.sessionId}
            onClick={() => setActiveIdx(idx)}
            className={`group relative flex items-center gap-1.5 px-3 h-7 font-mono text-[10px] whitespace-nowrap flex-shrink-0 transition-colors border-r border-white/[0.04] ${
              idx === activeIdx
                ? 'text-cyan-400/90 bg-[#06080e]'
                : 'text-slate-600 hover:text-slate-400 hover:bg-white/[0.02]'
            }`}
          >
            {idx === activeIdx && (
              <span className="absolute bottom-0 left-0 right-0 h-px bg-cyan-500/40" />
            )}
            <span>{tab.label}</span>
            <span
              role="button"
              aria-label="Close tab"
              onClick={(e) => handleCloseTab(idx, e)}
              className={`text-[12px] leading-none transition-all ${
                idx === activeIdx
                  ? 'text-slate-500 hover:text-red-400 opacity-50 hover:opacity-100'
                  : 'opacity-0 group-hover:opacity-40 hover:!text-red-400 hover:!opacity-100'
              }`}
            >
              ×
            </span>
          </button>
        ))}
        <button
          onClick={handleAddTab}
          className="px-3 h-7 text-slate-700 hover:text-cyan-400 transition-colors font-mono text-sm flex-shrink-0 leading-none"
          title="New terminal (Ctrl+T)"
        >
          +
        </button>
      </div>

      {/* Terminal area — ONLY the active tab is mounted. Daemon-backed
          sessions survive remount (the worker keeps running in claude
          daemon), so switching tabs re-attaches to existing PTYs
          without spawning new ones. The previous "mount all, hide
          inactive" rendered every tab as a live WS connection — when
          three tabs were restored from localStorage, opening the
          panel spawned three daemon workers at once. */}
      <div className="flex-1 min-h-0 relative z-10 flex flex-col">
        {everOpened && tabs[activeIdx] && (
          <TerminalChat
            ref={(r) => {
              const sid = tabs[activeIdx].sessionId
              if (r) terminalRefs.current.set(sid, r)
              else terminalRefs.current.delete(sid)
            }}
            key={`${tabs[activeIdx].sessionId}-${tabs[activeIdx].sessionKey}`}
            sessionId={tabs[activeIdx].sessionId}
            dangerousMode={true}
            onFocus={onMobileSidebarClose}
            onReady={() => {
              // Auto-repaint after WS open. Refit only — never send
              // Ctrl+L automatically, claude's TUI may interpret it as
              // a slash action and wipe the conversation. The resize
              // alone reaches the PTY via SIGWINCH and is enough to
              // re-lay-out the current frame at the real xterm dims.
              const sid = tabs[activeIdx].sessionId
              window.setTimeout(() => {
                const r = terminalRefs.current.get(sid)
                if (!r) return
                try { r.refit() } catch {}
              }, 800)
            }}
          />
        )}
      </div>

      {showKeyboard && <div className="md:hidden flex-shrink-0 h-2" style={{ background: '#06080e' }} />}
      {showKeyboard && (
        <div className="md:hidden flex-shrink-0 relative z-10 terminal-keyboard">
          <div className="terminal-key-row">
            <TermKey variant="danger"  label="esc"  onPress={() => sendKey('\x1b')}   />
            <TermKey variant="nav"     label="↑"    onPress={() => sendKey('\x1b[A')} />
            <TermKey variant="nav"     label="↓"    onPress={() => sendKey('\x1b[B')} />
            <TermKey variant="nav"     label="←"    onPress={() => sendKey('\x1b[D')} />
            <TermKey variant="nav"     label="→"    onPress={() => sendKey('\x1b[C')} />
          </div>
          <div className="terminal-key-row">
            <TermKey variant="danger"  label="^C"    onPress={() => sendKey('\x03')} />
            <TermKey variant="default" label="Paste" onPress={async () => {
              try {
                const t = await navigator.clipboard.readText()
                if (t) { sendKey(t); return }
              } catch {}
              setPasteOpen(true)
            }} />
            <TermKey variant="enter"   label="↵"    onPress={() => sendKey('\r')}   />
            <TermKey variant="nav"     label="done" onPress={blurTerminal}          />
          </div>
        </div>
      )}

      {pasteOpen && (
        <PasteOverlay
          onCancel={() => setPasteOpen(false)}
          onPaste={(text) => { if (text) sendKey(text); setPasteOpen(false) }}
        />
      )}
    </div>
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

// StatusBadge mirrors the one in ChatPanel — same colour conventions so
// users learn the dot meanings once. Hidden when status is null
// (panel just opened / no active session yet).
function StatusBadge({ status }: { status: SessionStatus | null }) {
  if (!status) return null
  const { color, label } = badgeMeta(status)
  const tooltip = status.detail ? `${label} · ${status.detail}` : label
  return (
    <div
      className="flex items-center gap-1 flex-shrink-0 ml-1"
      title={tooltip}
      style={{ fontSize: 9 }}
    >
      <span
        className="inline-block rounded-full"
        style={{
          width: 7,
          height: 7,
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

function badgeMeta(s: SessionStatus): { color: string; label: string } {
  switch (s.tempo) {
    case 'active':
      return { color: '#facc15', label: 'working' }
    case 'blocked':
      return { color: '#fb923c', label: 'needs you' }
    case 'idle':
      if (s.state === 'done') return { color: '#22c55e', label: 'done' }
      return { color: '#94a3b8', label: 'idle' }
  }
  switch (s.state) {
    case 'failed':
      return { color: '#ef4444', label: 'failed' }
    case 'stopped':
      return { color: '#6b7280', label: 'stopped' }
    case 'running':
      return { color: '#22d3ee', label: 'running' }
  }
  return { color: '#64748b', label: 'unknown' }
}
