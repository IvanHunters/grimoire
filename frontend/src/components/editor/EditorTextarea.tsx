import { forwardRef, useRef, useEffect, useCallback, useState } from 'react'
import { useNotes } from '../../contexts/NotesContext'
import SearchPanel from './SearchPanel'
import UploadModal from '../modals/UploadModal'

interface EditorTextareaProps {
  className?: string
  content?: string
  onChange?: (content: string) => void
  onReady?: (insertMarkdown: (type: string, value?: string) => void) => void
}

/**
 * Plain textarea editor - ported from design-prototype.html
 * Simpler than CodeMirror, exact match to prototype
 */
const EditorTextarea = forwardRef<HTMLDivElement, EditorTextareaProps>(
  ({ className = '', content = '', onChange, onReady }, ref) => {
    const { currentNote } = useNotes()
    const textareaRef = useRef<HTMLTextAreaElement>(null)
    const highlightRef = useRef<HTMLDivElement>(null)

    // Undo/Redo history
    const [undoHistory, setUndoHistory] = useState<Array<{
      value: string
      selectionStart: number
      selectionEnd: number
    }>>([])
    const [redoHistory, setRedoHistory] = useState<Array<{
      value: string
      selectionStart: number
      selectionEnd: number
    }>>([])

    const lastValueRef = useRef('')
    const lastSelectionStartRef = useRef(0)
    const lastSelectionEndRef = useRef(0)
    const isUndoRedoActionRef = useRef(false)

    // Search panel state
    const [showSearchPanel, setShowSearchPanel] = useState(false)
    const [showReplaceInitially, setShowReplaceInitially] = useState(false)
    const [searchMatches, setSearchMatches] = useState<Array<{ index: number; length: number }>>([])
    const [currentSearchMatch, setCurrentSearchMatch] = useState(-1)

    // Upload modal state
    const [showUploadModal, setShowUploadModal] = useState(false)
    const [uploadFile, setUploadFile] = useState<File | null>(null)
    const [uploadCursorPosition, setUploadCursorPosition] = useState(0)

    // Update textarea value when content prop changes
    useEffect(() => {
      if (textareaRef.current && content !== undefined) {
        textareaRef.current.value = content
        lastValueRef.current = content
      }
    }, [content])

    // Save undo state
    const saveUndoState = useCallback(() => {
      if (isUndoRedoActionRef.current) return
      if (!textareaRef.current) return

      const currentValue = textareaRef.current.value
      if (currentValue !== lastValueRef.current) {
        setUndoHistory((prev) => {
          const newHistory = [
            ...prev,
            {
              value: lastValueRef.current,
              selectionStart: lastSelectionStartRef.current,
              selectionEnd: lastSelectionEndRef.current,
            },
          ]
          // Limit to 100 items
          if (newHistory.length > 100) {
            newHistory.shift()
          }
          return newHistory
        })
        setRedoHistory([]) // Clear redo on new change
        lastValueRef.current = currentValue
        lastSelectionStartRef.current = textareaRef.current.selectionStart
        lastSelectionEndRef.current = textareaRef.current.selectionEnd
      }
    }, [])

    // Handle paste event - check for images
    const handlePaste = useCallback((e: React.ClipboardEvent<HTMLTextAreaElement>) => {
      const items = e.clipboardData?.items
      if (!items) {
        saveCurrentState()
        return
      }

      // Check if clipboard contains image
      for (let i = 0; i < items.length; i++) {
        if (items[i].type.indexOf('image') !== -1) {
          e.preventDefault() // Prevent default paste
          const blob = items[i].getAsFile()
          if (blob && textareaRef.current) {
            setUploadFile(blob)
            setUploadCursorPosition(textareaRef.current.selectionStart)
            setShowUploadModal(true)
          }
          return // Don't save undo state yet, will save in confirmUpload
        }
      }

      // Regular text paste
      saveCurrentState()
    }, [])

    // Confirm upload and insert markdown
    const handleUploadConfirm = useCallback((fileName: string) => {
      if (!uploadFile || !textareaRef.current) return

      // Get file extension
      const ext = uploadFile.name.split('.').pop() || 'png'
      const fullFileName = `${fileName}.${ext}`

      // Create markdown for uploaded file
      let markdown = ''
      if (uploadFile.type.startsWith('image/')) {
        // Image: ![alt](url)
        markdown = `![${fileName}](/uploads/${fullFileName})`
      } else {
        // Document: [filename](url)
        markdown = `[${fileName}](/uploads/${fullFileName})`
      }

      // Insert at saved cursor position
      const text = textareaRef.current.value
      const newText = text.substring(0, uploadCursorPosition) + markdown + text.substring(uploadCursorPosition)

      textareaRef.current.value = newText
      textareaRef.current.selectionStart = uploadCursorPosition + markdown.length
      textareaRef.current.selectionEnd = uploadCursorPosition + markdown.length

      // Save state and notify parent
      saveCurrentState()
      lastValueRef.current = newText
      onChange?.(newText)

      // Focus back to editor
      textareaRef.current.focus()

      // TODO: Actually upload file to server (for now just insert markdown)
      console.log('Upload file:', fullFileName, uploadFile)
    }, [uploadFile, uploadCursorPosition, onChange])

    // Save current state immediately (for paste, format buttons)
    const saveCurrentState = useCallback(() => {
      if (isUndoRedoActionRef.current) return
      if (!textareaRef.current) return

      setUndoHistory((prev) => {
        const newHistory = [
          ...prev,
          {
            value: textareaRef.current!.value,
            selectionStart: textareaRef.current!.selectionStart,
            selectionEnd: textareaRef.current!.selectionEnd,
          },
        ]
        if (newHistory.length > 100) {
          newHistory.shift()
        }
        return newHistory
      })
      setRedoHistory([])
      lastValueRef.current = textareaRef.current.value
      lastSelectionStartRef.current = textareaRef.current.selectionStart
      lastSelectionEndRef.current = textareaRef.current.selectionEnd
    }, [])

    // Render search highlights
    const renderHighlights = useCallback(() => {
      if (!textareaRef.current || !highlightRef.current) return

      const text = textareaRef.current.value

      if (searchMatches.length === 0) {
        // No search - just show plain text (escape HTML)
        const div = document.createElement('div')
        div.textContent = text
        highlightRef.current.innerHTML = `<div class="highlight-content">${div.innerHTML}</div>`
        return
      }

      // When searching, build HTML with highlighted matches
      let html = ''
      let lastIndex = 0

      searchMatches.forEach((match, index) => {
        // Text before match (escape HTML)
        const beforeDiv = document.createElement('div')
        beforeDiv.textContent = text.substring(lastIndex, match.index)
        html += beforeDiv.innerHTML

        // Highlighted match
        const isActive = index === currentSearchMatch
        const className = isActive ? 'search-highlight active' : 'search-highlight'
        const matchDiv = document.createElement('div')
        matchDiv.textContent = text.substring(match.index, match.index + match.length)
        html += `<span class="${className}">${matchDiv.innerHTML}</span>`

        lastIndex = match.index + match.length
      })

      // Remaining text (escape HTML)
      const remainingDiv = document.createElement('div')
      remainingDiv.textContent = text.substring(lastIndex)
      html += remainingDiv.innerHTML

      highlightRef.current.innerHTML = `<div class="highlight-content">${html}</div>`

      // Apply current scroll position to new highlight content
      if (textareaRef.current) {
        requestAnimationFrame(() => {
          const scrollTop = textareaRef.current!.scrollTop
          const scrollLeft = textareaRef.current!.scrollLeft
          const contentDiv = highlightRef.current?.querySelector('.highlight-content') as HTMLElement
          if (contentDiv) {
            contentDiv.style.transform = `translate(${-scrollLeft}px, ${-scrollTop}px)`
            contentDiv.style.willChange = 'transform'
          }
        })
      }
    }, [searchMatches, currentSearchMatch])

    // Handle input with auto-save on word boundaries
    const handleInput = useCallback(() => {
      if (!textareaRef.current) return

      const currentValue = textareaRef.current.value
      const diff = currentValue.length - lastValueRef.current.length

      if (diff > 0) {
        // Character was added
        const addedChar = currentValue.charAt(textareaRef.current.selectionStart - 1)

        // Save on word boundaries
        if (
          addedChar === ' ' ||
          addedChar === '\n' ||
          addedChar === '.' ||
          addedChar === ',' ||
          addedChar === ';' ||
          addedChar === '!' ||
          addedChar === '?' ||
          addedChar === ':'
        ) {
          saveUndoState()
        }
      }

      lastValueRef.current = currentValue
      renderHighlights() // Update highlight overlay

      // Notify parent about content change
      onChange?.(currentValue)
    }, [saveUndoState, renderHighlights, onChange])

    // Handle scroll - sync with highlight overlay using transform
    const handleScroll = useCallback(() => {
      if (!textareaRef.current || !highlightRef.current) return

      const scrollTop = textareaRef.current.scrollTop
      const scrollLeft = textareaRef.current.scrollLeft

      // Use transform instead of scrollTop for smoother sync
      const contentDiv = highlightRef.current.querySelector('.highlight-content') as HTMLElement
      if (contentDiv) {
        contentDiv.style.transform = `translate(${-scrollLeft}px, ${-scrollTop}px)`
        contentDiv.style.willChange = 'transform'
      }
    }, [])

    // Undo/Redo handlers
    const handleUndo = useCallback(() => {
      if (undoHistory.length === 0 || !textareaRef.current) return

      isUndoRedoActionRef.current = true

      const currentValue = textareaRef.current.value
      let historyIndex = undoHistory.length - 1

      // Skip states that match current value (avoid double undo)
      while (historyIndex >= 0 && undoHistory[historyIndex].value === currentValue) {
        historyIndex--
      }

      // No different state found
      if (historyIndex < 0) {
        isUndoRedoActionRef.current = false
        return
      }

      const currentState = {
        value: currentValue,
        selectionStart: textareaRef.current.selectionStart,
        selectionEnd: textareaRef.current.selectionEnd,
      }

      const prevState = undoHistory[historyIndex]

      // Remove all states up to and including the one we're restoring
      setUndoHistory((prev) => prev.slice(0, historyIndex))
      setRedoHistory((prev) => [...prev, currentState])

      textareaRef.current.value = prevState.value
      textareaRef.current.setSelectionRange(
        prevState.selectionStart,
        prevState.selectionEnd
      )

      onChange?.(prevState.value)
      lastValueRef.current = prevState.value
      isUndoRedoActionRef.current = false
    }, [undoHistory, onChange])

    const handleRedo = useCallback(() => {
      if (redoHistory.length === 0 || !textareaRef.current) return

      isUndoRedoActionRef.current = true

      const currentValue = textareaRef.current.value
      let historyIndex = redoHistory.length - 1

      // Skip states that match current value (avoid double redo)
      while (historyIndex >= 0 && redoHistory[historyIndex].value === currentValue) {
        historyIndex--
      }

      // No different state found
      if (historyIndex < 0) {
        isUndoRedoActionRef.current = false
        return
      }

      const currentState = {
        value: currentValue,
        selectionStart: textareaRef.current.selectionStart,
        selectionEnd: textareaRef.current.selectionEnd,
      }

      const nextState = redoHistory[historyIndex]

      // Remove all states up to and including the one we're restoring
      setRedoHistory((prev) => prev.slice(0, historyIndex))
      setUndoHistory((prev) => [...prev, currentState])

      textareaRef.current.value = nextState.value
      textareaRef.current.setSelectionRange(
        nextState.selectionStart,
        nextState.selectionEnd
      )

      onChange?.(nextState.value)
      lastValueRef.current = nextState.value
      isUndoRedoActionRef.current = false
    }, [redoHistory, onChange])

    // Auto-completion on Enter (tables, lists, tasks)
    const handleAutoComplete = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (!textareaRef.current) return

      const textarea = textareaRef.current
      const start = textarea.selectionStart
      const text = textarea.value

      // Get current line
      const lineStart = text.lastIndexOf('\n', start - 1) + 1
      const lineEnd = text.indexOf('\n', start)
      const currentLine = text.substring(lineStart, lineEnd === -1 ? text.length : lineEnd)

      // Check if in table row: | col1 | col2 |
      const tableRowMatch = currentLine.match(/^\|(.+)\|$/)
      if (tableRowMatch) {
        e.preventDefault()
        const cells = tableRowMatch[1].split('|')
        const newRow = '\n|' + cells.map(() => '  ').join('|') + '|'

        // Insert new row AFTER current line
        const actualLineEnd = lineEnd === -1 ? text.length : lineEnd
        textarea.value = text.substring(0, actualLineEnd) + newRow + text.substring(actualLineEnd)

        // Position cursor in first cell of new row
        textarea.selectionStart = textarea.selectionEnd = actualLineEnd + 3 // After "\n| "
        lastValueRef.current = textarea.value
        return
      }

      // Check if in unordered list: - item, * item, + item
      const unorderedListMatch = currentLine.match(/^(\s*)([-*+])\s+(.*)$/)
      if (unorderedListMatch) {
        const [, indent, marker, content] = unorderedListMatch

        // If line has no content after marker, end the list
        if (!content.trim()) {
          e.preventDefault()
          // Remove the empty list item
          textarea.value = text.substring(0, lineStart) + '\n' + text.substring(start)
          textarea.selectionStart = textarea.selectionEnd = lineStart + 1
          lastValueRef.current = textarea.value
          return
        }

        // Continue the list
        e.preventDefault()
        const newItem = '\n' + indent + marker + ' '
        textarea.value = text.substring(0, start) + newItem + text.substring(start)
        textarea.selectionStart = textarea.selectionEnd = start + newItem.length
        lastValueRef.current = textarea.value
        return
      }

      // Check if in ordered list: 1. item, 2. item
      const orderedListMatch = currentLine.match(/^(\s*)(\d+)\.\s+(.*)$/)
      if (orderedListMatch) {
        const [, indent, number, content] = orderedListMatch

        // If line has no content after number, end the list
        if (!content.trim()) {
          e.preventDefault()
          // Remove the empty list item
          textarea.value = text.substring(0, lineStart) + '\n' + text.substring(start)
          textarea.selectionStart = textarea.selectionEnd = lineStart + 1
          lastValueRef.current = textarea.value
          return
        }

        // Continue the list with incremented number
        e.preventDefault()
        const nextNumber = parseInt(number) + 1
        const newItem = '\n' + indent + nextNumber + '. '
        textarea.value = text.substring(0, start) + newItem + text.substring(start)
        textarea.selectionStart = textarea.selectionEnd = start + newItem.length
        lastValueRef.current = textarea.value
        return
      }

      // Check if in task list: - [ ] task, - [x] task
      const taskListMatch = currentLine.match(/^(\s*)([-*+])\s+\[([ x])\]\s+(.*)$/i)
      if (taskListMatch) {
        const [, indent, marker, , content] = taskListMatch

        // If line has no content after checkbox, end the list
        if (!content.trim()) {
          e.preventDefault()
          // Remove the empty list item
          textarea.value = text.substring(0, lineStart) + '\n' + text.substring(start)
          textarea.selectionStart = textarea.selectionEnd = lineStart + 1
          lastValueRef.current = textarea.value
          return
        }

        // Continue the task list with unchecked item
        e.preventDefault()
        const newItem = '\n' + indent + marker + ' [ ] '
        textarea.value = text.substring(0, start) + newItem + text.substring(start)
        textarea.selectionStart = textarea.selectionEnd = start + newItem.length
        lastValueRef.current = textarea.value
        return
      }
    }, [])

    // Keyboard shortcuts
    const handleKeyDown = useCallback(
      (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
        const isMac = navigator.platform.toUpperCase().indexOf('MAC') >= 0
        const cmdOrCtrl = isMac ? e.metaKey : e.ctrlKey

        // Cmd+F - Open search
        if (cmdOrCtrl && e.key === 'f') {
          e.preventDefault()
          setShowReplaceInitially(false)
          setShowSearchPanel(true)
          return
        }

        // Cmd+H - Open search with replace
        if (cmdOrCtrl && e.key === 'h') {
          e.preventDefault()
          setShowReplaceInitially(true)
          setShowSearchPanel(true)
          return
        }

        // Escape - Close search panel
        if (e.key === 'Escape' && showSearchPanel) {
          e.preventDefault()
          setShowSearchPanel(false)
          return
        }

        // F3 - Next/Previous match (standard in many editors)
        if (e.key === 'F3' && showSearchPanel) {
          e.preventDefault()
          // Trigger findNext/findPrev via custom event
          const event = new CustomEvent(e.shiftKey ? 'searchFindPrev' : 'searchFindNext')
          document.dispatchEvent(event)
          return
        }

        // Cmd+G / Ctrl+G - Next/Previous match (Mac/VSCode style)
        if (cmdOrCtrl && e.key === 'g' && showSearchPanel) {
          e.preventDefault()
          const event = new CustomEvent(e.shiftKey ? 'searchFindPrev' : 'searchFindNext')
          document.dispatchEvent(event)
          return
        }

        // ESC - Close search panel
        if (e.key === 'Escape' && showSearchPanel) {
          e.preventDefault()
          setShowSearchPanel(false)
          setSearchMatches([])
          setCurrentSearchMatch(-1)
          return
        }

        // Enter - auto-complete tables and lists (only if search panel is closed)
        if (e.key === 'Enter' && !showSearchPanel) {
          handleAutoComplete(e)
          return
        }

        // Undo: Cmd/Ctrl + Z
        if (cmdOrCtrl && e.key.toLowerCase() === 'z' && !e.shiftKey) {
          e.preventDefault()
          handleUndo()
          return
        }

        // Redo: Cmd/Ctrl + Shift + Z or Cmd/Ctrl + Y
        if ((cmdOrCtrl && e.key.toLowerCase() === 'z' && e.shiftKey) || (cmdOrCtrl && e.key.toLowerCase() === 'y')) {
          e.preventDefault()
          handleRedo()
          return
        }
      },
      [handleUndo, handleRedo, handleAutoComplete, showSearchPanel]
    )

    // Insert markdown formatting
    const insertMarkdown = useCallback((type: string, value?: string) => {
      if (!textareaRef.current) return

      // Save current state before making changes
      saveCurrentState()

      const textarea = textareaRef.current
      const start = textarea.selectionStart
      const end = textarea.selectionEnd
      const selectedText = textarea.value.substring(start, end)
      const text = textarea.value
      let insertion = ''
      let cursorOffset = 0

      switch (type) {
        case 'bold':
          insertion = `**${selectedText || 'bold text'}**`
          cursorOffset = selectedText ? insertion.length : 2
          break

        case 'italic':
          insertion = `*${selectedText || 'italic text'}*`
          cursorOffset = selectedText ? insertion.length : 1
          break

        case 'heading':
          // Insert at start of line
          const lineStart = text.lastIndexOf('\n', start - 1) + 1
          textarea.value = text.substring(0, lineStart) + '# ' + text.substring(lineStart)
          textarea.selectionStart = textarea.selectionEnd = start + 2
          lastValueRef.current = textarea.value
          return

        case 'link':
          insertion = `[${selectedText || 'link text'}](url)`
          cursorOffset = selectedText ? insertion.length - 4 : 1
          break

        case 'image':
          insertion = `![${selectedText || 'alt text'}](image-url)`
          cursorOffset = selectedText ? insertion.length - 11 : 2
          break

        case 'code':
          insertion = `\`\`\`\n${selectedText || 'code here'}\n\`\`\``
          cursorOffset = selectedText ? 4 : 4
          break

        case 'mermaid':
          // Use value from Toolbar (full markdown with code)
          insertion = value || `\`\`\`mermaid\ngraph TD\n    A[Start] --> B[End]\n\`\`\``
          cursorOffset = insertion.length
          break

        case 'table':
          // Table passed as value from Toolbar
          insertion = value || '| Header1 | Header2 |\n| --- | --- |\n| Cell1 | Cell2 |'
          cursorOffset = insertion.length
          break

        default:
          return
      }

      // Insert text
      textarea.value = text.substring(0, start) + insertion + text.substring(end)
      textarea.selectionStart = textarea.selectionEnd = start + cursorOffset
      lastValueRef.current = textarea.value

      // Notify parent about content change
      onChange?.(textarea.value)

      // Focus textarea
      textarea.focus()
    }, [saveCurrentState, onChange])

    // Call onReady when component mounts (only once)
    useEffect(() => {
      if (onReady) {
        onReady(insertMarkdown)
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [])

    // Update highlights callback
    const handleUpdateHighlights = useCallback((matches: Array<{ index: number; length: number }>, currentMatch: number) => {
      setSearchMatches(matches)
      setCurrentSearchMatch(currentMatch)
    }, [])

    useEffect(() => {
      renderHighlights()
    }, [searchMatches, currentSearchMatch, renderHighlights])

    // Auto-scroll to current search match (without stealing focus)
    useEffect(() => {
      if (currentSearchMatch >= 0 && searchMatches.length > 0 && textareaRef.current && highlightRef.current) {
        const match = searchMatches[currentSearchMatch]
        if (!match) return

        const textarea = textareaRef.current
        const activeElement = document.activeElement

        // Set selection first
        textarea.setSelectionRange(match.index, match.index + match.length)

        // Use requestAnimationFrame to ensure DOM is updated
        requestAnimationFrame(() => {
          // Find the active highlight element
          const highlights = highlightRef.current?.querySelectorAll('.search-highlight.active')
          if (highlights && highlights.length > 0) {
            const activeHighlight = highlights[0] as HTMLElement

            // Get positions
            const highlightRect = activeHighlight.getBoundingClientRect()
            const textareaRect = textarea.getBoundingClientRect()

            // Calculate scroll offset needed to center the highlight
            const relativeTop = highlightRect.top - textareaRect.top
            const targetScrollTop = textarea.scrollTop + relativeTop - (textarea.clientHeight / 2) + (highlightRect.height / 2)

            // Instant scroll to position (smooth looks weird when scrolling up)
            textarea.scrollTop = Math.max(0, targetScrollTop)
          }

          // Restore focus to search input if it was there
          if (activeElement && activeElement.tagName === 'INPUT' &&
              (activeElement.id === 'search-input' || activeElement.id === 'replace-input')) {
            (activeElement as HTMLInputElement).focus()
          }
        })
      }
    }, [currentSearchMatch, searchMatches])

    // Initialize highlight on note load
    useEffect(() => {
      renderHighlights()
    }, [currentNote, renderHighlights])

    if (!currentNote) {
      return (
        <div ref={ref} className={`flex items-center justify-center ${className}`}>
          <p className="text-gray-500">No note selected</p>
        </div>
      )
    }

    return (
      <div ref={ref} className={`${className} flex flex-col`}>
        {/* Search Panel */}
        <SearchPanel
          visible={showSearchPanel}
          onClose={() => {
            setShowSearchPanel(false)
            setSearchMatches([])
            setCurrentSearchMatch(-1)
            setShowReplaceInitially(false)
          }}
          textareaRef={textareaRef}
          onUpdateHighlights={handleUpdateHighlights}
          onSyncScroll={handleScroll}
          showReplaceInitially={showReplaceInitially}
        />

        {/* Editor with highlight overlay - wrapper like in prototype */}
        <div className="flex-1 relative">
          {/* Highlight overlay behind textarea */}
          <div
            id="editor-highlight"
            ref={highlightRef}
            className="editor-highlight"
          />

          {/* Editor textarea */}
          <textarea
            id="editor"
            ref={textareaRef}
            className="absolute inset-0 editor-pane resize-none focus:outline-none overflow-y-auto border-0"
            style={{ padding: '16px' }}
            placeholder="Start writing..."
            defaultValue={content}
            onInput={handleInput}
            onScroll={handleScroll}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onCut={saveCurrentState}
          />
        </div>

        {/* Upload Modal */}
        <UploadModal
          visible={showUploadModal}
          file={uploadFile}
          onClose={() => {
            setShowUploadModal(false)
            setUploadFile(null)
          }}
          onConfirm={handleUploadConfirm}
        />
      </div>
    )
  }
)

EditorTextarea.displayName = 'EditorTextarea'

export default EditorTextarea
