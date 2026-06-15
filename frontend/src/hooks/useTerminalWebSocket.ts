import { useEffect, useRef, useCallback } from 'react'
import type { Note } from '../types/note'

const WS_URL = import.meta.env.VITE_WS_URL ||
  `${window.location.protocol === 'https:' ? 'wss' : 'ws'}://${window.location.host}/claude-chat`

export interface TaskContextPayload {
  id: string
  title: string
  status: string
  priority: string
  description?: string
  folderPath?: string
  projectPath?: string
}

interface UseTerminalWebSocketProps {
  sessionId: string
  dangerousMode?: boolean
  currentNote?: Note | null
  taskContext?: TaskContextPayload | null
  onOutput: (data: Uint8Array) => void
  /**
   * If set, the backend spawns this session via `claude --resume <uuid>`
   * in the historical session's cwd. Use for "Continue this session"
   * flow on archived transcripts. Requires USE_DAEMON_BACKEND=1.
   */
  resumeFromSessionId?: string
  /**
   * When true alongside resumeFromSessionId, the new session forks from
   * the historical transcript via --fork-session: it gets its own UUID
   * and the original conversation stays untouched. Default (false)
   * continues the same session in place.
   */
  resumeFork?: boolean
  /**
   * If set, the backend attaches to a live daemon worker by this UUID
   * instead of spawning. Use for "open this currently-running session"
   * flow from the sidebar. Mutually exclusive with resumeFromSessionId.
   */
  attachToSessionId?: string
  /**
   * Explicit display name for the session. Sent to backend at init so
   * it can persist to the Mongo overlay alongside the spawn. Use for
   * Fork from kebab where the user types a name — without this, the
   * listing falls back to the daemon's structured "grimoire-fork-…"
   * token until the user renames again later.
   */
  sessionName?: string
  /**
   * Initial xterm dimensions. Both must be > 0 for the hook to open
   * the WS — they're included in the init payload so backend can
   * spawn the daemon worker at the right size from t=0. Without
   * this, claude renders initial scrollback at the daemon default
   * (80x24), then xterm's first resize event arrives 50-100ms later
   * and only repaints the current screen via SIGWINCH — scrollback
   * stays at the old size, lines wrap differently than viewport,
   * characters bleed across rows.
   */
  initialCols?: number
  initialRows?: number
  /**
   * Called every time the WS reaches OPEN and the init payload has been
   * sent (initial connect AND every reconnect). Parents use this to
   * auto-trigger a repaint of the claude TUI once the daemon worker is
   * attached, so a fresh open doesn't leave a half-drawn frame behind.
   */
  onReady?: () => void
}

export function useTerminalWebSocket({ sessionId, dangerousMode = true, currentNote, taskContext, onOutput, resumeFromSessionId, resumeFork, attachToSessionId, sessionName, initialCols, initialRows, onReady }: UseTerminalWebSocketProps) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<number | null>(null)
  const onOutputRef = useRef(onOutput)
  const isConnectingRef = useRef(false)
  const intentionalCloseRef = useRef(false)
  const currentNoteRef = useRef(currentNote)
  const taskContextRef = useRef(taskContext)
  const sendResizeRef = useRef<((cols: number, rows: number) => void) | null>(null)
  const onReadyRef = useRef(onReady)

  // Keep onReady fresh — it's invoked from inside ws.onopen which closes
  // over the ref at attach time, so a parent re-render with a new
  // callback identity still wins.
  useEffect(() => { onReadyRef.current = onReady }, [onReady])

  // One-shot snapshot of spawn-mode props captured at the FIRST init.
  // Reconnects must NOT re-send resume_from/fork/attach — otherwise
  // every WS reconnection respawns a fresh fork worker via
  // GetOrResume, polluting m.sessions and creating duplicate rows in
  // the sidebar. After the first init we set initSentRef so future
  // reconnects send only the basic identity (sessionId + dims).
  const initSentRef = useRef(false)
  const initSnapshotRef = useRef<{ resumeFromSessionId?: string; resumeFork?: boolean; attachToSessionId?: string; sessionName?: string }>({})

  // Keep onOutput callback ref up to date
  useEffect(() => {
    onOutputRef.current = onOutput
  }, [onOutput])

  // Keep currentNote ref up to date
  useEffect(() => {
    currentNoteRef.current = currentNote
  }, [currentNote])

  // Keep taskContext ref up to date
  useEffect(() => {
    taskContextRef.current = taskContext
  }, [taskContext])

  // Connect to WebSocket
  useEffect(() => {
    // Wait for xterm to mount + fit before opening the WS. Sending
    // init without cols/rows would force the backend to spawn the
    // daemon worker at the default 80x24, which causes initial
    // scrollback to be written at the wrong width.
    if (!initialCols || !initialRows) {
      return
    }
    // Prevent multiple simultaneous connections
    if (isConnectingRef.current) {
      return
    }

    const connect = () => {
      // Prevent reconnect if already connecting
      if (isConnectingRef.current) {
        return
      }
      isConnectingRef.current = true

      const ws = new WebSocket(WS_URL)

      ws.onopen = () => {
        isConnectingRef.current = false

        // Send init message with currentNote or taskContext from refs
        const note = currentNoteRef.current
        const tc = taskContextRef.current
        ws.send(JSON.stringify({
          type: 'init',
          sessionId,
          dangerousMode,
          taskContext: tc ?? null,
          currentNote: note ? {
            name: note.title,
            folder: note.folder || '',
            content: note.content,
            type: note.type || '',
            projectPath: note.projectPath || '',
          } : null,
          // Spawn-mode props are ONE-SHOT — only the very first init
          // for this hook instance sends them. Subsequent reconnects
          // resend the snapshot from the first init, ignoring any
          // current prop values (which might be stale or refer to the
          // ORIGINAL fork's source — sending again would re-fork).
          ...(initSentRef.current
            ? initSnapshotRef.current
            : (() => {
                initSnapshotRef.current = {
                  resumeFromSessionId: resumeFromSessionId ?? undefined,
                  resumeFork: resumeFork || undefined,
                  attachToSessionId: attachToSessionId ?? undefined,
                  sessionName: sessionName || undefined,
                }
                initSentRef.current = true
                return initSnapshotRef.current
              })()),
          // Initial dimensions — backend uses for the very first
          // daemon Dispatch/Attach so claude never renders at the
          // wrong size.
          cols: initialCols,
          rows: initialRows,
        }))

        // Notify the parent that the WS is up. Fires on every (re)connect
        // so a parent can auto-repaint claude — same effect as the
        // manual Repaint button, just without the click.
        try { onReadyRef.current?.() } catch (e) { console.error('onReady threw:', e) }
      }

      ws.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data)

          if (message.type === 'terminal_output' && message.content) {
            // Decode base64-encoded PTY bytes into Uint8Array.
            // The backend sends raw PTY bytes as base64 to preserve all byte values
            // (including ANSI escape sequences) through the JSON transport layer.
            const binaryStr = atob(message.content)
            const bytes = new Uint8Array(binaryStr.length)
            for (let i = 0; i < binaryStr.length; i++) {
              bytes[i] = binaryStr.charCodeAt(i)
            }
            onOutputRef.current(bytes)
          }
        } catch (error) {
          console.error('Failed to parse WebSocket message:', error)
        }
      }

      ws.onerror = (error) => {
        console.error('Terminal WebSocket error:', error)
        isConnectingRef.current = false
      }

      ws.onclose = () => {
        wsRef.current = null
        isConnectingRef.current = false

        // Only reconnect if not intentionally closed
        if (!intentionalCloseRef.current) {
          reconnectTimeoutRef.current = window.setTimeout(() => {
            connect()
          }, 1000)
        }
      }

      wsRef.current = ws
    }

    // Reset intentional close flag
    intentionalCloseRef.current = false
    connect()

    return () => {
      // Mark as intentional close
      intentionalCloseRef.current = true
      isConnectingRef.current = false

      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
        reconnectTimeoutRef.current = null
      }

      if (wsRef.current) {
        // Remove event handlers before closing to prevent race conditions
        const ws = wsRef.current
        ws.onopen = null
        ws.onmessage = null
        ws.onerror = null
        ws.onclose = null
        ws.close()
        wsRef.current = null
      }
    }
  }, [sessionId, dangerousMode, initialCols, initialRows])

  // Send input to terminal
  const sendInput = useCallback((data: string) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'terminal_input',
        sessionId,
        content: data,
      }))
    } else {
      console.warn(`[WS] Cannot send input - WebSocket not ready (state: ${wsRef.current?.readyState})`)
    }
  }, [sessionId])

  // Send restart message to backend
  const sendRestart = useCallback(() => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      const note = currentNoteRef.current
      wsRef.current.send(JSON.stringify({
        type: 'restart_session',
        sessionId,
        dangerousMode,
        currentNote: note ? {
          name: note.title,
          folder: note.folder || '',
          content: note.content,
          type: note.type || '',
          projectPath: note.projectPath || '',
        } : null,
      }))
    }
  }, [sessionId, dangerousMode])

  // Send PTY resize signal to backend so the process knows the actual terminal dimensions
  const sendResize = useCallback((cols: number, rows: number) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({
        type: 'terminal_resize',
        sessionId,
        cols,
        rows,
      }))
    }
  }, [sessionId])

  // Keep sendResize accessible via ref (used in TerminalChat before ws is ready)
  useEffect(() => {
    sendResizeRef.current = sendResize
  }, [sendResize])

  return { sendInput, sendRestart, sendResize }
}
