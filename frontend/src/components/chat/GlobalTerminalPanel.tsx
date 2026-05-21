import { useState, useCallback, useRef, useEffect } from 'react'
import { X, RotateCcw, Trash2 } from 'lucide-react'
import { TerminalChat, type TerminalChatHandle } from './TerminalChat'
import { sessionsAPI } from '../../api/sessions'

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
  // Defer mounting terminals until panel is first opened — prevents phantom WS sessions on load
  const [everOpened, setEverOpened] = useState(false)
  useEffect(() => { if (visible) setEverOpened(true) }, [visible])
  const [showKeyboard, setShowKeyboard] = useState(() => localStorage.getItem('terminal.showKeyboard') !== 'false')
  const [keyboardOffset, setKeyboardOffset] = useState(0)
  const terminalRefs = useRef<Map<string, TerminalChatHandle>>(new Map())

  useEffect(() => { saveTabs(tabs) }, [tabs])

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

  const handleCloseTab = useCallback(async (idx: number, e: React.MouseEvent) => {
    e.stopPropagation()
    const tab = tabs[idx]
    try { await sessionsAPI.deleteSession(tab.sessionId) } catch {}
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
  }, [tabs, onClose])

  const handleKillActive = useCallback(async () => {
    const tab = tabs[activeIdx]
    if (!tab) return
    try { await sessionsAPI.deleteSession(tab.sessionId) } catch {}
    setTabs(prev => prev.map((t, i) => i === activeIdx ? { ...t, sessionKey: t.sessionKey + 1 } : t))
  }, [tabs, activeIdx])

  const handleRestartActive = useCallback(() => {
    const id = tabs[activeIdx]?.sessionId
    if (id) terminalRefs.current.get(id)?.restart()
  }, [tabs, activeIdx])

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

      {/* Terminal area — active in flex, inactive absolutely behind */}
      <div className="flex-1 min-h-0 relative z-10">
        {everOpened && tabs.map((tab, idx) => (
          <div
            key={tab.sessionId}
            style={
              idx === activeIdx
                ? { position: 'relative', width: '100%', height: '100%', display: 'flex', flexDirection: 'column' }
                : { position: 'absolute', inset: 0, opacity: 0, pointerEvents: 'none', zIndex: -1, display: 'flex', flexDirection: 'column' }
            }
          >
            <TerminalChat
              ref={(r) => { if (r) terminalRefs.current.set(tab.sessionId, r); else terminalRefs.current.delete(tab.sessionId) }}
              key={tab.sessionKey}
              sessionId={tab.sessionId}
              dangerousMode={true}
              onFocus={onMobileSidebarClose}
            />
          </div>
        ))}
      </div>

      {showKeyboard && <div className="md:hidden flex-shrink-0 h-2" style={{ background: '#06080e' }} />}
      {showKeyboard && (
        <div className="md:hidden flex-shrink-0 relative z-10 terminal-keyboard">
          <div className="terminal-key-row">
            <TermKey variant="danger"  label="ESC" onPress={() => sendKey('\x1b')}   />
            <TermKey variant="danger"  label="^C"  onPress={() => sendKey('\x03')}   />
            <TermKey variant="default" label="^D"  onPress={() => sendKey('\x04')}   />
            <TermKey variant="default" label="^L"  onPress={() => sendKey('\x0c')}   />
            <TermKey variant="default" label="^A"  onPress={() => sendKey('\x01')}   />
            <TermKey variant="default" label="^E"  onPress={() => sendKey('\x05')}   />
          </div>
          <div className="terminal-key-row">
            <TermKey variant="nav"     label="↑"   onPress={() => sendKey('\x1b[A')} />
            <TermKey variant="nav"     label="↓"   onPress={() => sendKey('\x1b[B')} />
            <TermKey variant="nav"     label="←"   onPress={() => sendKey('\x1b[D')} />
            <TermKey variant="nav"     label="→"   onPress={() => sendKey('\x1b[C')} />
            <TermKey variant="default" label="Tab" onPress={() => sendKey('\t')}     />
            <TermKey variant="enter"   label="↵"   onPress={() => sendKey('\r')}     />
            <TermKey variant="nav"     label="done" onPress={blurTerminal}            />
          </div>
        </div>
      )}
    </div>
  )
}
