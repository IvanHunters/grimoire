import { useEffect, useRef, useState } from 'react'
import { Search, X } from 'lucide-react'
import { sessionsAPI, type SessionSearchHit } from '../../api/sessions'

interface GlobalSearchModalProps {
  visible: boolean
  onClose: () => void
  /** Called when the user clicks a search hit. */
  onOpenHit?: (sessionId: string, lineNumber: number) => void
}

/**
 * GlobalSearchModal is a cmd-K style palette that searches every Claude
 * conversation transcript on disk. Substring match, case-insensitive,
 * no regex. Results group by session and show context snippets around
 * each match.
 *
 * Debounced: typing stops triggering requests until the user pauses for
 * 250ms. Earlier requests are abandoned via a cancellation flag.
 */
function GlobalSearchModal({ visible, onClose, onOpenHit }: GlobalSearchModalProps) {
  const [query, setQuery] = useState('')
  const [hits, setHits] = useState<SessionSearchHit[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Autofocus on open + reset on close.
  useEffect(() => {
    if (!visible) {
      setQuery('')
      setHits([])
      setError(null)
      return
    }
    // tiny delay so the focus doesn't fight the modal mount animation
    const id = setTimeout(() => inputRef.current?.focus(), 50)
    return () => clearTimeout(id)
  }, [visible])

  // Debounced search.
  useEffect(() => {
    if (!visible) return
    if (query.trim().length < 2) {
      setHits([])
      setError(null)
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    const timer = setTimeout(() => {
      sessionsAPI
        .search(query.trim(), { limit: 80 })
        .then((results) => {
          if (!cancelled) {
            setHits(results)
            setError(null)
          }
        })
        .catch((e) => {
          if (!cancelled) setError(String(e?.message ?? e))
        })
        .finally(() => {
          if (!cancelled) setLoading(false)
        })
    }, 250)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [query, visible])

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

  // Group hits by sessionId for readability.
  const grouped = groupBySession(hits)

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-start justify-center z-[2000] pt-20"
      onClick={handleOverlayClick}
      onKeyDown={handleKeyDown}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-2xl mx-4 max-h-[75vh] flex flex-col overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-2 px-4 py-3 border-b border-white/[0.06] flex-shrink-0">
          <Search className="w-4 h-4 text-slate-500" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search chat history (not notes — sessions only)…"
            className="flex-1 bg-transparent outline-none font-mono text-sm text-slate-200 placeholder:text-slate-600"
          />
          {loading && (
            <span className="font-mono text-[10px] text-slate-500 animate-pulse">
              searching…
            </span>
          )}
          <button
            onClick={onClose}
            className="p-1 text-slate-500 hover:text-slate-300 rounded"
            title="Close (Esc)"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto">
          {error && (
            <div className="px-4 py-3 text-xs font-mono text-red-400">
              error: {error}
            </div>
          )}
          {!loading && query.trim().length >= 2 && hits.length === 0 && !error && (
            <div className="px-4 py-6 text-center text-xs font-mono text-slate-500">
              no matches
            </div>
          )}
          {query.trim().length < 2 && (
            <div className="px-4 py-6 text-center text-xs font-mono text-slate-600">
              type at least 2 characters
            </div>
          )}
          <ul className="divide-y divide-white/[0.04]">
            {grouped.map((group) => (
              <li key={group.sessionId} className="px-4 py-3">
                <div className="flex items-baseline justify-between mb-1.5">
                  <span className="font-mono text-[10px] text-cyan-500/80 tracking-wider uppercase truncate">
                    {group.sessionId.slice(0, 8)} · {group.cwd}
                  </span>
                  <span className="font-mono text-[10px] text-slate-600">
                    {group.hits.length} match{group.hits.length === 1 ? '' : 'es'}
                  </span>
                </div>
                <ul className="space-y-1">
                  {group.hits.slice(0, 5).map((h) => (
                    <HitSnippet
                      key={h.lineNumber}
                      hit={h}
                      query={query.trim()}
                      onClick={onOpenHit ? () => onOpenHit(h.sessionId, h.lineNumber) : undefined}
                    />
                  ))}
                  {group.hits.length > 5 && (
                    <li className="font-mono text-[10px] text-slate-600 italic pl-3">
                      + {group.hits.length - 5} more
                    </li>
                  )}
                </ul>
              </li>
            ))}
          </ul>
        </div>

        <div className="border-t border-white/[0.06] px-4 py-2 flex-shrink-0 flex items-center justify-between">
          <span className="text-[10px] font-mono text-slate-600">
            {hits.length} hit{hits.length === 1 ? '' : 's'} across {grouped.length} session{grouped.length === 1 ? '' : 's'}
          </span>
          <span className="text-[10px] font-mono text-slate-700">esc to close</span>
        </div>
      </div>
    </div>
  )
}

function HitSnippet({ hit, query, onClick }: { hit: SessionSearchHit; query: string; onClick?: () => void }) {
  const dotColour = hit.role === 'assistant' ? '#22d3ee' : '#facc15'
  const clickable = !!onClick
  return (
    <li
      className={`flex items-start gap-2 ${clickable ? 'cursor-pointer hover:bg-white/[0.03] -mx-2 px-2 py-0.5 rounded' : ''}`}
      onClick={onClick}
    >
      <span
        className="inline-block rounded-full flex-shrink-0 mt-1.5"
        style={{ width: 6, height: 6, background: dotColour }}
        title={hit.role}
      />
      <span className="font-mono text-[11px] text-slate-300 leading-relaxed">
        {highlight(hit.snippet, query)}
      </span>
    </li>
  )
}

// Light highlight: wrap query matches with <mark>. Case-insensitive.
function highlight(text: string, query: string) {
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

interface HitGroup {
  sessionId: string
  cwd: string
  hits: SessionSearchHit[]
}

function groupBySession(hits: SessionSearchHit[]): HitGroup[] {
  const map = new Map<string, HitGroup>()
  for (const h of hits) {
    let g = map.get(h.sessionId)
    if (!g) {
      g = { sessionId: h.sessionId, cwd: h.cwd, hits: [] }
      map.set(h.sessionId, g)
    }
    g.hits.push(h)
  }
  return Array.from(map.values())
}

export default GlobalSearchModal
