import { useEffect, useRef, useState } from 'react'
import { X, RefreshCw, Pencil, Trash2, Check, Plus, Filter, Upload, Search } from 'lucide-react'
import { sessionsAPI, type SessionListItem, type SessionLiveState, type ImportFileResult, type SessionSearchHit } from '../../api/sessions'

interface SessionsModalProps {
  visible: boolean
  onClose: () => void
  /** If set, only show sessions for this cwd. Omit for all-projects view. */
  cwd?: string
  /**
   * The cwd associated with the current note (projectPath). When set,
   * the modal renders a "this project only" toggle that filters the
   * list to that cwd. Pass empty/undefined to hide the toggle.
   */
  currentProjectCwd?: string
  /**
   * Called when the user clicks a session row. isLive tells the parent
   * whether the session has a live daemon worker — useful for choosing
   * between "view transcript" vs "attach to running session".
   */
  onOpenSession?: (sessionId: string, isLive: boolean, name: string) => void
}

/**
 * SessionsModal lists every Claude session known to the system: live ones
 * from the daemon and historical JSONL transcripts on disk. Click a card
 * to inspect (future: open in chat / view transcript / delete).
 *
 * First version is read-only — no resume, no rename, no delete yet. Those
 * land in a follow-up once the core "see my sessions" UX is validated.
 */
function SessionsModal({ visible, onClose, cwd, currentProjectCwd, onOpenSession }: SessionsModalProps) {
  const [sessions, setSessions] = useState<SessionListItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Inline transcript search. Empty query → normal session list.
  // Non-empty query → search results replace the list.
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SessionSearchHit[]>([])
  const [searching, setSearching] = useState(false)
  const searchInputRef = useRef<HTMLInputElement>(null)
  // When projectFilter is true and currentProjectCwd is set, list is
  // scoped to that cwd. Otherwise we show every session on disk.
  // The hard `cwd` prop (if any) wins over this — that's caller-forced.
  const [projectFilter, setProjectFilter] = useState(false)
  // When true, list is filtered to imported-only sessions. Cheap
  // client-side filter on top of the standard listing.
  const [importedOnly, setImportedOnly] = useState(false)
  const [dragOver, setDragOver] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importSummary, setImportSummary] = useState<{ ok: number; failed: number } | null>(null)

  // Compute the actual cwd used for the listing query.
  const effectiveCwd = cwd ?? (projectFilter && currentProjectCwd ? currentProjectCwd : undefined)

  // sessionId → name lookup so search-result group headers can show
  // the human-readable session title instead of a hex prefix.
  const sessionsById = sessions.reduce<Record<string, string>>((acc, s) => {
    if (s.name) acc[s.sessionId] = s.name
    return acc
  }, {})

  // Name-matching sessions for the search input. When query is set,
  // we show these matching cards at the top alongside content hits
  // below — covering both "I remember the name" and "I remember a
  // word from the conversation" use cases.
  const nameMatches = query.trim().length >= 2
    ? sessions.filter((s) => {
        const q = query.trim().toLowerCase()
        return (
          s.name?.toLowerCase().includes(q) ||
          s.firstPrompt?.toLowerCase().includes(q) ||
          s.cwd?.toLowerCase().includes(q) ||
          s.sessionId.toLowerCase().includes(q)
        )
      })
    : []

  // A session that matches BOTH by name and by transcript content would
  // otherwise render twice — once as a name card, once in the content
  // hits. Drop content hits for sessions already shown as name matches
  // so each session appears exactly once in the search results.
  const nameMatchIds = new Set(nameMatches.map((s) => s.sessionId))
  const contentHits = hits.filter((h) => !nameMatchIds.has(h.sessionId))

  useEffect(() => {
    if (!visible) return
    let cancelled = false
    const fetchList = (initial: boolean) => {
      if (initial) setLoading(true)
      setError(null)
      sessionsAPI
        .listByProject(effectiveCwd)
        .then((items) => {
          if (!cancelled) setSessions(items)
        })
        .catch((e) => {
          if (!cancelled) setError(String(e?.message ?? e))
        })
        .finally(() => {
          if (!cancelled && initial) setLoading(false)
        })
    }
    fetchList(true)
    // Quiet 5s poll so newly-resumed sessions appear in the Active
    // section without the user pressing Refresh. Cheap (one HTTP call,
    // listing is in-memory + JSONL headers).
    const interval = window.setInterval(() => fetchList(false), 5000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [visible, effectiveCwd])

  // Autofocus search input on modal open. Esc to close already wired
  // at the overlay; search-clear via the small X inside the field.
  useEffect(() => {
    if (!visible) {
      setQuery('')
      setHits([])
      return
    }
    const id = setTimeout(() => searchInputRef.current?.focus(), 50)
    return () => clearTimeout(id)
  }, [visible])

  // Debounced transcript search. Runs while modal is visible and query
  // has at least 2 chars. Scope by effectiveCwd so the search respects
  // the project filter toggle.
  useEffect(() => {
    if (!visible) return
    if (query.trim().length < 2) {
      setHits([])
      setSearching(false)
      return
    }
    let cancelled = false
    setSearching(true)
    const timer = setTimeout(() => {
      sessionsAPI
        .search(query.trim(), { cwd: effectiveCwd, limit: 80 })
        .then((results) => {
          if (!cancelled) setHits(results)
        })
        .catch((e) => {
          if (!cancelled) setError(String(e?.message ?? e))
        })
        .finally(() => {
          if (!cancelled) setSearching(false)
        })
    }, 250)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [visible, query, effectiveCwd])

  const handleRefresh = () => {
    setLoading(true)
    sessionsAPI
      .listByProject(effectiveCwd)
      .then(setSessions)
      .catch((e) => setError(String(e?.message ?? e)))
      .finally(() => setLoading(false))
  }

  const handleImportFiles = async (files: File[]) => {
    if (files.length === 0) return
    // Filter to .jsonl — backend rejects anything else but it's nicer
    // to short-circuit obvious mistakes in the browser.
    const jsonlFiles = files.filter((f) => f.name.endsWith('.jsonl'))
    if (jsonlFiles.length === 0) {
      setError('drop .jsonl transcripts only')
      return
    }
    setImporting(true)
    setError(null)
    try {
      const results: ImportFileResult[] = await sessionsAPI.importTranscripts(jsonlFiles)
      const ok = results.filter((r) => r.result).length
      const failed = results.filter((r) => r.error).length
      setImportSummary({ ok, failed })
      // Surface first failure detail if any.
      const firstErr = results.find((r) => r.error)
      if (firstErr) {
        setError(`${firstErr.filename}: ${firstErr.error}`)
      }
      handleRefresh()
      // Clear summary after a few seconds so it doesn't linger.
      setTimeout(() => setImportSummary(null), 4000)
    } catch (e) {
      setError(String((e as Error)?.message ?? e))
    } finally {
      setImporting(false)
    }
  }

  const handleDragOver = (e: React.DragEvent) => {
    if (e.dataTransfer.types.includes('Files')) {
      e.preventDefault()
      setDragOver(true)
    }
  }

  const handleDragLeave = (e: React.DragEvent) => {
    // Only un-highlight when leaving the outer container, not when
    // crossing nested children.
    if (e.currentTarget === e.target) setDragOver(false)
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOver(false)
    const files = Array.from(e.dataTransfer.files)
    handleImportFiles(files)
  }

  const handleFilePicker = () => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.jsonl,application/jsonl,application/x-ndjson'
    input.multiple = true
    input.onchange = () => {
      const files = input.files ? Array.from(input.files) : []
      handleImportFiles(files)
    }
    input.click()
  }

  if (!visible) return null

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === e.currentTarget) onClose()
  }

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-start justify-center z-[2000] pt-16"
      onClick={handleOverlayClick}
    >
      <div
        className={`bg-[#0a0b10] border rounded-lg shadow-2xl w-full max-w-3xl mx-4 max-h-[80vh] flex flex-col overflow-hidden relative transition-colors ${dragOver ? 'border-amber-500/60' : 'border-white/[0.09]'}`}
        onClick={(e) => e.stopPropagation()}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        {dragOver && (
          <div className="absolute inset-0 z-10 bg-amber-500/5 border-2 border-dashed border-amber-500/60 rounded-lg flex items-center justify-center pointer-events-none">
            <div className="flex flex-col items-center gap-2 text-amber-400 font-mono text-xs uppercase tracking-widest">
              <Upload className="w-6 h-6" />
              drop .jsonl transcripts to import
            </div>
          </div>
        )}
        <div className="flex items-center justify-between px-5 py-3 border-b border-white/[0.06] flex-shrink-0">
          <span className="text-[10px] font-mono font-semibold tracking-widest text-cyan-400 uppercase truncate">
            Sessions {effectiveCwd ? `· ${effectiveCwd}` : '· all projects'}
          </span>
          <div className="flex items-center gap-1 flex-shrink-0">
            <button
              onClick={handleFilePicker}
              disabled={importing}
              className="p-1.5 text-slate-500 hover:text-amber-400 hover:bg-white/5 transition-colors rounded disabled:opacity-40"
              title="Import .jsonl transcripts (or drop them on this modal)"
            >
              <Upload className={`w-3.5 h-3.5 ${importing ? 'animate-pulse' : ''}`} />
            </button>
            {currentProjectCwd && !cwd && (
              <button
                onClick={() => setProjectFilter((v) => !v)}
                className={`p-1.5 transition-colors rounded ${projectFilter ? 'text-amber-400 bg-amber-500/10' : 'text-slate-500 hover:text-slate-300'}`}
                title={projectFilter ? 'Showing this project only — click to show all' : `Show only sessions in ${currentProjectCwd}`}
              >
                <Filter className="w-3.5 h-3.5" />
              </button>
            )}
            <button
              onClick={() => setImportedOnly((v) => !v)}
              className={`p-1.5 transition-colors rounded ${importedOnly ? 'text-fuchsia-400 bg-fuchsia-500/10' : 'text-slate-500 hover:text-slate-300'}`}
              title={importedOnly ? 'Showing imported only — click to show all' : 'Show only imported sessions'}
            >
              <span className="font-mono text-[10px] tracking-wider uppercase font-semibold">imp</span>
            </button>
            <button
              onClick={handleRefresh}
              className="p-1.5 text-slate-500 hover:text-slate-300 transition-colors rounded"
              title="Refresh"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="p-1.5 text-slate-500 hover:text-slate-300 transition-colors rounded"
              title="Close"
            >
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        </div>

        {/* Inline search input — empty query shows the list, non-empty
            replaces it with transcript-search hits. Scoped to the
            current filter (project / all). */}
        <div className="px-4 py-2 border-b border-white/[0.06] flex items-center gap-2 flex-shrink-0">
          <Search className="w-3.5 h-3.5 text-slate-500 flex-shrink-0" />
          <input
            ref={searchInputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search inside transcripts… (or leave empty to browse list)"
            className="flex-1 bg-transparent outline-none font-mono text-xs text-slate-200 placeholder:text-slate-600"
          />
          {searching && (
            <span className="font-mono text-[9px] text-slate-500 animate-pulse uppercase tracking-wider">
              searching…
            </span>
          )}
          {query && (
            <button
              onClick={() => setQuery('')}
              className="p-1 text-slate-500 hover:text-slate-300 rounded"
              title="Clear search"
            >
              <X className="w-3 h-3" />
            </button>
          )}
        </div>

        <div className="flex-1 overflow-y-auto">
          {importSummary && (
            <div className="px-5 py-2 text-xs font-mono border-b border-white/[0.04] flex items-center gap-2">
              {importSummary.ok > 0 && (
                <span className="text-emerald-400">
                  ✓ imported {importSummary.ok}
                </span>
              )}
              {importSummary.failed > 0 && (
                <span className="text-red-400">
                  ✗ failed {importSummary.failed}
                </span>
              )}
            </div>
          )}
          {error && (
            <div className="px-5 py-3 text-xs font-mono text-red-400">
              error: {error}
            </div>
          )}
          {loading && sessions.length === 0 && (
            <div className="px-5 py-8 text-center text-xs font-mono text-slate-500">
              loading…
            </div>
          )}
          {!loading && sessions.length === 0 && !error && !query && (
            <div className="px-5 py-8 text-center text-xs font-mono text-slate-500">
              No sessions found.
            </div>
          )}

          {query && query.trim().length < 2 && (
            <div className="px-5 py-8 text-center text-xs font-mono text-slate-600">
              type at least 2 characters
            </div>
          )}
          {query && query.trim().length >= 2 && !searching && contentHits.length === 0 && nameMatches.length === 0 && (
            <div className="px-5 py-8 text-center text-xs font-mono text-slate-500">
              no matches
            </div>
          )}

          {/* Name / cwd / prompt matches — sessions whose card text
              hits the query directly. Shown above content hits since
              "I remember the name" is the more common case. */}
          {query && nameMatches.length > 0 && (
            <>
              <div className="px-4 py-2 text-[10px] font-mono text-slate-500 uppercase tracking-widest border-b border-white/[0.04]">
                Sessions matching name · {nameMatches.length}
              </div>
              <ul className="divide-y divide-white/[0.04]">
                {nameMatches.map((s) => (
                  <SessionRow
                    key={`nm-${s.sessionId}`}
                    item={s}
                    onClick={onOpenSession ? () => onOpenSession(s.sessionId, !!s.live, s.name) : undefined}
                    onRenamed={(newName) =>
                      setSessions((prev) =>
                        prev.map((p) => (p.sessionId === s.sessionId ? { ...p, name: newName } : p)),
                      )
                    }
                    onDeleted={() =>
                      setSessions((prev) => prev.filter((p) => p.sessionId !== s.sessionId))
                    }
                  />
                ))}
              </ul>
            </>
          )}

          {query && contentHits.length > 0 && (
            <>
              <div className="px-4 py-2 text-[10px] font-mono text-slate-500 uppercase tracking-widest border-b border-t border-white/[0.04]">
                Content matches · {contentHits.length}
              </div>
              <SearchResultsBlock
                hits={contentHits}
                query={query.trim()}
                onOpenSession={onOpenSession}
                // Pass the names we already have so the group header
                // can show "Equeo Project Overview" instead of an
                // 8-hex prefix nobody can recognise at a glance.
                sessionNames={sessionsById}
                // Live-state lookup so the click can pass isLive (so
                // backend knows attach vs resume).
                sessionsByIdLive={sessions.reduce<Record<string, boolean>>((acc, s) => {
                  if (s.live) acc[s.sessionId] = true
                  return acc
                }, {})}
                onRenamed={(sid, newName) => {
                  // Optimistic update of the local sessions list so
                  // the next render shows the new name without waiting
                  // for the 5s poll. Hits keep their old sessionName
                  // from the search response — that's stale but it's
                  // overridden by sessionNames map (which we update).
                  setSessions((prev) =>
                    prev.map((s) => (s.sessionId === sid ? { ...s, name: newName } : s)),
                  )
                }}
                onDeleted={(sid) => {
                  // Drop the session from local state immediately, and
                  // also remove any hits pointing to it so the group
                  // card disappears.
                  setSessions((prev) => prev.filter((s) => s.sessionId !== sid))
                  setHits((prev) => prev.filter((h) => h.sessionId !== sid))
                }}
              />
            </>
          )}

          {/* Split into live / history sections when no query is set.
              Backend already sorts live-first, so we just look for the
              boundary and inject a header in the middle. */}
          {(() => {
            const visibleSessions = importedOnly ? sessions.filter((s) => s.imported) : sessions
            const liveCount = visibleSessions.filter((s) => s.live).length
            return (
              <>
                {!query && liveCount > 0 && (
                  <div className="px-4 py-2 text-[10px] font-mono uppercase tracking-widest text-emerald-400/80 border-b border-emerald-500/15 bg-emerald-500/[0.03] sticky top-0 z-[1]">
                    ● Active now · {liveCount}
                  </div>
                )}
                <ul className={`divide-y divide-white/[0.04] ${query ? 'hidden' : ''}`}>
                  {visibleSessions.flatMap((s, idx) => {
                    const showHistoryHeader = idx > 0 && !s.live && visibleSessions[idx - 1].live
                    const row = (
                      <SessionRow
                        key={s.sessionId}
                        item={s}
                        onClick={onOpenSession ? () => onOpenSession(s.sessionId, !!s.live, s.name) : undefined}
                        onRenamed={(newName) =>
                          setSessions((prev) =>
                            prev.map((p) => (p.sessionId === s.sessionId ? { ...p, name: newName } : p)),
                          )
                        }
                        onDeleted={() =>
                          setSessions((prev) => prev.filter((p) => p.sessionId !== s.sessionId))
                        }
                      />
                    )
                    if (!showHistoryHeader) return [row]
                    return [
                      <li
                        key={`hist-hdr-${s.sessionId}`}
                        className="px-4 py-2 text-[10px] font-mono uppercase tracking-widest text-slate-600 border-b border-white/[0.04] bg-white/[0.01]"
                      >
                        ○ History · {visibleSessions.length - idx}
                      </li>,
                      row,
                    ]
                  })}
                  {importedOnly && visibleSessions.length === 0 && (
                    <li className="px-4 py-8 text-center font-mono text-[11px] text-slate-600">
                      No imported sessions.
                    </li>
                  )}
                </ul>
              </>
            )
          })()}
        </div>

        <div className="border-t border-white/[0.06] px-5 py-2 flex-shrink-0">
          <span className="text-[10px] font-mono text-slate-600">
            {sessions.length} session{sessions.length === 1 ? '' : 's'}
          </span>
        </div>
      </div>
    </div>
  )
}

interface SessionRowProps {
  item: SessionListItem
  onClick?: () => void
  onRenamed?: (newName: string) => void
  onDeleted?: () => void
}

function SessionRow({ item, onClick, onRenamed, onDeleted }: SessionRowProps) {
  const isLive = !!item.live
  const lastActivity = new Date(item.lastActivity)
  const clickable = !!onClick

  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState(item.name)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [busy, setBusy] = useState(false)

  // Reset edit value if the underlying name changed externally.
  useEffect(() => {
    setEditValue(item.name)
  }, [item.name])

  const handleSaveName = async (e: React.MouseEvent | React.KeyboardEvent) => {
    e.stopPropagation()
    const trimmed = editValue.trim()
    if (!trimmed || trimmed === item.name) {
      setEditing(false)
      setEditValue(item.name)
      return
    }
    setBusy(true)
    try {
      await sessionsAPI.renameSession(item.sessionId, trimmed)
      onRenamed?.(trimmed)
      setEditing(false)
    } catch (e) {
      console.error('rename failed', e)
    } finally {
      setBusy(false)
    }
  }

  const handleDelete = async (e: React.MouseEvent) => {
    e.stopPropagation()
    setBusy(true)
    try {
      // We delete the transcript too — historical browse stays useful
      // when the user explicitly says "remove this session".
      await sessionsAPI.deleteSession(item.sessionId, { deleteTranscript: true })
      onDeleted?.()
    } catch (e) {
      console.error('delete failed', e)
    } finally {
      setBusy(false)
      setConfirmingDelete(false)
    }
  }

  const stopPropagation = (e: React.MouseEvent) => e.stopPropagation()

  const handleRowClick = () => {
    if (editing || confirmingDelete) return
    onClick?.()
  }

  return (
    <li
      className={`px-5 py-3 hover:bg-white/[0.02] transition-colors group ${clickable && !editing && !confirmingDelete ? 'cursor-pointer' : ''}`}
      onClick={handleRowClick}
    >
      <div className="flex items-start gap-3">
        <LiveDot live={item.live} />
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2 mb-0.5">
            {editing ? (
              <input
                value={editValue}
                onChange={(e) => setEditValue(e.target.value)}
                onClick={stopPropagation}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleSaveName(e)
                  if (e.key === 'Escape') {
                    setEditing(false)
                    setEditValue(item.name)
                  }
                }}
                autoFocus
                className="font-mono text-sm bg-cyan-500/10 border border-cyan-500/30 text-slate-200 px-1.5 py-0.5 rounded outline-none"
                style={{ minWidth: 200 }}
              />
            ) : (
              <span className="font-mono text-sm text-slate-200 truncate">
                {item.name || '(unnamed)'}
              </span>
            )}
            {item.daemonShort && (
              <span className="font-mono text-[10px] text-cyan-500/70 tracking-wide">
                #{item.daemonShort}
              </span>
            )}
            {item.imported && (
              <span
                className="font-mono text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded bg-fuchsia-500/10 text-fuchsia-400 border border-fuchsia-500/30"
                title="Imported from a .jsonl upload"
              >
                imported
              </span>
            )}
          </div>
          <div className="font-mono text-[10px] text-slate-500 truncate">
            {item.cwd || '(no cwd)'}
            {item.gitBranch ? ` · ${item.gitBranch}` : ''}
          </div>
          {item.firstPrompt && (
            <div className="font-mono text-[11px] text-slate-400 mt-1 line-clamp-2">
              {item.firstPrompt}
            </div>
          )}
          {item.live?.detail && (
            <div className="font-mono text-[10px] text-amber-400/80 mt-1 truncate">
              {item.live.detail}
            </div>
          )}
        </div>
        <div className="flex-shrink-0 flex flex-col items-end gap-1">
          <div className="font-mono text-[10px] text-slate-500">
            {formatRelative(lastActivity)}
          </div>
          {isLive && (
            <div className="font-mono text-[9px] text-cyan-500/70 uppercase tracking-wider">
              live
            </div>
          )}
          <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
            {editing ? (
              <button
                onClick={handleSaveName}
                disabled={busy}
                className="p-1 text-emerald-400 hover:bg-white/5 rounded"
                title="Save (Enter)"
              >
                <Check className="w-3 h-3" />
              </button>
            ) : (
              <button
                onClick={(e) => {
                  stopPropagation(e)
                  setEditing(true)
                }}
                disabled={busy}
                className="p-1 text-slate-500 hover:text-cyan-400 hover:bg-white/5 rounded"
                title="Rename"
              >
                <Pencil className="w-3 h-3" />
              </button>
            )}
            {confirmingDelete ? (
              <>
                <button
                  onClick={handleDelete}
                  disabled={busy}
                  className="p-1 text-red-400 hover:bg-red-500/10 rounded font-mono text-[10px] tracking-wider uppercase"
                  title="Confirm delete (kills session + removes transcript)"
                >
                  delete
                </button>
                <button
                  onClick={(e) => {
                    stopPropagation(e)
                    setConfirmingDelete(false)
                  }}
                  disabled={busy}
                  className="p-1 text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded"
                  title="Cancel"
                >
                  <Plus className="w-3 h-3 rotate-45" />
                </button>
              </>
            ) : (
              <button
                onClick={(e) => {
                  stopPropagation(e)
                  setConfirmingDelete(true)
                }}
                disabled={busy}
                className="p-1 text-slate-500 hover:text-red-400 hover:bg-white/5 rounded"
                title="Delete session + transcript"
              >
                <Trash2 className="w-3 h-3" />
              </button>
            )}
          </div>
        </div>
      </div>
    </li>
  )
}

function LiveDot({ live }: { live?: SessionLiveState | null }) {
  if (!live) {
    return (
      <span
        className="inline-block rounded-full flex-shrink-0 mt-1.5"
        style={{ width: 8, height: 8, background: '#475569' }}
        title="historical"
      />
    )
  }
  const { color, label } = liveColour(live)
  return (
    <span
      className="inline-block rounded-full flex-shrink-0 mt-1.5"
      style={{
        width: 8,
        height: 8,
        background: color,
        boxShadow: live.tempo === 'active' ? `0 0 6px ${color}` : 'none',
      }}
      title={label}
    />
  )
}

function liveColour(live: SessionLiveState): { color: string; label: string } {
  switch (live.tempo) {
    case 'active':
      return { color: '#facc15', label: 'working' }
    case 'blocked':
      return { color: '#fb923c', label: 'needs you' }
    case 'idle':
      if (live.state === 'done') return { color: '#22c55e', label: 'done' }
      return { color: '#94a3b8', label: 'idle' }
  }
  switch (live.state) {
    case 'failed':
      return { color: '#ef4444', label: 'failed' }
    case 'stopped':
      return { color: '#6b7280', label: 'stopped' }
  }
  return { color: '#64748b', label: live.state }
}

function formatRelative(d: Date): string {
  const ms = Date.now() - d.getTime()
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const days = Math.floor(h / 24)
  if (days < 7) return `${days}d ago`
  return d.toLocaleDateString()
}

// SearchResultGroupCard is one session-grouped block in SearchResultsBlock.
// Lifted out so each card has its own rename/delete state. Clicking
// the card opens the session; the inline pencil starts a rename
// without bubbling.
function SearchResultGroupCard({
  sessionId,
  niceName,
  cwd,
  hits,
  query,
  isLive,
  onOpenSession,
  onRenamed,
  onDeleted,
}: {
  sessionId: string
  niceName: string
  cwd: string
  hits: SessionSearchHit[]
  query: string
  isLive: boolean
  onOpenSession?: (sessionId: string, isLive: boolean, name: string) => void
  onRenamed?: (sessionId: string, newName: string) => void
  onDeleted?: (sessionId: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [editValue, setEditValue] = useState(niceName)
  const [busy, setBusy] = useState(false)
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  useEffect(() => {
    setEditValue(niceName)
  }, [niceName])

  const stop = (e: React.SyntheticEvent) => e.stopPropagation()

  const handleSave = async (e: React.SyntheticEvent) => {
    stop(e)
    const trimmed = editValue.trim()
    if (!trimmed || trimmed === niceName) {
      setEditing(false)
      setEditValue(niceName)
      return
    }
    setBusy(true)
    try {
      await sessionsAPI.renameSession(sessionId, trimmed)
      onRenamed?.(sessionId, trimmed)
      setEditing(false)
    } catch (err) {
      console.error('rename failed', err)
    } finally {
      setBusy(false)
    }
  }

  const handleDelete = async (e: React.SyntheticEvent) => {
    stop(e)
    setBusy(true)
    try {
      await sessionsAPI.deleteSession(sessionId, { deleteTranscript: true })
      onDeleted?.(sessionId)
    } catch (err) {
      console.error('delete failed', err)
    } finally {
      setBusy(false)
      setConfirmingDelete(false)
    }
  }

  const clickable = !!onOpenSession && !editing
  return (
    <li
      className={`group px-4 py-3 ${clickable ? 'cursor-pointer hover:bg-white/[0.02] transition-colors' : ''}`}
      onClick={clickable ? () => onOpenSession?.(sessionId, isLive, niceName) : undefined}
    >
      <div className="flex items-baseline justify-between mb-1.5 gap-2">
        <div className="min-w-0 flex-1">
          {editing ? (
            <input
              autoFocus
              value={editValue}
              onChange={(e) => setEditValue(e.target.value)}
              onClick={stop}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSave(e)
                if (e.key === 'Escape') {
                  stop(e)
                  setEditing(false)
                  setEditValue(niceName)
                }
              }}
              disabled={busy}
              className="w-full bg-transparent border-b border-cyan-500/50 text-cyan-300 outline-none font-mono text-xs pb-px"
            />
          ) : (
            <div className="font-mono text-xs text-cyan-300 truncate">
              {niceName || sessionId.slice(0, 8)}
            </div>
          )}
          <div className="font-mono text-[9px] text-slate-600 tracking-wider uppercase truncate">
            {sessionId.slice(0, 8)} · {cwd}
          </div>
        </div>
        <div className="flex items-center gap-1.5 flex-shrink-0">
          <span className="font-mono text-[10px] text-slate-600">
            {hits.length} match{hits.length === 1 ? '' : 'es'}
          </span>
          <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
            {editing ? (
              <button
                onClick={handleSave}
                disabled={busy}
                className="p-1 text-emerald-400 hover:bg-white/5 rounded"
                title="Save (Enter)"
              >
                <Check className="w-3 h-3" />
              </button>
            ) : (
              <button
                onClick={(e) => { stop(e); setEditing(true) }}
                disabled={busy}
                className="p-1 text-slate-500 hover:text-cyan-400 hover:bg-white/5 rounded"
                title="Rename"
              >
                <Pencil className="w-3 h-3" />
              </button>
            )}
            {confirmingDelete ? (
              <>
                <button
                  onClick={handleDelete}
                  disabled={busy}
                  className="p-1 text-red-400 hover:bg-red-500/10 rounded font-mono text-[10px] tracking-wider uppercase"
                  title="Confirm delete (kills session + removes transcript)"
                >
                  delete
                </button>
                <button
                  onClick={(e) => { stop(e); setConfirmingDelete(false) }}
                  disabled={busy}
                  className="p-1 text-slate-500 hover:text-slate-300 hover:bg-white/5 rounded"
                  title="Cancel"
                >
                  <Plus className="w-3 h-3 rotate-45" />
                </button>
              </>
            ) : (
              <button
                onClick={(e) => { stop(e); setConfirmingDelete(true) }}
                disabled={busy}
                className="p-1 text-slate-500 hover:text-red-400 hover:bg-white/5 rounded"
                title="Delete session + transcript"
              >
                <Trash2 className="w-3 h-3" />
              </button>
            )}
          </div>
        </div>
      </div>
      <ul className="space-y-1">
        {hits.slice(0, 5).map((h) => (
          <li
            key={h.lineNumber}
            className="flex items-start gap-2"
          >
            <span
              className="font-mono text-[10px] tracking-wider uppercase flex-shrink-0 mt-px"
              style={{ color: h.role === 'assistant' ? '#22d3ee' : '#facc15' }}
            >
              {h.role === 'assistant' ? 'claude' : 'you'}
            </span>
            <span className="font-mono text-[11px] text-slate-300 leading-relaxed">
              {highlightQuery(h.snippet, query)}
            </span>
          </li>
        ))}
        {hits.length > 5 && (
          <li className="font-mono text-[10px] text-slate-600 italic pl-3">
            + {hits.length - 5} more
          </li>
        )}
      </ul>
    </li>
  )
}

// SearchResultsBlock renders hits grouped by sessionId. Each group
// shows the session's 8-hex prefix + cwd as a header, then up to 5
// matching snippets with the query highlighted. The whole group card
// is clickable and opens the session (same as clicking it in the
// regular list) — the per-hit click was confusing because users
// expected to "open the session", not "jump to a message".
function SearchResultsBlock({
  hits,
  query,
  onOpenSession,
  onRenamed,
  onDeleted,
  sessionNames,
  sessionsByIdLive,
}: {
  hits: SessionSearchHit[]
  query: string
  /** Click on a result card opens the session. Passes (sessionId, isLive, name). */
  onOpenSession?: (sessionId: string, isLive: boolean, name: string) => void
  /** Called after a successful rename — caller refreshes the list. */
  onRenamed?: (sessionId: string, newName: string) => void
  /** Called after a successful delete — caller refreshes the list. */
  onDeleted?: (sessionId: string) => void
  /** Map of sessionId → human-readable name. Missing entries fall
      back to the 8-hex prefix so the header is never empty. */
  sessionNames?: Record<string, string>
  /** Map of sessionId → has-live-daemon-worker. Drives isLive for onOpenSession. */
  sessionsByIdLive?: Record<string, boolean>
}) {
  // Group hits by sessionId.
  const groups: { sessionId: string; sessionName: string; cwd: string; hits: SessionSearchHit[] }[] = []
  const byId = new Map<string, number>()
  for (const h of hits) {
    let idx = byId.get(h.sessionId)
    if (idx === undefined) {
      idx = groups.length
      byId.set(h.sessionId, idx)
      groups.push({ sessionId: h.sessionId, sessionName: h.sessionName, cwd: h.cwd, hits: [] })
    }
    groups[idx].hits.push(h)
  }

  return (
    <ul className="divide-y divide-white/[0.04]">
      {groups.map((group) => {
        const niceName = group.sessionName || sessionNames?.[group.sessionId] || ''
        const isLive = sessionsByIdLive?.[group.sessionId] ?? false
        return (
          <SearchResultGroupCard
            key={group.sessionId}
            sessionId={group.sessionId}
            niceName={niceName}
            cwd={group.cwd}
            hits={group.hits}
            query={query}
            isLive={isLive}
            onOpenSession={onOpenSession}
            onRenamed={onRenamed}
            onDeleted={onDeleted}
          />
        )
      })}
    </ul>
  )
}

// highlightQuery wraps case-insensitive matches in <mark>.
function highlightQuery(text: string, query: string): React.ReactNode {
  if (!query) return text
  const lc = text.toLowerCase()
  const lq = query.toLowerCase()
  const parts: React.ReactNode[] = []
  let cursor = 0
  while (cursor < text.length) {
    const idx = lc.indexOf(lq, cursor)
    if (idx < 0) {
      parts.push(text.slice(cursor))
      break
    }
    if (idx > cursor) parts.push(text.slice(cursor, idx))
    parts.push(
      <mark key={idx} className="bg-amber-500/30 text-amber-200 rounded px-0.5">
        {text.slice(idx, idx + query.length)}
      </mark>,
    )
    cursor = idx + query.length
  }
  return parts
}

export default SessionsModal
