import { useRef, useCallback, useState, forwardRef, useImperativeHandle } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { useTerminalWebSocket, type TaskContextPayload } from '../../hooks/useTerminalWebSocket'
import { useEffect } from 'react'
import { useNotes } from '../../contexts/NotesContext'

interface TerminalChatProps {
  sessionId: string
  sessionName?: string
  dangerousMode?: boolean
  taskContext?: TaskContextPayload | null
  onFocus?: () => void
  /**
   * If set, the backend spawns this session via claude --resume <uuid>
   * from the historical transcript's cwd. Forwarded to the WebSocket
   * init message. Caller is responsible for using the historical UUID
   * as `sessionId` too (so reconnect finds the right session).
   */
  resumeFromSessionId?: string
  /** Pass to fork off the historical transcript instead of continuing. */
  resumeFork?: boolean
  /**
   * If set, the backend attaches to a live daemon session by this UUID
   * — no spawn, no resume. Used by sidebar click on an active session.
   */
  attachToSessionId?: string
  /** When the parent already renders a header with sessionName /
   *  dangerous-mode badge, hide TerminalChat's internal duplicate. */
  hideInternalHeader?: boolean
  /** Fired every time the underlying WS reaches OPEN. Use to trigger
   *  a post-attach repaint of the claude TUI. */
  onReady?: () => void
}

export interface TerminalChatHandle {
  restart: () => void
  sendKey: (data: string) => void
  refit: () => void
  blur: () => void
}

export const TerminalChat = forwardRef<TerminalChatHandle, TerminalChatProps>(
  ({ sessionId, sessionName, dangerousMode = true, taskContext, onFocus, resumeFromSessionId, resumeFork, attachToSessionId, hideInternalHeader, onReady }, ref) => {
  const { currentNote } = useNotes()
  const terminalRef = useRef<HTMLDivElement>(null)
  const xtermRef = useRef<Terminal | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)
  const sendInputRef = useRef<((data: string) => void) | null>(null)
  const sendResizeRef = useRef<((cols: number, rows: number) => void) | null>(null)
  const resizeTimeoutRef = useRef<number | undefined>(undefined)
  const isResizingRef = useRef(false)

  // Initial xterm dimensions. Gated > 0 so useTerminalWebSocket waits
  // for xterm to mount + fit before opening the connection. Without
  // this, init goes out with sessionId only — backend then has to
  // spawn the daemon worker at a default 80x24, claude renders
  // initial scrollback at that size, frontend resize arrives 50-100ms
  // later and only repaints the CURRENT screen via SIGWINCH. The
  // scrollback retains 80-col positioning while the viewport is
  // wider → visible character/line corruption that recovers only
  // when claude redraws.
  const [initialDims, setInitialDims] = useState<{ cols: number; rows: number } | null>(null)

  // Handle output from WebSocket — accepts Uint8Array (decoded from base64)
  const handleOutput = useCallback((data: Uint8Array) => {
    if (!xtermRef.current) {
      console.warn('[Terminal] Cannot write - terminal not initialized')
      return
    }

    const term = xtermRef.current

    // Check if user is at the bottom before writing (within 2 lines tolerance)
    const buffer = term.buffer.active
    const wasAtBottom = buffer.viewportY + term.rows >= buffer.baseY + buffer.cursorY - 2

    term.write(data)

    if (wasAtBottom) {
      requestAnimationFrame(() => {
        if (xtermRef.current) {
          xtermRef.current.scrollToBottom()
        }
      })
    }
  }, [])

  // Setup WebSocket connection — gated on initialDims so the init
  // payload carries the correct cols/rows.
  const { sendInput, sendRestart, sendResize } = useTerminalWebSocket({
    sessionId,
    dangerousMode,
    currentNote: taskContext ? null : currentNote,
    taskContext: taskContext ?? null,
    onOutput: handleOutput,
    resumeFromSessionId,
    resumeFork,
    attachToSessionId,
    sessionName,
    initialCols: initialDims?.cols,
    initialRows: initialDims?.rows,
    onReady,
  })

  // Keep sendInput and sendResize refs up to date
  useEffect(() => {
    sendInputRef.current = sendInput
  }, [sendInput])

  useEffect(() => {
    sendResizeRef.current = sendResize
  }, [sendResize])

  // Expose methods to parent
  useImperativeHandle(ref, () => ({
    restart: () => {
      // reset() wipes the visible buffer + cursor state + terminal modes
      // but leaves scrollback intact. clear() drops scrollback too — we
      // want both: the new session should render into a totally blank
      // terminal so leftover frames from the old PTY don't peek through.
      // Order matters: clear first (drops history), then reset (cursor
      // home, attrs default). Backend then kills the worker, respawns,
      // and streams fresh PTY output over the same WS into the now-empty
      // terminal.
      if (xtermRef.current) {
        try { xtermRef.current.clear() } catch {}
        try { xtermRef.current.reset() } catch {}
      }
      sendRestart()
    },
    sendKey: (data: string) => {
      sendInputRef.current?.(data)
      xtermRef.current?.focus()
    },
    refit: () => {
      if (!fitAddonRef.current || !xtermRef.current) return
      const term = xtermRef.current
      const fit = fitAddonRef.current
      const vp = terminalRef.current?.querySelector('.xterm-viewport') as HTMLElement | null
      const scrollToEnd = () => {
        term?.scrollToBottom()
        if (vp) vp.scrollTop = vp.scrollHeight
      }
      // Force a SIGWINCH-triggering resize even if dimensions didn't
      // change. fit.fit() only emits onResize when cols/rows change,
      // so after a tab-switch (where xterm already had the right
      // size) claude never sees a SIGWINCH and its TUI stays at the
      // OLD width's wrap points — replayed history bytes render
      // garbled. Sending (cols-1, rows) then (cols, rows) gives
      // claude two SIGWINCHes, forcing a clean repaint.
      try { fit.fit() } catch {}
      requestAnimationFrame(() => {
        try { fit.fit() } catch {}
        scrollToEnd()
        const cols = term.cols
        const rows = term.rows
        if (cols > 1 && rows > 0 && sendResizeRef.current) {
          // Jiggle: 1 col narrower then back, ~50ms apart, gives
          // claude two SIGWINCHes regardless of current state.
          sendResizeRef.current(cols - 1, rows)
          setTimeout(() => {
            sendResizeRef.current?.(cols, rows)
            scrollToEnd()
          }, 50)
        }
        setTimeout(() => {
          try { fit.fit() } catch {}
          scrollToEnd()
        }, 80)
      })
    },
    blur: () => {
      xtermRef.current?.blur()
    },
  }), [sendRestart])

  // Initialize terminal
  useEffect(() => {
    if (!terminalRef.current || xtermRef.current) return

    // Create terminal
    const term = new Terminal({
      cursorBlink: true,
      fontSize: window.innerWidth < 768 ? 12 : 13,
      fontFamily: '"JetBrains Mono", Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#06080e',
        foreground: '#d4d4d4',
        cursor: '#ffffff',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#ffffff',
      },
      allowTransparency: false,
      convertEol: true,
      scrollback: 10000,
      scrollOnUserInput: true,
    })

    // Create fit addon
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)

    // Open terminal
    term.open(terminalRef.current)
    fitAddon.fit()

    // Publish initial dimensions so the WS hook can finally connect
    // with cols/rows in the init payload.
    setInitialDims({ cols: term.cols, rows: term.rows })

    // Handle user input - send to WebSocket
    term.onData((data) => {
      sendInputRef.current?.(data)
    })

    // Notify PTY of terminal dimension changes so Claude CLI renders correctly
    term.onResize(({ cols, rows }) => {
      sendResizeRef.current?.(cols, rows)
    })

    // Mobile touch-to-scroll: capture phase so we intercept before xterm's canvas.
    // Use term.scrollLines() — direct scrollTop manipulation is overridden by xterm.
    let lastTouchY = 0
    let scrollAccum = 0

    const onTouchStart = (e: TouchEvent) => {
      lastTouchY = e.touches[0].clientY
      scrollAccum = 0
    }

    const onTouchMove = (e: TouchEvent) => {
      const currentY = e.touches[0].clientY
      const delta = lastTouchY - currentY
      lastTouchY = currentY
      scrollAccum += delta

      const pixelsPerLine = (term.options.fontSize || 13) * (term.options.lineHeight || 1)
      const lines = Math.trunc(scrollAccum / pixelsPerLine)
      if (lines !== 0) {
        term.scrollLines(lines)
        scrollAccum -= lines * pixelsPerLine
      }
      e.preventDefault()
    }

    if (terminalRef.current) {
      terminalRef.current.addEventListener('touchstart', onTouchStart, { passive: true, capture: true })
      terminalRef.current.addEventListener('touchmove', onTouchMove, { passive: false, capture: true })
    }

    // Prevent infinite resize loops
    const handleResize = () => {
      if (isResizingRef.current) return

      if (resizeTimeoutRef.current) {
        clearTimeout(resizeTimeoutRef.current)
      }

      resizeTimeoutRef.current = window.setTimeout(() => {
        if (!fitAddonRef.current || !xtermRef.current) return
        if (!terminalRef.current?.offsetHeight) return

        isResizingRef.current = true
        try {
          fitAddonRef.current.fit()
          // fit() triggers term.onResize which sends the new size to PTY
          xtermRef.current.scrollToBottom()
        } catch (error) {
          console.error('Failed to fit terminal:', error)
        } finally {
          setTimeout(() => {
            isResizingRef.current = false
          }, 200)
        }
      }, 100)
    }

    // Listen to window resize
    window.addEventListener('resize', handleResize)

    // Watch for container size changes using ResizeObserver
    const resizeObserver = new ResizeObserver(() => {
      handleResize()
    })

    if (terminalRef.current) {
      resizeObserver.observe(terminalRef.current)
    }

    xtermRef.current = term
    fitAddonRef.current = fitAddon

    // Focus terminal
    term.focus()

    // Single delayed fit after container stabilizes
    let initialFitTimeout: number | undefined
    initialFitTimeout = window.setTimeout(() => {
      if (fitAddonRef.current && xtermRef.current) {
        try {
          fitAddonRef.current.fit()
          xtermRef.current.scrollToBottom()
        } catch (error) {
          console.error('Failed initial fit:', error)
        }
      }
    }, 300)

    const containerEl = terminalRef.current

    return () => {
      if (initialFitTimeout) {
        clearTimeout(initialFitTimeout)
      }

      window.removeEventListener('resize', handleResize)
      if (resizeTimeoutRef.current) {
        clearTimeout(resizeTimeoutRef.current)
      }
      resizeObserver.disconnect()

      if (containerEl) {
        containerEl.removeEventListener('touchstart', onTouchStart, { capture: true })
        containerEl.removeEventListener('touchmove', onTouchMove, { capture: true })
      }

      term.dispose()
      xtermRef.current = null
      fitAddonRef.current = null
      isResizingRef.current = false
    }
  }, [sessionId])

  return (
    <div className="flex flex-col flex-1 min-h-0" style={{ background: '#06080e' }}>
      {!hideInternalHeader && (
        <div
          className="flex-shrink-0 px-4 py-2 flex items-center gap-3"
          style={{ borderBottom: '1px solid rgba(255,255,255,0.05)' }}
        >
          <span className="text-[10px] font-mono text-slate-700 truncate max-w-[220px]" title={sessionId}>
            {sessionName
              ? sessionName
              : sessionId.startsWith('global-')
              ? `global ···${sessionId.slice(-6)}`
              : sessionId.startsWith('note-task-')
              ? `task ···${sessionId.slice(-6)}`
              : sessionId.startsWith('note-')
              ? `note ···${sessionId.slice(-6)}`
              : `···${sessionId.slice(-6)}`}
          </span>
          {dangerousMode && (
            <span className="text-[10px] font-mono text-yellow-600/80 tracking-wide">
              ⚠ dangerous mode
            </span>
          )}
        </div>
      )}
      <div ref={terminalRef} className="flex-1 min-h-0 overflow-hidden p-2" onPointerDown={() => onFocus?.()} />
    </div>
  )
})

TerminalChat.displayName = 'TerminalChat'

export default TerminalChat
