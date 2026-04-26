import { useState, useEffect, useRef } from 'react'
import { X, FileText } from 'lucide-react'
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

  // Focus input when modal opens
  useEffect(() => {
    if (visible && inputRef.current) {
      inputRef.current.focus()
    }
  }, [visible])

  // Search as user types
  useEffect(() => {
    const searchNotes = async () => {
      if (!query.trim()) {
        setResults([])
        return
      }

      setLoading(true)
      try {
        const notes = await notesAPI.searchNotes(query)

        // Create results with snippets
        const searchResults = notes.map((note: Note) => {
          const queryLower = query.toLowerCase()
          const regex = new RegExp(`(${query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`, 'gi')

          // Highlight query in title
          const highlightedTitle = note.title.replace(regex, '<mark>$1</mark>')

          // Extract snippet around first occurrence of query in content
          const content = note.content.toLowerCase()
          const index = content.indexOf(queryLower)

          let snippet = ''
          if (index !== -1) {
            const start = Math.max(0, index - 50)
            const end = Math.min(note.content.length, index + query.length + 50)
            snippet = note.content.slice(start, end)

            // Add ellipsis
            if (start > 0) snippet = '...' + snippet
            if (end < note.content.length) snippet = snippet + '...'

            // Highlight query in snippet
            snippet = snippet.replace(regex, '<mark>$1</mark>')
          } else {
            // If not in content, just show start of content
            snippet = note.content.slice(0, 100)
            if (note.content.length > 100) snippet += '...'
          }

          return {
            id: note.id,
            title: highlightedTitle,
            path: note.path,
            snippet: snippet,
          }
        })

        setResults(searchResults)
      } catch (error) {
        console.error('Search failed:', error)
        setResults([])
      } finally {
        setLoading(false)
      }
    }

    const timeoutId = setTimeout(searchNotes, 300) // Debounce
    return () => clearTimeout(timeoutId)
  }, [query])

  const handleResultClick = (noteId: string) => {
    onNoteSelect(noteId)
    onClose()
    setQuery('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      onClose()
    }
  }

  if (!visible) return null

  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-start justify-center z-50 pt-20"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-lg shadow-xl w-full max-w-2xl mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200">
          <h2 className="text-lg font-semibold text-gray-900">Search Notes</h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 transition"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Search Input */}
        <div className="px-6 py-4 border-b border-gray-200">
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Search across all notes..."
            className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          />
        </div>

        {/* Results */}
        <div className="px-6 py-4 max-h-96 overflow-y-auto">
          {loading && (
            <div className="text-center text-gray-500 py-8">
              Searching...
            </div>
          )}

          {!loading && query && results.length === 0 && (
            <div className="text-center text-gray-500 py-8">
              No notes found
            </div>
          )}

          {!loading && !query && (
            <div className="text-center text-gray-400 py-8">
              Start typing to search...
            </div>
          )}

          {!loading && results.length > 0 && (
            <div className="space-y-2">
              {results.map((result) => (
                <button
                  key={result.id}
                  onClick={() => handleResultClick(result.id)}
                  className="w-full text-left p-4 rounded-lg hover:bg-gray-50 transition border border-transparent hover:border-gray-200"
                >
                  <div className="flex items-center gap-2 mb-1">
                    <FileText className="w-4 h-4 text-gray-400" />
                    <div
                      className="font-medium text-gray-900"
                      dangerouslySetInnerHTML={{ __html: result.title }}
                    />
                  </div>
                  <div className="text-xs text-gray-500 mb-2">{result.path}</div>
                  <div
                    className="text-sm text-gray-600 line-clamp-2"
                    dangerouslySetInnerHTML={{ __html: result.snippet }}
                  />
                </button>
              ))}
            </div>
          )}
        </div>

        {/* Footer */}
        {results.length > 0 && (
          <div className="px-6 py-3 border-t border-gray-200 text-sm text-gray-500">
            {results.length} {results.length === 1 ? 'result' : 'results'} found
          </div>
        )}
      </div>
    </div>
  )
}
