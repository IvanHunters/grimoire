import { useState, useEffect, useRef } from 'react'
import { X, FileText, Search } from 'lucide-react'
import { notesAPI } from '../../api/notes'
import type { Note } from '../../types/note'

interface SearchModalProps {
  visible: boolean
  onClose: () => void
  onNoteSelect: (noteId: string) => void
}

interface SearchResult {
  id: string
  title: string
  path: string
  snippet: string
}

export default function SearchModal({ visible, onClose, onNoteSelect }: SearchModalProps) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [loading, setLoading] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (visible && inputRef.current) {
      inputRef.current.focus()
    }
  }, [visible])

  useEffect(() => {
    const searchNotes = async () => {
      if (!query.trim()) {
        setResults([])
        return
      }

      setLoading(true)
      try {
        const notes = await notesAPI.searchNotes(query)

        const searchResults = notes.map((note: Note) => {
          const regex = new RegExp(`(${query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')
          const highlightedTitle = note.title.replace(regex, '<mark>$1</mark>')

          const content = note.content.toLowerCase()
          const index = content.indexOf(query.toLowerCase())

          let snippet = ''
          if (index !== -1) {
            const start = Math.max(0, index - 50)
            const end = Math.min(note.content.length, index + query.length + 50)
            snippet = note.content.slice(start, end)
            if (start > 0) snippet = '...' + snippet
            if (end < note.content.length) snippet = snippet + '...'
            snippet = snippet.replace(regex, '<mark>$1</mark>')
          } else {
            snippet = note.content.slice(0, 100)
            if (note.content.length > 100) snippet += '...'
          }

          return { id: note.id, title: highlightedTitle, path: note.path, snippet }
        })

        setResults(searchResults)
      } catch (error) {
        console.error('Search failed:', error)
        setResults([])
      } finally {
        setLoading(false)
      }
    }

    const timeoutId = setTimeout(searchNotes, 300)
    return () => clearTimeout(timeoutId)
  }, [query])

  const handleResultClick = (noteId: string) => {
    onNoteSelect(noteId)
    onClose()
    setQuery('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') onClose()
  }

  if (!visible) return null

  return (
    <div
      className="fixed inset-0 bg-black/70 backdrop-blur-sm flex items-start justify-center z-50 pt-4 md:pt-16"
      onClick={onClose}
    >
      <div
        className="bg-[#0a0b10] border border-white/[0.09] rounded-lg shadow-2xl w-full max-w-2xl mx-4 overflow-hidden"
        onClick={(e) => e.stopPropagation()}
        style={{ boxShadow: '0 0 0 1px rgba(6,182,212,0.08), 0 25px 60px rgba(0,0,0,0.7)' }}
      >
        {/* Search input row */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-white/[0.06]">
          <Search className="w-4 h-4 text-slate-600 flex-shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="search across all notes..."
            className="flex-1 bg-transparent text-sm text-slate-200 font-mono placeholder-slate-700 focus:outline-none"
          />
          {loading && (
            <span className="text-[10px] font-mono text-cyan-600 tracking-widest uppercase animate-pulse">
              searching
            </span>
          )}
          <button
            onClick={onClose}
            className="p-1 text-slate-700 hover:text-slate-400 transition-colors rounded flex-shrink-0"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Results */}
        <div className="max-h-[480px] overflow-y-auto">
          {!loading && !query && (
            <div className="px-4 py-10 text-center">
              <span className="text-[11px] font-mono text-slate-700 tracking-widest uppercase">
                type to search
              </span>
            </div>
          )}

          {!loading && query && results.length === 0 && (
            <div className="px-4 py-10 text-center">
              <span className="text-[11px] font-mono text-slate-700 tracking-widest uppercase">
                no results for &ldquo;{query}&rdquo;
              </span>
            </div>
          )}

          {!loading && results.length > 0 && (
            <div className="py-1">
              {results.map((result, i) => (
                <button
                  key={result.id}
                  onClick={() => handleResultClick(result.id)}
                  className="w-full text-left px-4 py-3 hover:bg-white/[0.03] transition-colors border-b border-white/[0.04] last:border-0 group"
                >
                  <div className="flex items-center gap-2 mb-1.5">
                    <FileText className="w-3.5 h-3.5 text-slate-600 group-hover:text-cyan-600 transition-colors flex-shrink-0" />
                    <div
                      className="text-sm font-medium text-slate-300 font-mono [&_mark]:bg-cyan-500/20 [&_mark]:text-cyan-300 [&_mark]:rounded [&_mark]:px-0.5"
                      dangerouslySetInnerHTML={{ __html: result.title }}
                    />
                    <span className="ml-auto text-[10px] font-mono text-slate-700">
                      {String(i + 1).padStart(2, '0')}
                    </span>
                  </div>
                  <div className="text-[11px] text-slate-700 font-mono mb-1.5 truncate pl-5">
                    {result.path}
                  </div>
                  <div
                    className="text-xs text-slate-500 line-clamp-2 pl-5 font-mono [&_mark]:bg-cyan-500/20 [&_mark]:text-cyan-300 [&_mark]:rounded [&_mark]:px-0.5"
                    dangerouslySetInnerHTML={{ __html: result.snippet }}
                  />
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        {results.length > 0 && (
          <div className="px-4 py-2 border-t border-white/[0.06] flex items-center justify-between">
            <span className="text-[10px] font-mono text-slate-700 tracking-widest uppercase">
              {results.length} {results.length === 1 ? 'result' : 'results'}
            </span>
            <span className="text-[10px] font-mono text-slate-700">
              ↵ open · esc close
            </span>
          </div>
        )}
      </div>
    </div>
  )
}
