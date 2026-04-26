import { useState, useEffect, useRef } from 'react'
import { ChevronLeft, ChevronRight, Folder, FileText, MessageSquare, ChevronDown, ChevronUp, ArrowRight, Trash2, Star } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'
import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu'
import NewNoteModal from '../modals/NewNoteModal'
import RenameModal from '../modals/RenameModal'
import DeleteConfirmModal from '../modals/DeleteConfirmModal'

interface SidebarProps {
  onNoteSelect?: (note: any) => void
  onOpenChatWithNote?: (noteId: string) => void
  currentChatNoteId?: string | null
}

function Sidebar({ onNoteSelect, onOpenChatWithNote, currentChatNoteId }: SidebarProps) {
  const { notes, folders, currentNote } = useNotes()
  const [collapsedFolders, setCollapsedFolders] = useState<Set<string>>(
    new Set(folders.filter((f) => f.isCollapsed).map((f) => f.path))
  )
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [toggleVisible, setToggleVisible] = useState(false)
  const [chatHistoryCollapsed, setChatHistoryCollapsed] = useState(false)
  const [chatHistory, setChatHistory] = useState<string[]>([]) // Note IDs with chat history
  const sidebarRef = useRef<HTMLElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)
  const hoverTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Context menu state
  const [contextMenu, setContextMenu] = useState<{
    visible: boolean
    x: number
    y: number
    items: ContextMenuItem[]
  }>({
    visible: false,
    x: 0,
    y: 0,
    items: [],
  })

  // Drag and drop state
  const [draggedItem, setDraggedItem] = useState<{
    type: 'note' | 'folder'
    data: typeof notes[0] | typeof folders[0]
  } | null>(null)
  const [dragOverFolder, setDragOverFolder] = useState<string | null>(null)

  // Modals state
  const [newNoteModal, setNewNoteModal] = useState<{
    visible: boolean
    defaultFolder: string
  }>({ visible: false, defaultFolder: '' })

  const [renameModal, setRenameModal] = useState<{
    visible: boolean
    type: 'note' | 'folder'
    currentName: string
    itemPath: string
  }>({ visible: false, type: 'note', currentName: '', itemPath: '' })

  const [deleteModal, setDeleteModal] = useState<{
    visible: boolean
    type: 'note' | 'folder'
    itemName: string
    itemPath: string
  }>({ visible: false, type: 'note', itemName: '', itemPath: '' })

  const toggleFolder = (path: string) => {
    setCollapsedFolders((prev) => {
      const next = new Set(prev)
      if (next.has(path)) {
        next.delete(path)
      } else {
        next.add(path)
      }
      return next
    })
  }

  const handleNoteClick = (note: typeof notes[0]) => {
    onNoteSelect?.(note)
  }

  const toggleSidebar = () => {
    setSidebarCollapsed((prev) => !prev)
  }

  const showToggle = () => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current)
    }
    setToggleVisible(true)
  }

  const hideToggle = () => {
    hoverTimeoutRef.current = setTimeout(() => {
      setToggleVisible(false)
    }, 100)
  }

  const handleFolderContextMenu = (e: React.MouseEvent, folderPath: string) => {
    e.preventDefault()
    e.stopPropagation()

    const folder = folders.find((f) => f.path === folderPath)

    const items: ContextMenuItem[] = [
      {
        icon: 'fa-file-alt',
        text: 'New Note',
        action: () => setNewNoteModal({ visible: true, defaultFolder: folderPath }),
      },
      {
        icon: 'fa-folder-plus',
        text: 'New Folder',
        action: () => setNewNoteModal({ visible: true, defaultFolder: folderPath }),
      },
      { divider: true },
      {
        icon: 'fa-trash',
        text: 'Delete Folder',
        action: () => setDeleteModal({
          visible: true,
          type: 'folder',
          itemName: folder?.name || folderPath,
          itemPath: folderPath,
        }),
        danger: true,
      },
    ]

    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      items,
    })
  }

  const handleNoteContextMenu = (e: React.MouseEvent, note: typeof notes[0]) => {
    e.preventDefault()
    e.stopPropagation()

    const items: ContextMenuItem[] = [
      {
        icon: 'fa-edit',
        text: 'Rename',
        action: () => setRenameModal({
          visible: true,
          type: 'note',
          currentName: note.title,
          itemPath: note.path,
        }),
      },
      {
        icon: 'fa-comments',
        text: 'Chat with this note',
        action: () => handleOpenChatWithNote(note.id),
      },
      { divider: true },
      {
        icon: 'fa-trash',
        text: 'Delete Note',
        action: () => setDeleteModal({
          visible: true,
          type: 'note',
          itemName: note.title,
          itemPath: note.path,
        }),
        danger: true,
      },
    ]

    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      items,
    })
  }

  const handleOpenChatWithNote = (noteId: string) => {
    // Add to chat history if not already there
    if (!chatHistory.includes(noteId)) {
      setChatHistory(prev => [noteId, ...prev])
    }

    // Switch to note (via URL navigation)
    const note = notes.find(n => n.id === noteId)
    if (note) {
      onNoteSelect?.(note)
    }

    // Open chat panel
    onOpenChatWithNote?.(noteId)
  }

  const handleRemoveFromHistory = (noteId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setChatHistory(prev => prev.filter(id => id !== noteId))
  }

  const handleClearHistory = () => {
    if (confirm('Очистить всю историю чатов?')) {
      setChatHistory([])
    }
  }

  const closeContextMenu = () => {
    setContextMenu((prev) => ({ ...prev, visible: false }))
  }

  // Modal handlers
  const handleCreateNote = (name: string, folder: string) => {
    console.log('Create note:', name, 'in folder:', folder)
    // TODO: API call to create note
    // createNote({ title: name, folder, content: '' })
  }

  const handleRename = (newName: string) => {
    console.log('Rename', renameModal.type, 'to:', newName)
    // TODO: API call to rename
    // if (renameModal.type === 'note') {
    //   updateNote(renameModal.itemPath, { title: newName })
    // } else {
    //   renameFolder(renameModal.itemPath, newName)
    // }
  }

  const handleDelete = () => {
    console.log('Delete', deleteModal.type, ':', deleteModal.itemPath)
    // TODO: API call to delete
    // if (deleteModal.type === 'note') {
    //   deleteNote(deleteModal.itemPath)
    // } else {
    //   deleteFolder(deleteModal.itemPath)
    // }
  }

  // Drag and drop handlers
  const handleNoteDragStart = (e: React.DragEvent, note: typeof notes[0]) => {
    setDraggedItem({ type: 'note', data: note })
    e.currentTarget.classList.add('dragging')
  }

  const handleNoteDragEnd = (e: React.DragEvent) => {
    e.currentTarget.classList.remove('dragging')
    setDraggedItem(null)
    setDragOverFolder(null)
  }

  const handleFolderDragStart = (e: React.DragEvent, folder: typeof folders[0]) => {
    setDraggedItem({ type: 'folder', data: folder })
    e.currentTarget.classList.add('dragging')
  }

  const handleFolderDragEnd = (e: React.DragEvent) => {
    e.currentTarget.classList.remove('dragging')
    setDraggedItem(null)
    setDragOverFolder(null)
  }

  const handleFolderDragOver = (e: React.DragEvent, folderPath: string) => {
    e.preventDefault()

    if (!draggedItem) return

    // Don't allow dropping folder into itself or its children
    if (draggedItem.type === 'folder') {
      const draggedFolder = draggedItem.data as typeof folders[0]
      if (folderPath === draggedFolder.path || folderPath.startsWith(draggedFolder.path + '/')) {
        return
      }
    }

    setDragOverFolder(folderPath)
  }

  const handleFolderDragLeave = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOverFolder(null)
  }

  const handleFolderDrop = (e: React.DragEvent, targetFolderPath: string) => {
    e.preventDefault()
    e.stopPropagation()

    if (!draggedItem) return

    if (draggedItem.type === 'note') {
      const note = draggedItem.data as typeof notes[0]

      // Don't move if already in this folder
      if (note.folder === targetFolderPath) {
        setDraggedItem(null)
        setDragOverFolder(null)
        return
      }

      console.log('Move note', note.title, 'to folder', targetFolderPath)
      // TODO: API call to update note.folder
      // updateNote(note.id, { folder: targetFolderPath })
    } else if (draggedItem.type === 'folder') {
      const folder = draggedItem.data as typeof folders[0]

      // Don't allow dropping folder into itself
      if (folder.path === targetFolderPath || targetFolderPath.startsWith(folder.path + '/')) {
        setDraggedItem(null)
        setDragOverFolder(null)
        return
      }

      console.log('Move folder', folder.path, 'into', targetFolderPath)
      // TODO: API call to move folder
      // moveFolder(folder.path, targetFolderPath)
    }

    setDraggedItem(null)
    setDragOverFolder(null)
  }

  // Drop in root (outside folders)
  const handleRootDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOverFolder('root')
  }

  const handleRootDragLeave = (e: React.DragEvent) => {
    e.preventDefault()
    setDragOverFolder(null)
  }

  const handleRootDrop = (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()

    if (!draggedItem) return

    if (draggedItem.type === 'note') {
      const note = draggedItem.data as typeof notes[0]

      // Don't move if already in root
      if (!note.folder || note.folder === '') {
        setDraggedItem(null)
        setDragOverFolder(null)
        return
      }

      console.log('Move note', note.title, 'to root')
      // TODO: API call to update note.folder to empty/root
      // updateNote(note.id, { folder: '' })
    } else if (draggedItem.type === 'folder') {
      const folder = draggedItem.data as typeof folders[0]

      // Check if already in root
      if (!folder.path.includes('/')) {
        setDraggedItem(null)
        setDragOverFolder(null)
        return
      }

      console.log('Move folder', folder.path, 'to root')
      // TODO: API call to move folder to root
      // moveFolder(folder.path, '')
    }

    setDraggedItem(null)
    setDragOverFolder(null)
  }

  // Setup hover listeners
  useEffect(() => {
    const sidebar = sidebarRef.current
    const toggle = toggleRef.current

    if (!sidebar || !toggle) return

    sidebar.addEventListener('mouseenter', showToggle)
    sidebar.addEventListener('mouseleave', hideToggle)
    toggle.addEventListener('mouseenter', showToggle)
    toggle.addEventListener('mouseleave', hideToggle)

    return () => {
      sidebar.removeEventListener('mouseenter', showToggle)
      sidebar.removeEventListener('mouseleave', hideToggle)
      toggle.removeEventListener('mouseenter', showToggle)
      toggle.removeEventListener('mouseleave', hideToggle)

      if (hoverTimeoutRef.current) {
        clearTimeout(hoverTimeoutRef.current)
      }
    }
  }, [])

  return (
    <>
      <aside
        id="sidebar"
        ref={sidebarRef}
        className={`w-64 bg-white border-r border-gray-200 flex flex-col ${
          sidebarCollapsed ? 'collapsed' : ''
        }`}
      >
        {/* Sidebar header */}
        <div className="h-14 border-b border-gray-200 flex items-center px-4">
          <h2 className="font-semibold text-gray-900">Notes</h2>
        </div>

      {/* Folder tree */}
      <div className="flex-1 overflow-y-auto p-2">
        <div className="space-y-1">
          {folders.map((folder) => {
            const isCollapsed = collapsedFolders.has(folder.path)
            const folderNotes = notes.filter((note) => note.folder === folder.path)

            return (
              <div key={folder.path}>
                {/* Folder item */}
                <button
                  draggable
                  onClick={() => toggleFolder(folder.path)}
                  onContextMenu={(e) => handleFolderContextMenu(e, folder.path)}
                  onDragStart={(e) => handleFolderDragStart(e, folder)}
                  onDragEnd={handleFolderDragEnd}
                  onDragOver={(e) => handleFolderDragOver(e, folder.path)}
                  onDragLeave={handleFolderDragLeave}
                  onDrop={(e) => handleFolderDrop(e, folder.path)}
                  className={`w-full flex items-center gap-2 px-2 py-1.5 hover:bg-gray-100 rounded text-left transition ${
                    dragOverFolder === folder.path ? 'drag-over' : ''
                  }`}
                >
                  {isCollapsed ? (
                    <ChevronRight className="w-4 h-4 text-gray-500" />
                  ) : (
                    <ChevronLeft className="w-4 h-4 text-gray-500" />
                  )}
                  <Folder className="w-4 h-4 text-gray-600" />
                  <span className="text-sm text-gray-900">{folder.name}</span>
                  <span className="ml-auto text-xs text-gray-500">
                    {folderNotes.length}
                  </span>
                </button>

                {/* Notes in folder (if not collapsed) */}
                {!isCollapsed && (
                  <div className="ml-6 mt-1 space-y-1">
                    {folderNotes.map((note) => (
                      <button
                        key={note.id}
                        draggable
                        onClick={() => handleNoteClick(note)}
                        onContextMenu={(e) => handleNoteContextMenu(e, note)}
                        onDragStart={(e) => handleNoteDragStart(e, note)}
                        onDragEnd={handleNoteDragEnd}
                        className={`w-full flex items-center gap-2 px-2 py-1.5 rounded text-left transition ${
                          currentNote?.id === note.id
                            ? 'bg-purple-100 text-purple-900'
                            : 'hover:bg-gray-100 text-gray-700'
                        }`}
                      >
                        <FileText className="w-4 h-4 text-gray-500" />
                        <span className="text-sm truncate">{note.title}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>

      {/* Chat History */}
      <div className="border-t border-gray-200">
        {/* Chat History header */}
        <div className="flex items-center justify-between px-4 py-2 bg-gray-50 hover:bg-gray-100 cursor-pointer transition" onClick={() => setChatHistoryCollapsed(!chatHistoryCollapsed)}>
          <div className="flex items-center gap-2">
            <MessageSquare className="w-4 h-4 text-purple-600" />
            <span className="text-sm font-medium text-gray-900">Chat History</span>
            {chatHistory.length > 0 && (
              <span className="text-xs bg-purple-100 text-purple-700 px-1.5 py-0.5 rounded-full">
                {chatHistory.length}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            {chatHistory.length > 0 && (
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  handleClearHistory()
                }}
                className="text-xs text-gray-400 hover:text-red-600 p-1 rounded hover:bg-red-50 transition"
                title="Clear all history"
              >
                <Trash2 className="w-3 h-3" />
              </button>
            )}
            {chatHistoryCollapsed ? (
              <ChevronDown className="w-4 h-4 text-gray-500" />
            ) : (
              <ChevronUp className="w-4 h-4 text-gray-500" />
            )}
          </div>
        </div>

        {/* Chat History list */}
        {!chatHistoryCollapsed && (
          <div className="max-h-48 overflow-y-auto p-2">
            {chatHistory.length === 0 ? (
              <div className="text-xs text-gray-500 px-2 py-4 text-center">
                No chat history yet
              </div>
            ) : (
              <div className="space-y-1">
                {chatHistory.map(noteId => {
                  const note = notes.find(n => n.id === noteId)
                  const isActive = currentChatNoteId === noteId
                  const isOrphaned = !note

                  return (
                    <div
                      key={noteId}
                      className={`flex items-center gap-2 px-2 py-1.5 rounded transition group ${
                        isActive
                          ? 'bg-purple-100 text-purple-900'
                          : isOrphaned
                          ? 'bg-gray-50 text-gray-400'
                          : 'hover:bg-gray-100 text-gray-700'
                      }`}
                    >
                      {/* Icon */}
                      {isActive ? (
                        <Star className="w-3.5 h-3.5 text-purple-600 fill-purple-600 flex-shrink-0" />
                      ) : (
                        <MessageSquare className={`w-3.5 h-3.5 flex-shrink-0 ${isOrphaned ? 'text-gray-300' : 'text-gray-500'}`} />
                      )}

                      {/* Note name */}
                      <button
                        onClick={() => handleOpenChatWithNote(noteId)}
                        className="flex-1 text-left text-sm truncate"
                        disabled={isOrphaned}
                      >
                        {note ? note.title : '(Deleted note)'}
                      </button>

                      {/* Action buttons (hidden until hover) */}
                      <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                        {!isActive && !isOrphaned && (
                          <button
                            onClick={() => handleOpenChatWithNote(noteId)}
                            className="p-1 hover:bg-purple-100 rounded transition"
                            title="Switch to this chat"
                          >
                            <ArrowRight className="w-3 h-3 text-purple-600" />
                          </button>
                        )}
                        <button
                          onClick={(e) => handleRemoveFromHistory(noteId, e)}
                          className="p-1 hover:bg-red-100 rounded transition"
                          title="Remove from history"
                        >
                          <Trash2 className="w-3 h-3 text-red-600" />
                        </button>
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Sidebar footer - drop zone for root */}
      <div
        className={`border-t border-gray-200 p-2 ${
          dragOverFolder === 'root' ? 'drag-over' : ''
        }`}
        onDragOver={handleRootDragOver}
        onDragLeave={handleRootDragLeave}
        onDrop={handleRootDrop}
      >
        <div className="text-xs text-gray-500 px-2">
          {notes.length} notes
        </div>
      </div>
    </aside>

      {/* Sidebar toggle button */}
      <button
        ref={toggleRef}
        className={`sidebar-toggle ${toggleVisible ? 'visible' : ''} ${
          sidebarCollapsed ? 'collapsed' : ''
        }`}
        onClick={toggleSidebar}
        title={sidebarCollapsed ? 'Show sidebar' : 'Hide sidebar'}
      >
        {sidebarCollapsed ? (
          <ChevronRight className="w-3 h-3" />
        ) : (
          <ChevronLeft className="w-3 h-3" />
        )}
      </button>

      {/* Context Menu */}
      <ContextMenu
        visible={contextMenu.visible}
        x={contextMenu.x}
        y={contextMenu.y}
        items={contextMenu.items}
        onClose={closeContextMenu}
      />

      {/* Modals */}
      <NewNoteModal
        visible={newNoteModal.visible}
        onClose={() => setNewNoteModal({ visible: false, defaultFolder: '' })}
        folders={folders}
        defaultFolder={newNoteModal.defaultFolder}
        onCreate={handleCreateNote}
      />

      <RenameModal
        visible={renameModal.visible}
        onClose={() => setRenameModal({ visible: false, type: 'note', currentName: '', itemPath: '' })}
        type={renameModal.type}
        currentName={renameModal.currentName}
        onRename={handleRename}
      />

      <DeleteConfirmModal
        visible={deleteModal.visible}
        onClose={() => setDeleteModal({ visible: false, type: 'note', itemName: '', itemPath: '' })}
        type={deleteModal.type}
        itemName={deleteModal.itemName}
        onConfirm={handleDelete}
      />
    </>
  )
}

export default Sidebar
