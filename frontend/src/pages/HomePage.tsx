import { useState, useRef, useEffect, useCallback, useLayoutEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useNotes } from '../contexts/NotesContext'
import type { ViewMode } from '../types/ui'
import { useSyncScroll } from '../hooks/useSyncScroll'
import WelcomeScreen from '../components/common/WelcomeScreen'
import ResizeHandle from '../components/common/ResizeHandle'
import Header from '../components/layout/Header'
import Sidebar from '../components/layout/Sidebar'
import EditorTextarea from '../components/editor/EditorTextarea'
import Toolbar from '../components/editor/Toolbar'
import Preview from '../components/preview/Preview'
import GraphView from '../components/graph/GraphView'
import ChatPanel from '../components/chat/ChatPanel'

function HomePage() {
  const { noteId } = useParams<{ noteId: string }>()
  const navigate = useNavigate()
  const { notes, currentNote, setCurrentNote, updateNote } = useNotes()
  const [viewMode, setViewMode] = useState<ViewMode>('split')
  const [editorWidth, setEditorWidth] = useState<number | null>(null)
  const [previewWidth, setPreviewWidth] = useState<number | null>(null)
  const [insertMarkdown, setInsertMarkdown] = useState<((type: string, value?: string) => void) | null>(null)
  const [editorContent, setEditorContent] = useState<string>('')
  const [showGraphView, setShowGraphView] = useState(false)
  const [showChatPanel, setShowChatPanel] = useState(false)
  const [chatNoteId, setChatNoteId] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const editorRef = useRef<HTMLDivElement>(null)
  const previewRef = useRef<HTMLDivElement>(null)

  // Update editor content when note changes
  useEffect(() => {
    if (currentNote) {
      setEditorContent(currentNote.content)
    }
  }, [currentNote])

  // Open note from URL parameter
  useEffect(() => {
    if (noteId && notes.length > 0) {
      const note = notes.find(n => n.id === noteId)
      if (note && note.id !== currentNote?.id) {
        setCurrentNote(note)
      } else if (!note) {
        // Note not found - show error and redirect to home
        console.error('Note not found:', noteId)
        alert(`Note not found (ID: ${noteId}). Redirecting to home.`)
        navigate('/')
      }
    }
  }, [noteId, notes, currentNote?.id, setCurrentNote, navigate])

  // Reset resize widths when switching from split view
  useEffect(() => {
    if (viewMode !== 'split') {
      setEditorWidth(null)
      setPreviewWidth(null)

      // Clear inline styles set by ResizeHandle
      if (containerRef.current) {
        const editorPanel = containerRef.current.querySelector('.editor-panel') as HTMLElement
        const previewPanel = containerRef.current.querySelector('.preview-panel') as HTMLElement

        if (editorPanel) {
          editorPanel.style.width = ''
          editorPanel.style.flex = ''
        }

        if (previewPanel) {
          previewPanel.style.width = ''
          previewPanel.style.flex = ''
        }
      }
    }
  }, [viewMode])

  // Show welcome screen only when no note is selected
  const showWelcome = !currentNote

  const handleResize = useCallback((newEditorWidth: number, newPreviewWidth: number) => {
    // Only update if sizes changed significantly (more than 5px)
    setEditorWidth(prev => {
      if (prev === null || Math.abs(prev - newEditorWidth) > 5) {
        console.log('Editor width updated:', prev, '->', newEditorWidth)
        return newEditorWidth
      }
      return prev
    })
    setPreviewWidth(prev => {
      if (prev === null || Math.abs(prev - newPreviewWidth) > 5) {
        console.log('Preview width updated:', prev, '->', newPreviewWidth)
        return newPreviewWidth
      }
      return prev
    })
  }, [])

  const handleContentChange = (content: string) => {
    setEditorContent(content)
  }

  // Auto-save content with debounce
  useEffect(() => {
    if (!currentNote || editorContent === currentNote.content) {
      return
    }

    const timeoutId = setTimeout(() => {
      console.log('Auto-saving note:', currentNote.id)
      updateNote(currentNote.id, editorContent).catch((error) => {
        console.error('Failed to save note:', error)
      })
    }, 1000) // 1 second debounce

    return () => clearTimeout(timeoutId)
  }, [editorContent, currentNote, updateNote])

  const handleNoteSelect = (note: typeof notes[0]) => {
    // Update URL to reflect selected note
    navigate(`/notes/${note.id}`)
    // setCurrentNote will be called by useEffect watching noteId
  }

  const handleOpenChatWithNote = (noteId: string) => {
    setChatNoteId(noteId)
    setShowChatPanel(true)
  }

  // Synchronized scroll (only in split view)
  useSyncScroll({
    editorRef,
    previewRef,
    enabled: viewMode === 'split' && !showWelcome,
  })

  // Initialize and restore panel widths
  useLayoutEffect(() => {
    if (viewMode === 'split' && containerRef.current) {
      const container = containerRef.current
      const editorPanel = container.querySelector('.editor-panel') as HTMLElement
      const previewPanel = container.querySelector('.preview-panel') as HTMLElement

      if (!editorPanel || !previewPanel) return

      // Initialize to 50/50 if not set
      if (editorWidth === null || previewWidth === null) {
        const containerWidth = container.offsetWidth
        const handleWidth = 4
        const half = (containerWidth - handleWidth) / 2

        editorPanel.style.width = `${half}px`
        editorPanel.style.flex = 'none'
        previewPanel.style.width = `${half}px`
        previewPanel.style.flex = 'none'

        // Save to state (will trigger re-render but that's ok)
        setEditorWidth(half)
        setPreviewWidth(half)
      } else {
        // Restore from state
        editorPanel.style.width = `${editorWidth}px`
        editorPanel.style.flex = 'none'
        previewPanel.style.width = `${previewWidth}px`
        previewPanel.style.flex = 'none'
      }
    }
  }, [viewMode, editorWidth, previewWidth, currentNote])

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <Header onNoteSelect={handleNoteSelect} />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar */}
        <Sidebar
          onNoteSelect={handleNoteSelect}
          onOpenChatWithNote={handleOpenChatWithNote}
          currentChatNoteId={chatNoteId}
        />

        {/* Editor/Preview area */}
        <div className="flex-1 flex flex-col bg-gray-50">
          {showWelcome ? (
            <WelcomeScreen />
          ) : (
            <>
              {/* Toolbar */}
              <Toolbar
                onInsertMarkdown={(type, value) => insertMarkdown?.(type, value)}
                currentNotePath={currentNote.path}
                onToggleGraph={() => setShowGraphView(true)}
                onToggleChat={() => setShowChatPanel(prev => !prev)}
                viewMode={viewMode}
                onViewModeChange={setViewMode}
              />

              {/* Editor and Preview panels */}
              <div className="flex-1 flex overflow-hidden" ref={containerRef}>
                {/* Editor panel */}
                {(viewMode === 'editor' || viewMode === 'split') && (
                  <div
                    className={`editor-panel flex flex-col overflow-hidden relative border-r border-gray-200 bg-white ${
                      viewMode === 'editor' ? 'w-full' : viewMode === 'split' && !editorWidth ? 'flex-1' : ''
                    }`}
                  >
                    <EditorTextarea
                      ref={editorRef}
                      className="flex-1"
                      content={editorContent}
                      onChange={handleContentChange}
                      onReady={(fn) => setInsertMarkdown(() => fn)}
                    />
                  </div>
                )}

                {/* Resize handle (only in split view) */}
                {viewMode === 'split' && (
                  <ResizeHandle onResize={handleResize} containerRef={containerRef} />
                )}

                {/* Preview panel */}
                {(viewMode === 'preview' || viewMode === 'split') && (
                  <div
                    className={`preview-panel bg-white ${
                      viewMode === 'preview' ? 'w-full' : viewMode === 'split' && !previewWidth ? 'flex-1' : ''
                    }`}
                  >
                    <Preview ref={previewRef} className="h-full" content={editorContent} />
                  </div>
                )}
              </div>
            </>
          )}
        </div>

      </div>

      {/* Graph View Overlay */}
      <GraphView
        visible={showGraphView}
        onClose={() => setShowGraphView(false)}
      />

      {/* Claude Chat Panel */}
      <ChatPanel
        visible={showChatPanel}
        onClose={() => setShowChatPanel(false)}
        noteId={chatNoteId}
      />
    </div>
  )
}

export default HomePage
