import { useState, useRef, useEffect, useCallback } from 'react'

interface SearchPanelProps {
  visible: boolean
  onClose: () => void
  textareaRef: React.RefObject<HTMLTextAreaElement>
  onUpdateHighlights: (matches: Array<{ index: number; length: number }>, currentMatch: number) => void
  onSyncScroll?: () => void
  showReplaceInitially?: boolean
}

/**
 * Search/Replace Panel - ACE Editor style (floating)
 * Ported from design-prototype.html
 *
 * Features:
 * - Regex/Case/Word toggles
 * - Match counter "N of M"
 * - Next/Prev navigation (Enter/Shift+Enter)
 * - Replace one/all
 * - Highlight overlay
 */
function SearchPanel({ visible, onClose, textareaRef, onUpdateHighlights, onSyncScroll, showReplaceInitially = false }: SearchPanelProps) {
  const [searchQuery, setSearchQuery] = useState('')
  const [replaceText, setReplaceText] = useState('')
  const [showReplace, setShowReplace] = useState(showReplaceInitially)
  const [regex, setRegex] = useState(false)
  const [caseSensitive, setCaseSensitive] = useState(false)
  const [wholeWord, setWholeWord] = useState(false)
  const [matches, setMatches] = useState<Array<{ index: number; length: number }>>([])
  const [currentMatch, setCurrentMatch] = useState(-1)

  const searchInputRef = useRef<HTMLInputElement>(null)
  const replaceInputRef = useRef<HTMLInputElement>(null)

  // Focus search input when panel opens
  useEffect(() => {
    if (visible && searchInputRef.current) {
      searchInputRef.current.focus()
      searchInputRef.current.select()
    }
  }, [visible])

  // Show replace initially if requested (from Cmd+H)
  useEffect(() => {
    if (visible && showReplaceInitially) {
      setShowReplace(true)
    }
  }, [visible, showReplaceInitially])

  // Update matches when query or options change
  useEffect(() => {
    if (!visible || !textareaRef.current || !searchQuery) {
      setMatches([])
      setCurrentMatch(-1)
      onUpdateHighlights([], -1)
      return
    }

    const textarea = textareaRef.current
    const text = textarea.value
    const foundMatches: Array<{ index: number; length: number }> = []

    try {
      if (regex) {
        // Regex search with multiline flag for ^ and $ to work across lines
        const flags = caseSensitive ? 'gm' : 'gim'
        const re = new RegExp(searchQuery, flags)
        const matches = [...text.matchAll(re)]

        foundMatches.push(...matches.map(m => ({
          index: m.index!,
          length: m[0].length
        })))
      } else {
        // Plain text search
        let searchText = searchQuery
        let contentText = text

        if (!caseSensitive) {
          searchText = searchText.toLowerCase()
          contentText = contentText.toLowerCase()
        }

        if (wholeWord) {
          // Whole word search - use regex
          const escapedQuery = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
          const flags = caseSensitive ? 'gm' : 'gim'
          const re = new RegExp(`\\b${escapedQuery}\\b`, flags)
          const matches = [...text.matchAll(re)]

          foundMatches.push(...matches.map(m => ({
            index: m.index!,
            length: m[0].length
          })))
        } else {
          // Simple substring search
          let index = 0
          while ((index = contentText.indexOf(searchText, index)) !== -1) {
            foundMatches.push({ index, length: searchQuery.length })
            index += searchQuery.length
          }
        }
      }

      setMatches(foundMatches)
      setCurrentMatch(foundMatches.length > 0 ? 0 : -1)
      onUpdateHighlights(foundMatches, foundMatches.length > 0 ? 0 : -1)
    } catch (e) {
      // Invalid regex
      setMatches([])
      setCurrentMatch(-1)
      onUpdateHighlights([], -1)
    }
  }, [searchQuery, regex, caseSensitive, wholeWord, visible, textareaRef, onUpdateHighlights])

  const findNext = useCallback(() => {
    if (matches.length === 0) return

    const nextIndex = (currentMatch + 1) % matches.length
    setCurrentMatch(nextIndex)
    onUpdateHighlights(matches, nextIndex)

    // Scroll to match
    if (textareaRef.current && matches[nextIndex]) {
      const textarea = textareaRef.current
      const match = matches[nextIndex]
      textarea.setSelectionRange(match.index, match.index + match.length)
      textarea.scrollTop = textarea.scrollHeight * (match.index / textarea.value.length) - textarea.clientHeight / 2

      // Sync highlight overlay after programmatic scroll
      // Use requestAnimationFrame to ensure browser has updated scrollTop
      requestAnimationFrame(() => {
        onSyncScroll?.()
      })

      // Return focus to search input so next Enter works
      setTimeout(() => {
        searchInputRef.current?.focus()
      }, 0)
    }
  }, [matches, currentMatch, onUpdateHighlights])

  const findPrev = useCallback(() => {
    if (matches.length === 0) return

    const prevIndex = currentMatch - 1 < 0 ? matches.length - 1 : currentMatch - 1
    setCurrentMatch(prevIndex)
    onUpdateHighlights(matches, prevIndex)

    // Scroll to match
    if (textareaRef.current && matches[prevIndex]) {
      const textarea = textareaRef.current
      const match = matches[prevIndex]
      textarea.setSelectionRange(match.index, match.index + match.length)
      textarea.scrollTop = textarea.scrollHeight * (match.index / textarea.value.length) - textarea.clientHeight / 2

      // Sync highlight overlay after programmatic scroll
      // Use requestAnimationFrame to ensure browser has updated scrollTop
      requestAnimationFrame(() => {
        onSyncScroll?.()
      })

      // Return focus to search input so next Enter works
      setTimeout(() => {
        searchInputRef.current?.focus()
      }, 0)
    }
  }, [matches, currentMatch, onUpdateHighlights])

  // Listen for global navigation shortcuts (F3, Cmd+G)
  useEffect(() => {
    if (!visible) return

    const handleFindNext = () => findNext()
    const handleFindPrev = () => findPrev()

    document.addEventListener('searchFindNext', handleFindNext)
    document.addEventListener('searchFindPrev', handleFindPrev)

    return () => {
      document.removeEventListener('searchFindNext', handleFindNext)
      document.removeEventListener('searchFindPrev', handleFindPrev)
    }
  }, [visible, findNext, findPrev])

  const replaceOne = () => {
    if (!textareaRef.current || currentMatch === -1 || !matches[currentMatch]) return

    const textarea = textareaRef.current
    const match = matches[currentMatch]
    const text = textarea.value

    // Replace current match
    const newText = text.substring(0, match.index) + replaceText + text.substring(match.index + match.length)
    textarea.value = newText

    // Trigger input event to update editor state
    const event = new Event('input', { bubbles: true })
    textarea.dispatchEvent(event)

    // Re-search to find new matches
    setTimeout(() => {
      if (!textareaRef.current || !searchQuery) return

      const updatedText = textareaRef.current.value
      const foundMatches: Array<{ index: number; length: number }> = []

      try {
        if (regex) {
          const flags = caseSensitive ? 'gm' : 'gim'
          const re = new RegExp(searchQuery, flags)
          const matches = [...updatedText.matchAll(re)]
          foundMatches.push(...matches.map(m => ({
            index: m.index!,
            length: m[0].length
          })))
        } else {
          let searchText = searchQuery
          let contentText = updatedText

          if (!caseSensitive) {
            searchText = searchText.toLowerCase()
            contentText = contentText.toLowerCase()
          }

          if (wholeWord) {
            const escapedQuery = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
            const flags = caseSensitive ? 'gm' : 'gim'
            const re = new RegExp(`\\b${escapedQuery}\\b`, flags)
            const matches = [...updatedText.matchAll(re)]
            foundMatches.push(...matches.map(m => ({
              index: m.index!,
              length: m[0].length
            })))
          } else {
            let idx = 0
            while ((idx = contentText.indexOf(searchText, idx)) !== -1) {
              foundMatches.push({ index: idx, length: searchQuery.length })
              idx += searchQuery.length
            }
          }
        }

        setMatches(foundMatches)
        const newCurrentMatch = foundMatches.length > 0 ? 0 : -1
        setCurrentMatch(newCurrentMatch)
        onUpdateHighlights(foundMatches, newCurrentMatch)
      } catch (e) {
        setMatches([])
        setCurrentMatch(-1)
        onUpdateHighlights([], -1)
      }
    }, 0)
  }

  const replaceAll = () => {
    if (!textareaRef.current || !searchQuery) return

    const textarea = textareaRef.current

    try {
      let pattern = searchQuery

      // Escape special chars if not regex mode
      if (!regex) {
        pattern = pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      }

      // Add word boundaries if whole word mode
      if (wholeWord) {
        pattern = '\\b' + pattern + '\\b'
      }

      // Use same flags as search
      const flags = caseSensitive ? 'gm' : 'gim'
      const regexPattern = new RegExp(pattern, flags)

      // Perform replacement
      textarea.value = textarea.value.replace(regexPattern, replaceText)

      // Trigger input event to update editor state
      const event = new Event('input', { bubbles: true })
      textarea.dispatchEvent(event)

      // Clear search after replacing
      setTimeout(() => {
        setSearchQuery('')
        setMatches([])
        setCurrentMatch(-1)
        onUpdateHighlights([], -1)
      }, 0)
    } catch (e) {
      // Invalid regex - do nothing
      console.error('Replace all failed:', e)
    }
  }

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      if (e.shiftKey) {
        findPrev()
      } else {
        findNext()
      }
    } else if (e.key === 'Escape') {
      onClose()
    }
  }

  const handleReplaceKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      replaceOne()
    } else if (e.key === 'Escape') {
      onClose()
    }
  }

  if (!visible) return null

  return (
    <div className="search-panel show">
      {/* Search row */}
      <div className="search-row">
        <button
          className={`search-icon-btn ${showReplace ? 'active' : ''}`}
          onClick={() => setShowReplace(!showReplace)}
          title="Toggle Replace"
        >
          <i className={`fas fa-chevron-${showReplace ? 'down' : 'right'}`}></i>
        </button>
        <input
          ref={searchInputRef}
          type="text"
          className="search-input"
          placeholder="Find"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          onKeyDown={handleSearchKeyDown}
        />
        <button className="search-icon-btn" onClick={findPrev} title="Previous (Shift+Enter)">
          <i className="fas fa-chevron-up"></i>
        </button>
        <button className="search-icon-btn" onClick={findNext} title="Next (Enter)">
          <i className="fas fa-chevron-down"></i>
        </button>
        <span className="search-count">
          {matches.length > 0 ? `${currentMatch + 1} of ${matches.length}` : '0 of 0'}
        </span>
        <button
          className={`search-icon-btn ${regex ? 'active' : ''}`}
          onClick={() => setRegex(!regex)}
          title="Regex"
        >
          <i className="fas fa-asterisk"></i>
        </button>
        <button
          className={`search-icon-btn ${caseSensitive ? 'active' : ''}`}
          onClick={() => setCaseSensitive(!caseSensitive)}
          title="Match Case"
        >
          <i className="fas fa-font"></i>
        </button>
        <button
          className={`search-icon-btn ${wholeWord ? 'active' : ''}`}
          onClick={() => setWholeWord(!wholeWord)}
          title="Whole Word"
        >
          <i className="fas fa-border-all"></i>
        </button>
        <button className="search-close" onClick={onClose}>
          <i className="fas fa-times"></i>
        </button>
      </div>

      {/* Replace row (hidden by default) */}
      {showReplace && (
        <div className="search-row replace-row show">
          <div style={{ width: 26 }}></div>
          <input
            ref={replaceInputRef}
            type="text"
            className="search-input"
            placeholder="Replace"
            value={replaceText}
            onChange={(e) => setReplaceText(e.target.value)}
            onKeyDown={handleReplaceKeyDown}
          />
          <button className="search-icon-btn" onClick={replaceOne} title="Replace">
            <i className="fas fa-exchange-alt"></i>
          </button>
          <button className="search-icon-btn" onClick={replaceAll} title="Replace All">
            <i className="fas fa-retweet"></i>
          </button>
        </div>
      )}
    </div>
  )
}

export default SearchPanel
