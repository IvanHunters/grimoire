import { useState, useEffect, useRef } from 'react'
import { Terminal, ChevronDown, ChevronUp, Settings2 } from 'lucide-react'
import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu'
import { sessionsAPI } from '../../api/sessions'
import { tasksAPI } from '../../api/tasks'
import type { ClaudeSession } from '../../types/claude'
import { SessionStatusPill, formatSessionAge } from './SessionStatusPill'

/**
 * ClaudeSessionsPanel renders the "Claude Sessions" section that lives
 * at the bottom of the main Sidebar AND the TasksPage sidebar. It owns
 * its own data loading, drag-to-reorder, rename, context-menu, and the
 * resize handle above the list. Mount it in any sidebar — the
 * `session-order` localStorage key is shared so the user's manual
 * ordering survives navigation between pages.
 *
 * Two click destinations:
 *   - note-bound session (`note-<noteId>`) → onOpenChatWithNote(noteId)
 *     so the page can open the primary note chat.
 *   - any other id (Quick Terminal, task session, raw daemon UUID) →
 *     onAttachToSession(id, name) — the page should pop ResumeChatModal
 *     in mode='attach' so we connect to the live PTY without spawning.
 */
export interface ClaudeSessionsPanelProps {
  isMobile?: boolean
  /** Currently-attached session — used for the purple-active highlight. */
  activeSessionId?: string
  /** Called when user clicks a non-note session (or when no
   *  onOpenChatWithNote provided). Page should open attach modal. */
  onAttachToSession?: (sessionId: string, name: string) => void
  /** Called when user clicks a `note-<id>` session. */
  onOpenChatWithNote?: (noteId: string) => void
  /** Called after sessionsAPI.deleteSession succeeds (e.g. to close a
   *  currently-open chat panel). */
  onSessionDeleted?: (sessionId: string) => void
  /** Mobile only: close the parent sidebar after a click. */
  onMobileClose?: () => void
}

const LS_COLLAPSED = 'claude-sessions-collapsed'
const LS_HEIGHT = 'claude-sessions-height'
const LS_ORDER = 'session-order' // shared with all sidebars

export function ClaudeSessionsPanel({
  isMobile,
  activeSessionId,
  onAttachToSession,
  onOpenChatWithNote,
  onSessionDeleted,
  onMobileClose,
}: ClaudeSessionsPanelProps) {
  const [sessions, setSessions] = useState<ClaudeSession[]>([])
  // taskTitles maps task-id → human title so note-task-<id> sessions
  // can show their task's real name in the panel instead of the
  // claude-auto-renamed "Load and review task details" that comes
  // back from the daemon. Fetched once on mount, then refreshed when
  // sessions list changes (cheap; /api/tasks returns metadata only).
  const [taskTitles, setTaskTitles] = useState<Record<string, string>>({})
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    return localStorage.getItem(LS_COLLAPSED) === 'true'
  })
  const [height, setHeight] = useState<number>(() => {
    const raw = localStorage.getItem(LS_HEIGHT)
    const n = raw ? parseInt(raw, 10) : 200
    return Number.isFinite(n) && n >= 80 ? n : 200
  })
  const [order, setOrder] = useState<string[]>(() => {
    try { return JSON.parse(localStorage.getItem(LS_ORDER) ?? '[]') } catch { return [] }
  })
  const [dragId, setDragId] = useState<string | null>(null)
  const [dragOverIdx, setDragOverIdx] = useState<number | null>(null)
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')
  const [contextMenu, setContextMenu] = useState<{ visible: boolean; x: number; y: number; items: ContextMenuItem[] }>({
    visible: false, x: 0, y: 0, items: [],
  })
  const resizeRef = useRef<HTMLDivElement>(null)

  // Persist collapse + height changes locally.
  useEffect(() => { localStorage.setItem(LS_COLLAPSED, String(collapsed)) }, [collapsed])
  useEffect(() => { localStorage.setItem(LS_HEIGHT, String(height)) }, [height])

  // Fetch task titles once when there's at least one note-task-* session
  // we can't already label. Refresh on a 60s tick — task titles rarely
  // change and the user can manually rename via context menu anyway.
  useEffect(() => {
    const hasUnknownTaskSession = sessions.some(
      (s) => s.id.startsWith('note-task-') &&
        !taskTitles[s.id.slice('note-task-'.length)],
    )
    if (!hasUnknownTaskSession && Object.keys(taskTitles).length > 0) return
    let cancelled = false
    tasksAPI.list().then((all) => {
      if (cancelled) return
      const map: Record<string, string> = {}
      for (const t of all) {
        if (t.id && t.title) map[t.id] = t.title
      }
      setTaskTitles(map)
    }).catch(() => {})
    return () => { cancelled = true }
  }, [sessions, taskTitles])

  // Load + poll live sessions from /api/sessions (the in-memory
  // manager listing keyed by grimoire id). We use this instead of
  // by-project so:
  //   - `note-task-<taskId>` keys are preserved (NOT replaced by the
  //     daemon's UUID), which means the click handler can find the
  //     right task and show its real title — not the Haiku-renamed
  //     "Load and review task details" name that claude auto-generates.
  //   - `global-<id>` and `note-<noteId>` keys stay grimoire-side too.
  // We then enrich with status fields by joining against by-project on
  // the daemon UUID.
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const [grimoireSessions, byProject] = await Promise.all([
          sessionsAPI.listActiveSessions(),
          sessionsAPI.listByProject().catch(() => [] as Awaited<ReturnType<typeof sessionsAPI.listByProject>>),
        ])
        if (cancelled) return
        // Index by-project entries by their JSONL/daemon UUID so we
        // can copy live tempo/state/detail/needs back onto the
        // grimoire-keyed rows.
        const byUuid: Record<string, typeof byProject[number]> = {}
        for (const item of byProject) {
          byUuid[item.sessionId] = item
        }
        // grimoire-keyed shape directly (ClaudeSession ↔ models.ClaudeSession
        // returned by /api/sessions). Enrich with live status if we can
        // join by the session's UUID — for note-task/note sessions the
        // grimoire id IS keyed differently so the join may miss; that's
        // fine, the row still renders with the name and click works.
        const liveSessions = grimoireSessions.map((s) => {
          // ClaudeSession from backend already carries tempo/state/detail/
          // needs from ListActiveSessions. We ONLY override those fields
          // when a by-project match is found AND its live data is
          // non-empty — otherwise the override would clobber valid backend
          // values with `undefined` (which is what happened for
          // note-task-* sessions where by-project doesn't surface a row
          // keyed by the grimoire id).
          const match = Object.values(byUuid).find((b) =>
            b.daemonShort && b.name === s.name && !!b.live
          )
          if (!match || !match.live) return s
          return {
            ...s,
            tempo: match.live.tempo ?? s.tempo,
            state: match.live.state ?? s.state,
            detail: match.live.detail ?? s.detail,
            needs: match.live.needs ?? s.needs,
          }
        })
        setSessions(liveSessions)
      } catch (err) {
        console.error('Failed to load Claude sessions:', err)
      }
    }
    load()
    const interval = window.setInterval(load, 3000)
    const refreshHandler = () => load()
    window.addEventListener('claude-sessions-refresh', refreshHandler)
    return () => {
      cancelled = true
      clearInterval(interval)
      window.removeEventListener('claude-sessions-refresh', refreshHandler)
    }
  }, [])

  // Resize: vertical drag pinned to the bottom of the parent sidebar.
  const handleResizeStart = (e: React.MouseEvent) => {
    e.preventDefault()
    const startY = e.clientY
    const startHeight = height
    const onMove = (mv: MouseEvent) => {
      const delta = startY - mv.clientY
      const next = Math.max(80, Math.min(600, startHeight + delta))
      setHeight(next)
    }
    const onUp = () => {
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }

  const handleCommitRename = async (sessionId: string, newName: string) => {
    const trimmed = newName.trim()
    setRenamingId(null)
    if (!trimmed) return
    try {
      await sessionsAPI.renameSession(sessionId, trimmed)
      setSessions((prev) => prev.map((s) => (s.id === sessionId ? { ...s, name: trimmed } : s)))
    } catch (err) {
      console.error('rename failed', err)
    }
  }

  const handleDelete = async (sessionId: string) => {
    try {
      // deleteTranscript:true so the JSONL also goes — otherwise the
      // historical row keeps coming back on the next poll, looking like
      // "delete didn't work".
      await sessionsAPI.deleteSession(sessionId, { deleteTranscript: true })
      setSessions((prev) => prev.filter((s) => s.id !== sessionId))
      onSessionDeleted?.(sessionId)
      // Drop matching tab from GlobalTerminalPanel's localStorage so the
      // panel doesn't restore the (now-dead) tab on next open.
      try {
        const raw = localStorage.getItem('global-terminal-tabs')
        if (raw) {
          const tabs = JSON.parse(raw) as Array<{ sessionId: string; label: string }>
          const filtered = tabs.filter((t) => t.sessionId !== sessionId)
          if (filtered.length !== tabs.length) {
            localStorage.setItem('global-terminal-tabs', JSON.stringify(filtered))
            window.dispatchEvent(new CustomEvent('global-terminal-tabs-changed'))
          }
        }
      } catch {}
    } catch (err) {
      console.error('delete failed', err)
      alert('Failed to delete session')
    }
  }

  const handleSessionContextMenu = (e: React.MouseEvent, session: ClaudeSession) => {
    e.preventDefault()
    e.stopPropagation()
    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      items: [
        {
          text: 'Rename',
          icon: 'edit',
          action: () => {
            setRenamingId(session.id)
            setRenameValue(session.name || '')
          },
        },
        {
          text: 'Fork…',
          icon: 'code-branch',
          action: () => {
            const suggested = session.name ? `${session.name} (fork)` : 'fork'
            const name = window.prompt('Имя для форка:', suggested)
            if (name == null) return
            const trimmed = name.trim()
            if (!trimmed) return
            window.dispatchEvent(new CustomEvent('fork-session-request', {
              detail: { sourceId: session.id, name: trimmed },
            }))
          },
        },
        {
          text: 'Export .jsonl',
          icon: 'download',
          action: () => {
            // Stream the JSONL straight from the backend via
            // Content-Disposition — no JS heap allocation for big files.
            const a = document.createElement('a')
            a.href = `/api/sessions/${session.id}/jsonl`
            a.download = `${session.name || session.id}.jsonl`
            document.body.appendChild(a)
            a.click()
            a.remove()
          },
        },
        {
          text: 'Compact + restart',
          icon: 'archive',
          action: async () => {
            try {
              const r = await fetch(`/api/sessions/${encodeURIComponent(session.id)}/compact`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ generate_ledger: true }),
              })
              if (r.status === 404) {
                // No on-disk transcript yet (fresh session, zero
                // turns). Compact has nothing to shrink — silently
                // skip and just restart. User intent ≈ "give me a
                // clean state for this session", which a plain
                // restart already provides.
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
              }
              window.dispatchEvent(new CustomEvent('claude-session-restart-request', {
                detail: { sessionId: session.id },
              }))
              window.dispatchEvent(new CustomEvent('claude-sessions-refresh'))
            } catch (err) {
              console.error('compact failed', err)
              alert('Compact failed: ' + (err as Error).message)
            }
          },
        },
        {
          text: 'Kill Session',
          icon: 'trash',
          action: () => handleDelete(session.id),
          danger: true,
        },
      ],
    })
  }

  // Three-tier sort:
  //   0. NEEDS-YOU   — session is blocked waiting on user input (highest priority)
  //   1. HAS-STATUS  — any known state/tempo (running/working/done/active/idle/...)
  //   2. NO-STATUS   — session backend hasn't reported status yet
  // Within each tier, sort by lastActivity DESC (newest first).
  // No-status tier additionally respects user's saved drag order so old
  // sessions don't reshuffle on every poll.
  // needsYou MUST match the pill label logic so sort tier and visible
  // badge agree. Pill: tempo='active' beats state='blocked' → "working".
  // So a session is "needs you" ONLY when tempo isn't actively
  // generating: tempo=blocked OR (state=blocked AND tempo != active).
  const needsYou = (s: ClaudeSession) => {
    if (s.tempo === 'active') return false // pill says "working" — match it
    if (s.tempo === 'blocked') return true
    if (s.state === 'blocked') return true
    const needsField = (s as ClaudeSession & { needs?: string }).needs
    return typeof needsField === 'string' && needsField.trim() !== ''
  }
  const isWorking = (s: ClaudeSession) => {
    if (needsYou(s)) return false
    return s.tempo === 'active'
  }
  const isReady = (s: ClaudeSession) => {
    if (needsYou(s) || isWorking(s)) return false
    const st = (s.state || '').trim()
    return st === 'done' || st === 'running' || st === 'working' || st === 'ready'
  }
  const hasStatus = (s: ClaudeSession) => {
    // "everything else with status" — failed/stopped/unknown-but-set.
    if (needsYou(s) || isWorking(s) || isReady(s)) return false
    const st = (s.state || '').trim()
    const tp = (s.tempo || '').trim()
    if (!st && !tp) return false
    if (st === 'unknown' && (!tp || tp === 'unknown')) return false
    return true
  }
  // Bucket lastActivity into 60-second windows so continuously-emitting
  // sessions don't reshuffle on every 3s poll. Two sessions whose activity
  // is within the same 60s bucket are tied; we tiebreak by createdAt
  // (stable across polls). The visible result: a freshly-active session
  // appears near the top once and stays there until another session
  // gets a notably more recent burst (>60s newer).
  const BUCKET_MS = 60_000
  const ts = (d?: string) => (d ? new Date(d).getTime() : 0)
  const byActivityDesc = (a: ClaudeSession, b: ClaudeSession) => {
    const ba = Math.floor(ts(a.lastActivity) / BUCKET_MS)
    const bb = Math.floor(ts(b.lastActivity) / BUCKET_MS)
    if (ba !== bb) return bb - ba // newer bucket first
    // Tiebreak by createdAt (older first → stable). a/b's createdAt may
    // be missing for daemon-rehydrated sessions — fall back to id for
    // determinism.
    const ca = ts(a.createdAt)
    const cb = ts(b.createdAt)
    if (ca !== cb) return ca - cb
    return a.id < b.id ? -1 : 1
  }
  // needs-you tier: sort by ACTUAL lastActivity (no bucket smoothing).
  // When a session becomes blocked-waiting, the user wants it visible
  // immediately at #1 — even if another needs-you session got blocked
  // 30 seconds earlier. Stability concerns from byActivityDesc don't
  // apply here because needs-you events are rare (one per turn, not
  // every PTY byte).
  const byLastActivityRaw = (a: ClaudeSession, b: ClaudeSession) =>
    ts(b.lastActivity) - ts(a.lastActivity)
  // Priority tiers (top → bottom in sidebar):
  //   1. NEEDS YOU — blocked / awaiting user input
  //   2. WORKING   — tempo=active, claude is generating
  //   3. READY     — done/running/working but idle (no active turn)
  //   4. OTHER     — failed / stopped / unknown-but-set status
  //   5. NO-STATUS — manual drag-order preserved + tail of newcomers
  // Within each tier sort by lastActivity desc (most recent first).
  const tierNeedsYou = sessions.filter(needsYou).sort(byLastActivityRaw)
  const tierWorking = sessions.filter(isWorking).sort(byActivityDesc)
  const tierReady = sessions.filter(isReady).sort(byActivityDesc)
  const tierOther = sessions.filter(hasStatus).sort(byActivityDesc)
  const tierNoStatus = sessions.filter(
    (s) => !needsYou(s) && !isWorking(s) && !isReady(s) && !hasStatus(s),
  )
  const orderedNoStatus: ClaudeSession[] = [
    ...(order.map((id) => tierNoStatus.find((s) => s.id === id)).filter(Boolean) as ClaudeSession[]),
    ...tierNoStatus.filter((s) => !order.includes(s.id)),
  ]
  const orderedSessions: ClaudeSession[] = [
    ...tierNeedsYou,
    ...tierWorking,
    ...tierReady,
    ...tierOther,
    ...orderedNoStatus,
  ]

  const handleDragStart = (id: string) => setDragId(id)
  const handleDragOver = (e: React.DragEvent, idx: number) => {
    e.preventDefault()
    setDragOverIdx(idx)
  }
  const handleDrop = (e: React.DragEvent, targetIdx: number) => {
    e.preventDefault()
    if (!dragId) return
    const ids = orderedSessions.map((s) => s.id)
    const fromIdx = ids.indexOf(dragId)
    if (fromIdx === -1 || fromIdx === targetIdx) {
      setDragId(null); setDragOverIdx(null); return
    }
    ids.splice(fromIdx, 1)
    ids.splice(targetIdx, 0, dragId)
    setOrder(ids)
    localStorage.setItem(LS_ORDER, JSON.stringify(ids))
    setDragId(null)
    setDragOverIdx(null)
  }

  const handleClick = (session: ClaudeSession, sessionName: string) => {
    if (renamingId === session.id) return
    // Note-bound chats go through ChatPanel via onOpenChatWithNote.
    // Anything else (global-*, task-*, raw daemon UUIDs) → attach modal.
    if (session.id.startsWith('note-') && !session.id.startsWith('note-task-')) {
      const noteId = session.id.replace('note-', '')
      onOpenChatWithNote?.(noteId)
    } else {
      onAttachToSession?.(session.id, sessionName)
    }
    if (isMobile) onMobileClose?.()
  }

  return (
    <>
      {/* Resize handle (above the section) */}
      {!collapsed && (
        <div
          ref={resizeRef}
          className="h-3 flex items-center justify-center cursor-ns-resize group flex-shrink-0"
          onMouseDown={handleResizeStart}
        >
          <div className="w-8 h-px bg-white/[0.08] group-hover:bg-cyan-500/40 transition-colors rounded-full" />
        </div>
      )}

      {/* Claude Sessions section */}
      <div className="border-t border-white/[0.06] flex-shrink-0">
        {/* Header */}
        <div
          className="flex items-center justify-between px-4 py-2.5 hover:bg-white/[0.02] cursor-pointer transition"
          onClick={() => setCollapsed((v) => !v)}
        >
          <div className="flex items-center gap-2">
            <Terminal className="w-3.5 h-3.5 text-purple-500" />
            <span className="text-[10px] font-mono font-semibold tracking-widest text-slate-600 uppercase">
              Claude Sessions
            </span>
            {sessions.length > 0 && (
              <span className="text-[10px] font-mono bg-purple-500/10 text-purple-400 px-1.5 py-0.5 rounded border border-purple-500/20">
                {sessions.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={(e) => {
                e.stopPropagation()
                window.dispatchEvent(new CustomEvent('open-sessions-modal'))
              }}
              title="Open sessions (Cmd+K)"
              className="p-1 text-slate-700 hover:text-purple-400 transition-colors rounded"
            >
              <Settings2 className="w-3 h-3" />
            </button>
            {collapsed ? (
              <ChevronDown className="w-3.5 h-3.5 text-slate-600" />
            ) : (
              <ChevronUp className="w-3.5 h-3.5 text-slate-600" />
            )}
          </div>
        </div>

        {/* List */}
        {!collapsed && (
          <div className="overflow-y-auto px-2 pb-2" style={{ height: `${height}px` }}>
            {sessions.length === 0 ? (
              <div className="text-[10px] font-mono text-slate-700 px-2 py-4 text-center tracking-wider uppercase">
                no active sessions
              </div>
            ) : (
              <div className="space-y-0.5" onDragLeave={() => setDragOverIdx(null)}>
                {orderedSessions.map((session, idx) => {
                  // Display name priority:
                  //   1. For note-task-<id> sessions: REAL task title
                  //      (we look it up via /api/tasks). claude tends to
                  //      auto-rename these to generic "Load and review
                  //      task details" via Haiku — the user wants to
                  //      see THEIR task title back.
                  //   2. Whatever the session.name field has (user
                  //      rename or claude ai-title).
                  //   3. Friendly fallback by id prefix.
                  let sessionName: string
                  const taskId = session.id.startsWith('note-task-')
                    ? session.id.slice('note-task-'.length)
                    : null
                  if (taskId && taskTitles[taskId]) {
                    sessionName = taskTitles[taskId]
                  } else if (session.name && session.name !== 'Terminal Session' && session.name !== '(unnamed)') {
                    sessionName = session.name
                  } else if (session.id.startsWith('note-task-')) {
                    sessionName = 'Task Session'
                  } else if (session.id.startsWith('global-')) {
                    sessionName = 'Quick Terminal'
                  } else {
                    sessionName = session.id.slice(0, 16)
                  }

                  const isActive = activeSessionId === session.id
                  const isRenaming = renamingId === session.id
                  return (
                    <div key={session.id}>
                      {dragOverIdx === idx && dragId !== session.id && (
                        <div className="h-px mx-2 mb-0.5 bg-cyan-500/60 rounded-full" />
                      )}
                      <div
                        draggable={!isRenaming}
                        onDragStart={() => handleDragStart(session.id)}
                        onDragEnd={() => { setDragId(null); setDragOverIdx(null) }}
                        onDragOver={(e) => handleDragOver(e, idx)}
                        onDrop={(e) => handleDrop(e, idx)}
                        onContextMenu={(e) => handleSessionContextMenu(e, session)}
                        onClick={() => handleClick(session, sessionName)}
                        className={`w-full flex items-start gap-2 px-2 py-1.5 rounded transition text-left select-none cursor-pointer ${
                          dragId === session.id ? 'opacity-40' : ''
                        } ${
                          isActive
                            ? 'bg-purple-500/10 border border-purple-500/20'
                            : 'hover:bg-white/[0.03] border border-transparent'
                        }`}
                      >
                        <Terminal className={`w-3 h-3 flex-shrink-0 mt-0.5 cursor-grab active:cursor-grabbing ${
                          isActive ? 'text-purple-400' : 'text-purple-600'
                        }`} />
                        <div className="flex-1 min-w-0">
                          {isRenaming ? (
                            <input
                              autoFocus
                              value={renameValue}
                              onChange={(e) => setRenameValue(e.target.value)}
                              onBlur={() => handleCommitRename(session.id, renameValue)}
                              onKeyDown={(e) => {
                                if (e.key === 'Enter') handleCommitRename(session.id, renameValue)
                                if (e.key === 'Escape') setRenamingId(null)
                                e.stopPropagation()
                              }}
                              onClick={(e) => e.stopPropagation()}
                              className="w-full text-xs font-mono bg-transparent border-b border-cyan-500/50 text-cyan-300 outline-none pb-px"
                            />
                          ) : (
                            <div className="flex items-center gap-1.5 min-w-0">
                              <div className={`flex-1 text-xs font-mono truncate ${isActive ? 'text-purple-300' : 'text-slate-400'}`}>
                                {sessionName}
                              </div>
                              <SessionStatusPill state={session.state} tempo={session.tempo} detail={session.detail} needs={session.needs} />
                            </div>
                          )}
                          {session.workingDir && (
                            <div className="text-[10px] font-mono text-slate-700 truncate">
                              {session.workingDir}
                            </div>
                          )}
                          <div className="flex items-center gap-1.5 mt-0.5">
                            {session.createdAt && (
                              <span className="text-[9px] font-mono text-slate-800" title={`Created: ${new Date(session.createdAt).toLocaleString()}`}>
                                {'+'}{formatSessionAge(session.createdAt)}
                              </span>
                            )}
                            {session.lastActivity && (
                              <span className="text-[9px] font-mono text-slate-700" title={`Last active: ${new Date(session.lastActivity).toLocaleString()}`}>
                                {'·'} {formatSessionAge(session.lastActivity)} ago
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </div>

      <ContextMenu
        visible={contextMenu.visible}
        x={contextMenu.x}
        y={contextMenu.y}
        items={contextMenu.items}
        onClose={() => setContextMenu((c) => ({ ...c, visible: false }))}
      />
    </>
  )
}

export default ClaudeSessionsPanel
