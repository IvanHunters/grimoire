import { useState, useEffect, useRef } from 'react'
import { X, Plus, Tag, ChevronDown, ChevronRight } from 'lucide-react'
import { tagsAPI, type TagCount } from '../../api/tags'

interface TagPickerProps {
  tags: string[]
  onChange: (tags: string[]) => void
  defaultExpanded?: boolean
}

function TagPicker({ tags, onChange, defaultExpanded = false }: TagPickerProps) {
  const [inputValue, setInputValue] = useState('')
  const [suggestions, setSuggestions] = useState<TagCount[]>([])
  const [showSuggestions, setShowSuggestions] = useState(false)
  const [allTags, setAllTags] = useState<TagCount[]>([])
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const inputRef = useRef<HTMLInputElement>(null)

  // Load all tags on mount
  useEffect(() => {
    tagsAPI.getAllTags().then(setSuggestions).catch(console.error)
    tagsAPI.getAllTags().then(setAllTags).catch(console.error)
  }, [])

  // Filter suggestions based on input
  useEffect(() => {
    if (inputValue.trim()) {
      const filtered = allTags.filter(
        (tc) =>
          tc.tag.toLowerCase().includes(inputValue.toLowerCase()) &&
          !tags.includes(tc.tag)
      )
      setSuggestions(filtered)
    } else {
      // Show popular tags when input is empty
      setSuggestions(allTags.filter((tc) => !tags.includes(tc.tag)).slice(0, 10))
    }
  }, [inputValue, tags, allTags])

  const handleAddTag = (tag: string) => {
    const normalizedTag = tag.trim().toLowerCase()
    if (normalizedTag && !tags.includes(normalizedTag)) {
      onChange([...tags, normalizedTag])
      setInputValue('')
      setShowSuggestions(false)
      inputRef.current?.focus()
    }
  }

  const handleRemoveTag = (tagToRemove: string) => {
    onChange(tags.filter((t) => t !== tagToRemove))
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && inputValue.trim()) {
      e.preventDefault()
      handleAddTag(inputValue)
    } else if (e.key === 'Escape') {
      setShowSuggestions(false)
    } else if (e.key === 'Backspace' && !inputValue && tags.length > 0) {
      // Remove last tag on backspace if input is empty
      handleRemoveTag(tags[tags.length - 1])
    }
  }

  return (
    <div className="space-y-2">
      {/* Header with toggle */}
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center gap-1 text-xs font-medium text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 transition"
      >
        {isExpanded ? (
          <ChevronDown className="w-3 h-3" />
        ) : (
          <ChevronRight className="w-3 h-3" />
        )}
        <Tag className="w-3 h-3" />
        <span>Tags</span>
        {tags.length > 0 && (
          <span className="ml-auto text-gray-500 dark:text-gray-400">
            ({tags.length})
          </span>
        )}
      </button>

      {/* Tags display (collapsible) */}
      {isExpanded && (
        <>
          <div className="flex flex-wrap gap-1.5 min-h-[32px] p-2 bg-gray-50 dark:bg-gray-900 rounded border border-gray-200 dark:border-gray-700">
            {tags.map((tag) => (
              <span
                key={tag}
                className="inline-flex items-center gap-1 px-2 py-0.5 text-xs bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 rounded-full"
              >
                {tag}
                <button
                  onClick={() => handleRemoveTag(tag)}
                  className="hover:bg-blue-200 dark:hover:bg-blue-800 rounded-full p-0.5"
                  title="Remove tag"
                >
                  <X className="w-3 h-3" />
                </button>
              </span>
            ))}

            {/* Input for new tag */}
            <div className="relative flex-1 min-w-[120px]">
              <input
                ref={inputRef}
                type="text"
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onFocus={() => setShowSuggestions(true)}
                onBlur={() => setTimeout(() => setShowSuggestions(false), 200)}
                onKeyDown={handleKeyDown}
                placeholder="Add tag..."
                className="w-full px-1 py-0.5 text-sm bg-transparent border-none focus:outline-none dark:text-gray-200"
              />

              {/* Suggestions dropdown */}
              {showSuggestions && suggestions.length > 0 && (
                <div className="absolute top-full left-0 mt-1 w-64 bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 max-h-48 overflow-y-auto z-50">
                  {suggestions.map((tc) => (
                    <button
                      key={tc.tag}
                      onClick={() => handleAddTag(tc.tag)}
                      className="w-full flex items-center justify-between px-3 py-2 text-sm text-left hover:bg-gray-100 dark:hover:bg-gray-700 transition"
                    >
                      <span className="text-gray-800 dark:text-gray-200">{tc.tag}</span>
                      <span className="text-xs text-gray-500 dark:text-gray-400">
                        {tc.count} {tc.count === 1 ? 'note' : 'notes'}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>

            {/* Add button */}
            <button
              onClick={() => inputValue.trim() && handleAddTag(inputValue)}
              disabled={!inputValue.trim()}
              className="p-1 text-blue-600 dark:text-blue-400 hover:bg-blue-50 dark:hover:bg-blue-900 rounded disabled:opacity-40 disabled:cursor-not-allowed"
              title="Add tag (Enter)"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>

          <p className="text-xs text-gray-500 dark:text-gray-400">
            Press Enter to add, Backspace to remove last
          </p>
        </>
      )}
    </div>
  )
}

export default TagPicker
