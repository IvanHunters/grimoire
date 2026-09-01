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
import NoteSettings from '../components/editor/NoteSettings'
import TagPicker from '../components/editor/TagPicker'
import Preview from '../components/preview/Preview'
import GraphView from '../components/graph/GraphView'
import ChatPanel from '../components/chat/ChatPanel'

function HomePage() {
  const { noteId } = useParams<{ noteId: string }>()
  const navigate = useNavigate()
  const { notes, currentNote, setCurrentNote, updateNote, fetchNotes } = useNotes()

  // Load saved view mode from localStorage, default to 'preview'
  const [viewMode, setViewMode] = useState<ViewMode>(() => {
    const saved = localStorage.getItem('viewMode')
    return (saved === 'editor' || saved === 'split' || saved === 'preview') ? saved : 'preview'
  })

  const [editorWidth, setEditorWidth] = useState<number | null>(null)
  const [previewWidth, setPreviewWidth] = useState<number | null>(null)
  const [sidebarWidth, setSidebarWidth] = useState<number>(256) // Default 256px (w-64)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [insertMarkdown, setInsertMarkdown] = useState<((type: string, value?: string) => void) | null>(null)
  const [editorContent, setEditorContent] = useState<string>('')
  const [editorTags, setEditorTags] = useState<string[]>([])
  const [showGraphView, setShowGraphView] = useState(false)
  const [showChatPanel, setShowChatPanel] = useState(false)
  const [chatNoteId, setChatNoteId] = useState<string | null>(null)
  const [mountedChatNoteIds, setMountedChatNoteIds] = useState<string[]>([])
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)
  const [mobilePanel, setMobilePanel] = useState<'editor' | 'preview'>('editor')
  // Sidebar attach modal: when user clicks a non-note session in the
  // sidebar list, we open this modal in 'open' mode to render the
  // existing PTY without spawning anything new.
  const [attachSessionId, setAttachSessionId] = useState<string | null>(null)
  const [attachSessionName, setAttachSessionName] = useState<string>('')
  // When the user forks a session from the ChatPanel kebab, we mount a
  // fresh TerminalChat with resumeFromSessionId=source + resumeFork=true.
  // attachForkSourceId holds the SOURCE's full UUID for the next mount;
  // it's null once the fork is established (after first init).
  const [attachForkSourceId, setAttachForkSourceId] = useState<string | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const editorRef = useRef<HTMLDivElement>(null)
  const previewRef = useRef<HTMLDivElement>(null)

  // Save view mode to localStorage when changed
  useEffect(() => {
    localStorage.setItem('viewMode', viewMode)
  }, [viewMode])

  // Update editor content and tags when note changes (only when switching notes, not on every update)
  useEffect(() => {
    if (currentNote) {
      setEditorContent(currentNote.content)
      setEditorTags(currentNote.tags || [])
    }
  }, [currentNote?.id])

  // Sync tags from currentNote when updated externally (e.g., via MCP)
  // Only update if tags actually changed to avoid re-triggering auto-save
  useEffect(() => {
    if (currentNote && currentNote.tags) {
      const currentTagsStr = JSON.stringify(currentNote.tags.sort())
      const editorTagsStr = JSON.stringify([...editorTags].sort())
      if (currentTagsStr !== editorTagsStr) {
        setEditorTags(currentNote.tags)
      }
    }
  }, [currentNote?.tags])

  // Sync content from currentNote when updated externally (e.g., via MCP)
  const lastUserEditRef = useRef<number>(Date.now())
  const lastSyncedContentRef = useRef<string>('')

  useEffect(() => {
    if (!currentNote) return

    // Skip if this is the same content we already synced
    if (currentNote.content === lastSyncedContentRef.current) return

    // Skip if content matches what user is editing
    if (currentNote.content === editorContent) {
      lastSyncedContentRef.current = currentNote.content
      return
    }

    // Check if user was editing recently (within last 2 seconds)
    const timeSinceLastEdit = Date.now() - lastUserEditRef.current

    if (timeSinceLastEdit > 2000) {
      // User hasn't edited recently, safe to update
      console.log('[Sync] Updating editor content from external change (MCP)')
      setEditorContent(currentNote.content)
      lastSyncedContentRef.current = currentNote.content
    } else {
      // User is actively editing, show warning
      console.warn('[Sync] Content conflict: user editing while MCP updated note. User changes will be saved.')
      // User's auto-save will overwrite MCP changes in 1 second
      // This is expected behavior - user has priority
    }
  }, [currentNote?.content])

  // Track user edits
  const handleContentChangeTracked = useCallback((content: string) => {
    lastUserEditRef.current = Date.now()
    setEditorContent(content)
  }, [])

  // Reset scroll positions when switching notes
  useEffect(() => {
    if (currentNote) {
      // Reset editor scroll
      if (editorRef.current) {
        const textarea = editorRef.current.querySelector('textarea')
        if (textarea) {
          textarea.scrollTop = 0
        }
      }
      // Reset preview scroll
      if (previewRef.current) {
        previewRef.current.scrollTop = 0
      }
    }
  }, [currentNote?.id]) // Only when note ID changes, not content

  // Open note from URL parameter
  useEffect(() => {
    if (noteId && notes.length > 0) {
      const note = notes.find(n => n.id === noteId)
      if (note && note.id !== currentNote?.id) {
        setCurrentNote(note)
      } else if (!note) {
        // Note not found - silently redirect to home
        console.error('Note not found:', noteId)
        navigate('/')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [noteId, notes, currentNote?.id, setCurrentNote])

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
        return newEditorWidth
      }
      return prev
    })
    setPreviewWidth(prev => {
      if (prev === null || Math.abs(prev - newPreviewWidth) > 5) {
        return newPreviewWidth
      }
      return prev
    })
  }, [])

  const handleSidebarResize = useCallback((clientX: number) => {
    // Min width 200px, max width 600px
    const newWidth = Math.max(200, Math.min(600, clientX))
    setSidebarWidth(newWidth)
  }, [])

  const handleTagsChange = (tags: string[]) => {
    setEditorTags(tags)
  }

  const handleProjectPathChange = async (projectPath: string) => {
    if (!currentNote) return

    try {
      await fetch(`/api/notes/${currentNote.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ projectPath })
      })

      // Update local note
      await fetchNotes()
    } catch (error) {
      console.error('Failed to update project path:', error)
      alert('Failed to update project path')
    }
  }

  // Auto-save content and tags with debounce
  useEffect(() => {
    if (!currentNote) return

    const contentChanged = editorContent !== currentNote.content
    const tagsChanged = JSON.stringify(editorTags) !== JSON.stringify(currentNote.tags || [])

    if (!contentChanged && !tagsChanged) {
      return
    }

    const timeoutId = setTimeout(() => {
      updateNote(
        currentNote.id,
        contentChanged ? editorContent : undefined,
        tagsChanged ? editorTags : undefined
      ).catch((error) => {
        console.error('Failed to save note:', error)
      })
    }, 1000) // 1 second debounce

    return () => clearTimeout(timeoutId)
  }, [editorContent, editorTags, currentNote, updateNote])

  const handleNoteSelect = (note: typeof notes[0]) => {
    // Update URL to reflect selected note
    navigate(`/notes/${note.id}`)
    // setCurrentNote will be called by useEffect watching noteId
  }

  const openChat = useCallback((noteId: string) => {
    setChatNoteId(noteId)
    setShowChatPanel(true)
    setMountedChatNoteIds(prev => prev.includes(noteId) ? prev : [...prev, noteId])
  }, [])

  const handleOpenChatWithNote = (noteId: string) => {
    const note = notes.find(n => n.id === noteId)
    if (note) {
      navigate(`/notes/${note.id}`)
    }
    openChat(noteId)
    setMobileSidebarOpen(false)
  }

  // Handle session deletion - close ANY chat panel currently showing
  // the deleted session. Two paths exist depending on how the panel
  // was opened: note-bound (chatNoteId state) or sidebar-attached
  // (attachSessionId state). Without closing both, the WS in the
  // still-open panel reconnects to a dead session and respawns it.
  const handleSessionDeleted = (deletedSessionId: string) => {
    const deletedNoteId = deletedSessionId.startsWith('note-') ? deletedSessionId.slice(5) : deletedSessionId
    setMountedChatNoteIds(prev => prev.filter(id => id !== deletedNoteId))
    if (showChatPanel && chatNoteId) {
      const currentSessionId = `note-${chatNoteId}`
      if (currentSessionId === deletedSessionId) {
        setShowChatPanel(false)
        setChatNoteId(null)
      }
    }
    if (attachSessionId === deletedSessionId) {
      setAttachSessionId(null)
      setAttachSessionName('')
      setAttachForkSourceId(null)
    }
  }

  // Synchronized scroll (only in split view)
  useSyncScroll({
    editorRef,
    previewRef,
    enabled: viewMode === 'split' && !showWelcome,
  })

  // Sidebar context-menu "Fork…" dispatches this event with the
  // source session id + the user-given name. Run the same flow the
  // in-terminal kebab Fork uses: generate a new client UUID, route
  // to attach mode, ChatPanel mounts with resume props and dispatches
  // claude --bg --resume <source> --fork-session.
  useEffect(() => {
    const handler = (e: Event) => {
      const ce = e as CustomEvent<{ sourceId: string; name: string }>
      const { sourceId, name } = ce.detail || ({} as { sourceId: string; name: string })
      if (!sourceId || !name) return
      const newId = crypto.randomUUID()
      setAttachForkSourceId(sourceId)
      setAttachSessionName(name)
      setAttachSessionId(newId)
      const refresh = () => window.dispatchEvent(new CustomEvent('claude-sessions-refresh'))
      setTimeout(refresh, 1200)
      setTimeout(refresh, 2500)
    }
    window.addEventListener('fork-session-request', handler)
    return () => window.removeEventListener('fork-session-request', handler)
  }, [])

  // Initialize and restore panel widths
  useLayoutEffect(() => {
    if (viewMode === 'split' && containerRef.current) {
      const container = containerRef.current
      const editorPanel = container.querySelector('.editor-panel') as HTMLElement
      const previewPanel = container.querySelector('.preview-panel') as HTMLElement

      if (!editorPanel || !previewPanel) return

      // On mobile: active panel takes full width, no fixed pixel widths
      if (window.innerWidth < 768) {
        if (mobilePanel === 'editor') {
          editorPanel.style.width = '100%'
          editorPanel.style.flex = '1'
          previewPanel.style.width = ''
          previewPanel.style.flex = ''
        } else {
          previewPanel.style.width = '100%'
          previewPanel.style.flex = '1'
          editorPanel.style.width = ''
          editorPanel.style.flex = ''
        }
        return
      }

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
  }, [viewMode, editorWidth, previewWidth, currentNote, mobilePanel])

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <Header
        onNoteSelect={handleNoteSelect}
        previewRef={previewRef}
        onToggleMobileSidebar={() => setMobileSidebarOpen(p => !p)}
        onCloseMobileSidebar={() => setMobileSidebarOpen(false)}
        mobileSidebarOpen={mobileSidebarOpen}
      />

      {/* Main content area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Sidebar */}
        <Sidebar
          width={sidebarWidth}
          collapsed={sidebarCollapsed}
          onToggleCollapse={setSidebarCollapsed}
          onNoteSelect={handleNoteSelect}
          onOpenChatWithNote={handleOpenChatWithNote}
          onAttachToSession={(id, name) => {
            // Regular sidebar click — NOT a fork action. Reset the
            // fork-source state so the next ChatPanel mount doesn't
            // re-send resumeFromSessionId/fork in its WS init,
            // which would spawn yet another fork against the OLD
            // source (Bug from fork audit).
            setAttachForkSourceId(null)
            setAttachSessionId(id)
            setAttachSessionName(name)
          }}
          onSessionDeleted={handleSessionDeleted}
          activeSessionId={attachSessionId ?? (showChatPanel && chatNoteId ? `note-${chatNoteId}` : undefined)}
          mobileOpen={mobileSidebarOpen}
          onMobileClose={() => setMobileSidebarOpen(false)}
        />

        {/* Sidebar resize handle - hidden when collapsed or on mobile */}
        {!sidebarCollapsed && (
          <div
            className="hidden md:block w-2 bg-gray-200 hover:bg-purple-400 cursor-ew-resize transition-colors flex-shrink-0 dark:bg-gray-700 dark:hover:bg-purple-500"
            onMouseDown={(e) => {
            e.preventDefault()
            const startX = e.clientX
            const startWidth = sidebarWidth

            const handleMouseMove = (moveEvent: MouseEvent) => {
              const deltaX = moveEvent.clientX - startX
              handleSidebarResize(startWidth + deltaX)
            }

            const handleMouseUp = () => {
              document.removeEventListener('mousemove', handleMouseMove)
              document.removeEventListener('mouseup', handleMouseUp)
            }

            document.addEventListener('mousemove', handleMouseMove)
            document.addEventListener('mouseup', handleMouseUp)
          }}
          />
        )}

        {/* Editor/Preview area */}
        <div className="flex-1 flex flex-col bg-gray-50 dark:bg-gray-900 min-w-0">
          {showWelcome ? (
            <WelcomeScreen />
          ) : (
            <>
              {/* Toolbar */}
              <Toolbar
                onInsertMarkdown={(type, value) => insertMarkdown?.(type, value)}
                currentNotePath={currentNote.path}
                onToggleGraph={() => setShowGraphView(true)}
                onToggleChat={() => {
                  if (!showChatPanel) {
                    openChat(currentNote.id)
                  } else {
                    setShowChatPanel(false)
                    setChatNoteId(null)
                  }
                }}
                viewMode={viewMode}
                onViewModeChange={setViewMode}
              />

              {/* Mobile panel toggle for split mode */}
              {viewMode === 'split' && (
                <div className="md:hidden flex border-b border-[rgba(6,182,212,0.08)]" style={{ background: '#0a0b10' }}>
                  <button
                    onClick={() => setMobilePanel('editor')}
                    className={`flex-1 min-h-[44px] text-xs font-mono transition-colors ${mobilePanel === 'editor' ? 'text-cyan-400 border-b-2 border-cyan-500' : 'text-slate-600'}`}
                  >
                    editor
                  </button>
                  <button
                    onClick={() => setMobilePanel('preview')}
                    className={`flex-1 min-h-[44px] text-xs font-mono transition-colors ${mobilePanel === 'preview' ? 'text-cyan-400 border-b-2 border-cyan-500' : 'text-slate-600'}`}
                  >
                    preview
                  </button>
                </div>
              )}

              {/* Editor and Preview panels */}
              <div className="flex-1 flex overflow-hidden" ref={containerRef}>
                {/* Editor panel */}
                {(viewMode === 'editor' || viewMode === 'split') && (
                  <div
                    className={`editor-panel flex flex-col overflow-hidden relative border-r border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800 ${
                      viewMode === 'editor' ? 'w-full' : viewMode === 'split' && !editorWidth ? 'flex-1' : ''
                    } ${viewMode === 'split' && mobilePanel === 'preview' ? 'hidden md:flex' : ''}`}
                  >
                    <NoteSettings
                      projectPath={currentNote.projectPath}
                      onProjectPathChange={handleProjectPathChange}
                    />
                    <div className="px-4 py-2 border-b border-gray-200 dark:border-gray-700">
                      <TagPicker
                        tags={editorTags}
                        onChange={handleTagsChange}
                      />
                    </div>
                    <EditorTextarea
                      ref={editorRef}
                      className="flex-1"
                      content={editorContent}
                      onChange={handleContentChangeTracked}
                      onReady={(fn) => setInsertMarkdown(() => fn)}
                    />
                  </div>
                )}

                {/* Resize handle (only in split view, desktop only) */}
                {viewMode === 'split' && (
                  <div className="hidden md:block">
                    <ResizeHandle onResize={handleResize} containerRef={containerRef} />
                  </div>
                )}

                {/* Preview panel */}
                {(viewMode === 'preview' || viewMode === 'split') && (
                  <div
                    className={`preview-panel bg-white overflow-hidden dark:bg-gray-800 ${
                      viewMode === 'preview' ? 'w-full' : viewMode === 'split' && !previewWidth ? 'flex-1' : ''
                    } ${viewMode === 'split' && mobilePanel === 'editor' ? 'hidden md:block' : ''}`}
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

      {/* Claude Chat Panel — all visited sessions stay mounted to preserve terminal state */}
      {mountedChatNoteIds.map(nId => (
        <ChatPanel
          key={nId}
          visible={showChatPanel && chatNoteId === nId}
          onClose={() => {
            setShowChatPanel(false)
            setChatNoteId(null)
          }}
          noteId={nId}
          onCloseMobileSidebar={() => setMobileSidebarOpen(false)}
        />
      ))}

      {/* Sidebar-driven attach: reuses ChatPanel (same right-side
          terminal panel with mobile keyboard / status badge / paste
          overlay that note-bound chats use) but with an explicit
          sessionId instead of a note. Closing here keeps the daemon
          worker alive; reopening from sidebar re-attaches. */}
      {attachSessionId && (
        <ChatPanel
          // Re-key on the active sessionId so a fork forces a fresh
          // TerminalChat mount — otherwise React reuses the old component
          // and never re-sends the resume init.
          key={attachSessionId}
          visible={!!attachSessionId}
          customSessionId={attachSessionId}
          customSessionName={attachSessionName}
          resumeFromSessionId={attachForkSourceId ?? undefined}
          resumeFork={!!attachForkSourceId}
          onClose={() => {
            setAttachSessionId(null)
            setAttachSessionName('')
            setAttachForkSourceId(null)
          }}
          onCloseMobileSidebar={() => setMobileSidebarOpen(false)}
          onForked={(newId, newName, sourceId) => {
            // Switch the panel to the new fork. Backend will dispatch
            // `claude --resume <sourceId> --fork-session` on next init.
            setAttachForkSourceId(sourceId)
            setAttachSessionName(newName)
            setAttachSessionId(newId)
            // Backend takes ~1s to populate m.sessions after Dispatch
            // returns (sleep 400ms + attach). Schedule a few sidebar
            // refreshes so the fork's row appears with its proper
            // user-given name as soon as the manager session exists
            // — instead of waiting up to 3s for the regular poll.
            const refresh = () => window.dispatchEvent(new CustomEvent('claude-sessions-refresh'))
            setTimeout(refresh, 1200)
            setTimeout(refresh, 2500)
          }}
        />
      )}
    </div>
  )
}

export default HomePage
