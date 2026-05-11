import { useState, useEffect, useRef, useCallback } from 'react'
import { ChevronLeft, ChevronRight, Folder, FileText, Terminal, ChevronDown, ChevronUp } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'
import type { FolderNode } from '../../types/folder'
import type { ClaudeSession } from '../../types/claude'
import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu'
import { SwipeableItem } from '../common/SwipeableItem'
import NewNoteModal from '../modals/NewNoteModal'
import RenameModal from '../modals/RenameModal'
import DeleteConfirmModal from '../modals/DeleteConfirmModal'
import FolderProjectPathModal from '../modals/FolderProjectPathModal'
import { sessionsAPI } from '../../api/sessions'
import { foldersAPI } from '../../api/folders'
import { exportFolderToZip } from '../../utils/export'

interface SidebarProps {
  width?: number
  collapsed?: boolean
  onToggleCollapse?: (collapsed: boolean) => void
  onNoteSelect?: (note: any) => void
  onOpenChatWithNote?: (noteId: string) => void
  onSessionDeleted?: (sessionId: string) => void
  activeSessionId?: string
  mobileOpen?: boolean
  onMobileClose?: () => void
}

// Count all notes in folder and subfolders recursively
function countNotesInFolder(folder: FolderNode, notes: any[]): number {
  // Count notes in current folder
  const currentFolderNotes = notes.filter((note) => note.folder === folder.path).length

  // Count notes in all subfolders recursively
  const subfolderNotes = folder.children
    ? folder.children.reduce((sum, child) => sum + countNotesInFolder(child, notes), 0)
    : 0

  return currentFolderNotes + subfolderNotes
}

// Recursive folder tree node component
interface FolderTreeNodeProps {
  folder: FolderNode
  level: number
  notes: any[]
  currentNote: any
  collapsedFolders: Set<string>
  dragOverFolder: string | null
  onToggleFolder: (path: string) => void
  onNoteClick: (note: any) => void
  onFolderContextMenu: (e: React.MouseEvent, path: string) => void
  onNoteContextMenu: (e: React.MouseEvent, note: any) => void
  onFolderDragStart: (e: React.DragEvent, folder: FolderNode) => void
  onFolderDragEnd: (e: React.DragEvent) => void
  onFolderDragOver: (e: React.DragEvent, path: string) => void
  onFolderDragLeave: (e: React.DragEvent) => void
  onFolderDrop: (e: React.DragEvent, path: string) => void
  onNoteDragStart: (e: React.DragEvent, note: any) => void
  onNoteDragEnd: (e: React.DragEvent) => void
}

function FolderTreeNode({
  folder,
  level,
  notes,
  currentNote,
  collapsedFolders,
  dragOverFolder,
  onToggleFolder,
  onNoteClick,
  onFolderContextMenu,
  onNoteContextMenu,
  onFolderDragStart,
  onFolderDragEnd,
  onFolderDragOver,
  onFolderDragLeave,
  onFolderDrop,
  onNoteDragStart,
  onNoteDragEnd,
}: FolderTreeNodeProps) {
  const isCollapsed = collapsedFolders.has(folder.path)
  const folderNotes = notes.filter((note) => note.folder === folder.path)
  const totalNotesCount = countNotesInFolder(folder, notes)

  return (
    <div>
      {/* Folder item */}
      <button
        draggable
        onClick={() => onToggleFolder(folder.path)}
        onContextMenu={(e) => onFolderContextMenu(e, folder.path)}
        onDragStart={(e) => onFolderDragStart(e, folder)}
        onDragEnd={onFolderDragEnd}
        onDragOver={(e) => onFolderDragOver(e, folder.path)}
        onDragLeave={onFolderDragLeave}
        onDrop={(e) => onFolderDrop(e, folder.path)}
        className={`w-full flex items-center gap-1.5 py-1.5 hover:bg-white/[0.04] rounded text-left transition ${
          dragOverFolder === folder.path ? 'drag-over' : ''
        }`}
        style={{ paddingLeft: `${level * 12 + 8}px` }}
      >
        {isCollapsed ? (
          <ChevronRight className="w-3.5 h-3.5 text-slate-600" />
        ) : (
          <ChevronDown className="w-3.5 h-3.5 text-slate-600" />
        )}
        <Folder className="w-3.5 h-3.5 text-cyan-700" />
        <span className="text-xs font-mono text-slate-400">{folder.name}</span>
        <span className="ml-auto text-[10px] font-mono text-slate-700 pr-1">
          {totalNotesCount}
        </span>
      </button>

      {/* Notes in this folder (if not collapsed) */}
      {!isCollapsed && folderNotes.length > 0 && (
        <div className="mt-1 space-y-1">
          {folderNotes.map((note) => (
            <button
              key={note.id}
              draggable
              onClick={() => onNoteClick(note)}
              onContextMenu={(e) => onNoteContextMenu(e, note)}
              onDragStart={(e) => onNoteDragStart(e, note)}
              onDragEnd={onNoteDragEnd}
              className={`w-full flex items-center gap-2 py-1.5 rounded text-left transition ${
                currentNote?.id === note.id
                  ? 'bg-cyan-500/10 border-l-2 border-cyan-500/50'
                  : 'hover:bg-white/[0.04] border-l-2 border-transparent'
              }`}
              style={{ paddingLeft: `${(level + 1) * 12 + 6}px` }}
            >
              <FileText className={`w-3.5 h-3.5 flex-shrink-0 ${currentNote?.id === note.id ? 'text-cyan-500' : 'text-slate-600'}`} />
              <span className={`text-xs font-mono truncate ${currentNote?.id === note.id ? 'text-cyan-300' : 'text-slate-400'}`}>{note.title}</span>
            </button>
          ))}
        </div>
      )}

      {/* Child folders (recursive) */}
      {!isCollapsed && folder.children && folder.children.length > 0 && (
        <div className="mt-1">
          {folder.children.map((child) => (
            <FolderTreeNode
              key={child.path}
              folder={child}
              level={level + 1}
              notes={notes}
              currentNote={currentNote}
              collapsedFolders={collapsedFolders}
              dragOverFolder={dragOverFolder}
              onToggleFolder={onToggleFolder}
              onNoteClick={onNoteClick}
              onFolderContextMenu={onFolderContextMenu}
              onNoteContextMenu={onNoteContextMenu}
              onFolderDragStart={onFolderDragStart}
              onFolderDragEnd={onFolderDragEnd}
              onFolderDragOver={onFolderDragOver}
              onFolderDragLeave={onFolderDragLeave}
              onFolderDrop={onFolderDrop}
              onNoteDragStart={onNoteDragStart}
              onNoteDragEnd={onNoteDragEnd}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function Sidebar({ width = 256, collapsed = false, onToggleCollapse, onNoteSelect, onOpenChatWithNote, onSessionDeleted, activeSessionId, mobileOpen = false, onMobileClose }: SidebarProps) {
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768)
  useEffect(() => {
    const handler = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [])
  const { notes, folderTree, currentNote, fetchNotes, fetchFolders, createNote, createFolder, deleteNote, deleteFolder } = useNotes()

  // Helper to get all folder paths
  const getAllFolderPaths = (node: FolderNode | null): string[] => {
    if (!node) return []
    const paths: string[] = []
    const traverse = (n: FolderNode) => {
      if (n.path) paths.push(n.path)
      if (n.children) n.children.forEach(traverse)
    }
    traverse(node)
    return paths
  }

  // Initialize with all folders collapsed
  const [collapsedFolders, setCollapsedFolders] = useState<Set<string>>(() => {
    return new Set(getAllFolderPaths(folderTree))
  })

  // Flatten folder tree for legacy components (modals)
  const flattenedFolders = (folderNode: FolderNode): FolderNode[] => {
    const result: FolderNode[] = []
    const traverse = (node: FolderNode) => {
      if (node.path) result.push(node)
      if (node.children) node.children.forEach(traverse)
    }
    traverse(folderNode)
    return result
  }

  // Get parent folder paths for a given path
  const getParentPaths = (path: string): string[] => {
    const parts = path.split('/')
    const parents: string[] = []
    for (let i = 1; i < parts.length; i++) {
      parents.push(parts.slice(0, i).join('/'))
    }
    return parents
  }

  // Update collapsed folders when folder tree changes
  useEffect(() => {
    if (folderTree) {
      const allPaths = getAllFolderPaths(folderTree)
      setCollapsedFolders(new Set(allPaths))
    }
  }, [folderTree])

  // Auto-expand folder when note is selected
  useEffect(() => {
    if (currentNote && currentNote.folder) {
      setCollapsedFolders((prev) => {
        const newSet = new Set(prev)
        // Expand current folder
        newSet.delete(currentNote.folder)
        // Expand all parent folders
        const parents = getParentPaths(currentNote.folder)
        parents.forEach(parent => newSet.delete(parent))
        return newSet
      })
    }
  }, [currentNote?.id])
  const [toggleVisible, setToggleVisible] = useState(false)
  const [chatHistoryCollapsed, setChatHistoryCollapsed] = useState(false)
  const [claudeSessions, setClaudeSessions] = useState<ClaudeSession[]>([]) // Active Claude terminal sessions
  const [sessionsHeight, setSessionsHeight] = useState<number>(200) // Height of sessions list
  const sidebarRef = useRef<HTMLElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)
  const hoverTimeoutRef = useRef<number | null>(null)
  const sessionsResizeRef = useRef<HTMLDivElement>(null)

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
    data: typeof notes[0] | FolderNode
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

  const [folderProjectPathModal, setFolderProjectPathModal] = useState<{
    visible: boolean
    folderPath: string
    currentProjectPath: string
  }>({ visible: false, folderPath: '', currentProjectPath: '' })

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
    if (isMobile) onMobileClose?.()
  }

  const toggleSidebar = () => {
    onToggleCollapse?.(!collapsed)
  }

  const showToggle = () => {
    if (hoverTimeoutRef.current) {
      clearTimeout(hoverTimeoutRef.current)
    }
    setToggleVisible(true)
  }

  const hideToggle = () => {
    hoverTimeoutRef.current = window.setTimeout(() => {
      setToggleVisible(false)
    }, 100)
  }

  // Helper to find folder by path in tree
  const findFolderByPath = (tree: FolderNode, path: string): FolderNode | null => {
    if (tree.path === path) return tree
    if (tree.children) {
      for (const child of tree.children) {
        const found = findFolderByPath(child, path)
        if (found) return found
      }
    }
    return null
  }

  const handleFolderContextMenu = (e: React.MouseEvent, folderPath: string) => {
    e.preventDefault()
    e.stopPropagation()

    const folder = findFolderByPath(folderTree, folderPath)

    const items: ContextMenuItem[] = [
      {
        icon: 'fa-file-alt',
        text: 'New Note',
        action: () => setNewNoteModal({ visible: true, defaultFolder: folderPath }),
      },
      {
        icon: 'fa-folder-plus',
        text: 'New Folder',
        action: () => handleCreateFolder(folderPath),
      },
      {
        icon: 'fa-cog',
        text: 'Set Project Path',
        action: () => setFolderProjectPathModal({
          visible: true,
          folderPath: folderPath,
          currentProjectPath: folder?.projectPath || '',
        }),
      },
      {
        icon: 'fa-file-archive',
        text: 'Export as ZIP',
        action: () => handleExportFolderZip(folderPath),
      },
      { divider: true, text: '' },
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

  const handleExportFolderZip = async (folderPath: string) => {
    try {
      await exportFolderToZip(folderPath, notes || [])
    } catch (error) {
      console.error('Failed to export folder ZIP:', error)
      alert(error instanceof Error ? error.message : 'Failed to export folder')
    }
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
      { divider: true, text: '' },
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
    // Switch to note (via URL navigation)
    const note = (notes || []).find(n => n.id === noteId)
    if (note) {
      onNoteSelect?.(note)
    }

    // Open chat panel
    onOpenChatWithNote?.(noteId)
  }

  const closeContextMenu = () => {
    setContextMenu((prev) => ({ ...prev, visible: false }))
  }

  // Handle session deletion
  const handleDeleteSession = async (sessionId: string) => {
    try {
      await sessionsAPI.deleteSession(sessionId)
      // Remove from local state immediately
      setClaudeSessions(prev => prev.filter(s => s.id !== sessionId))
      // Notify parent that session was deleted (to close chat panel if needed)
      if (onSessionDeleted) {
        onSessionDeleted(sessionId)
      }
    } catch (error) {
      console.error('Failed to delete session:', error)
      alert('Failed to delete session')
    }
  }

  // Handle session context menu
  const handleSessionContextMenu = (e: React.MouseEvent, session: ClaudeSession) => {
    e.preventDefault()
    e.stopPropagation()

    setContextMenu({
      visible: true,
      x: e.clientX,
      y: e.clientY,
      items: [
        {
          text: 'Kill Session',
          icon: 'trash',
          action: () => handleDeleteSession(session.id),
          danger: true,
        },
      ],
    })
  }

  // Modal handlers
  const handleCreateNote = async (name: string, folder: string) => {
    console.log('Create note:', name, 'in folder:', folder)

    try {
      const newNote = await createNote(name, folder, '')
      // Open newly created note
      onNoteSelect?.(newNote)
      // Close modal
      setNewNoteModal({ visible: false, defaultFolder: '' })
    } catch (error) {
      console.error('Failed to create note:', error)
      alert('Failed to create note. See console for details.')
    }
  }

  const handleCreateFolder = async (parentPath: string) => {
    const folderName = prompt('Enter folder name:')
    if (!folderName) return

    const newPath = parentPath ? `${parentPath}/${folderName}` : folderName

    try {
      await createFolder(newPath)
    } catch (error) {
      console.error('Failed to create folder:', error)
      alert('Failed to create folder. See console for details.')
    }
  }

  const handleRename = async (newName: string) => {
    console.log('Rename', renameModal.type, 'to:', newName)

    try {
      if (renameModal.type === 'note') {
        // Find note by path to get ID
        const note = (notes || []).find(n => n.path === renameModal.itemPath)
        if (note) {
          // Update note title via API
          await fetch(`/api/notes/${note.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ title: newName })
          })

          // Refresh notes list
          await fetchNotes()
        }
      } else {
        // Rename folder by moving to new path
        const oldPath = renameModal.itemPath
        const pathParts = oldPath.split('/')
        pathParts[pathParts.length - 1] = newName
        const newPath = pathParts.join('/')

        // Call MoveFolder API
        await fetch('/api/folders/move', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ from: oldPath, to: newPath })
        })

        // Refresh folders list
        await fetchFolders()
      }

      // Close modal
      setRenameModal({ visible: false, type: 'note', currentName: '', itemPath: '' })
    } catch (error) {
      console.error('Failed to rename:', error)
      alert('Failed to rename. See console for details.')
    }
  }

  const handleDelete = async () => {
    console.log('Delete', deleteModal.type, ':', deleteModal.itemPath)

    try {
      if (deleteModal.type === 'note') {
        // Find note by path to get ID
        const note = (notes || []).find(n => n.path === deleteModal.itemPath)
        if (note) {
          await deleteNote(note.id)
        }
      } else {
        await deleteFolder(deleteModal.itemPath)
      }

      // Close modal
      setDeleteModal({ visible: false, type: 'note', itemName: '', itemPath: '' })
    } catch (error) {
      console.error('Failed to delete:', error)
      alert('Failed to delete. See console for details.')
    }
  }

  const handleUpdateFolderProjectPath = async (projectPath: string) => {
    try {
      await foldersAPI.updateFolder(folderProjectPathModal.folderPath, projectPath)

      // Refresh folders list
      await fetchFolders()

      // Close modal
      setFolderProjectPathModal({ visible: false, folderPath: '', currentProjectPath: '' })
    } catch (error) {
      console.error('Failed to update folder project path:', error)
      alert('Failed to update folder project path. See console for details.')
    }
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

  const handleFolderDragStart = (e: React.DragEvent, folder: FolderNode) => {
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
      const draggedFolder = draggedItem.data as FolderNode
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

  const handleFolderDrop = async (e: React.DragEvent, targetFolderPath: string) => {
    e.preventDefault()
    e.stopPropagation()

    if (!draggedItem) return

    try {
      if (draggedItem.type === 'note') {
        const note = draggedItem.data as typeof notes[0]

        // Don't move if already in this folder
        if (note.folder === targetFolderPath) {
          setDraggedItem(null)
          setDragOverFolder(null)
          return
        }

        console.log('Move note', note.title, 'to folder', targetFolderPath)

        // Update note folder via API
        const response = await fetch(`/api/notes/${note.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ folder: targetFolderPath })
        })

        if (!response.ok) {
          throw new Error(`Failed to update note: ${response.statusText}`)
        }

        console.log('Note moved successfully, refreshing...')

        // Refresh notes list
        await fetchNotes()
        console.log('Notes refreshed')
      } else if (draggedItem.type === 'folder') {
        const folder = draggedItem.data as FolderNode

        // Don't allow dropping folder into itself
        if (folder.path === targetFolderPath || targetFolderPath.startsWith(folder.path + '/')) {
          setDraggedItem(null)
          setDragOverFolder(null)
          return
        }

        console.log('Move folder', folder.path, 'into', targetFolderPath)

        // Construct new path: targetFolder/folderName
        const folderName = folder.path.split('/').pop() || folder.path
        const newPath = `${targetFolderPath}/${folderName}`

        console.log('Moving folder from', folder.path, 'to', newPath)

        // Move folder via API
        const response = await fetch('/api/folders/move', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ from: folder.path, to: newPath })
        })

        if (!response.ok) {
          throw new Error(`Failed to move folder: ${response.statusText}`)
        }

        console.log('Folder moved successfully, refreshing...')

        // Refresh both lists
        await fetchFolders()
        await fetchNotes()
        console.log('Lists refreshed')
      }
    } catch (error) {
      console.error('Failed to move item:', error)
      alert('Failed to move. See console for details.')
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

  // Handle sessions resize
  const handleSessionsResize = useCallback((clientY: number) => {
    if (!sidebarRef.current || !sessionsResizeRef.current) return

    const sidebarRect = sidebarRef.current.getBoundingClientRect()
    const newHeight = sidebarRect.bottom - clientY - 10 // 10px offset for better UX

    // Min height 100px, max height 500px
    const clampedHeight = Math.max(100, Math.min(500, newHeight))
    setSessionsHeight(clampedHeight)
  }, [])

  const handleRootDrop = async (e: React.DragEvent) => {
    e.preventDefault()
    e.stopPropagation()

    if (!draggedItem) return

    try {
      if (draggedItem.type === 'note') {
        const note = draggedItem.data as typeof notes[0]

        // Don't move if already in root
        if (!note.folder || note.folder === '') {
          setDraggedItem(null)
          setDragOverFolder(null)
          return
        }

        console.log('Move note', note.title, 'to root')

        // Update note folder to empty (root)
        const response = await fetch(`/api/notes/${note.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ folder: '' })
        })

        if (!response.ok) {
          throw new Error(`Failed to update note: ${response.statusText}`)
        }

        console.log('Note moved to root successfully, refreshing...')

        // Refresh notes list
        await fetchNotes()
        console.log('Notes refreshed')
      } else if (draggedItem.type === 'folder') {
        const folder = draggedItem.data as FolderNode

        // Check if already in root
        if (!folder.path.includes('/')) {
          setDraggedItem(null)
          setDragOverFolder(null)
          return
        }

        console.log('Move folder', folder.path, 'to root')

        // Get just the folder name (last part of path)
        const folderName = folder.path.split('/').pop() || folder.path

        console.log('Moving folder from', folder.path, 'to', folderName)

        // Move folder to root
        const response = await fetch('/api/folders/move', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ from: folder.path, to: folderName })
        })

        if (!response.ok) {
          throw new Error(`Failed to move folder: ${response.statusText}`)
        }

        console.log('Folder moved to root successfully, refreshing...')

        // Refresh both lists
        await fetchFolders()
        await fetchNotes()
        console.log('Lists refreshed')
      }
    } catch (error) {
      console.error('Failed to move item:', error)
      alert('Failed to move. See console for details.')
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

  // Load Claude terminal sessions
  useEffect(() => {
    const loadSessions = async () => {
      try {
        const sessions = await sessionsAPI.listActiveSessions()
        setClaudeSessions(sessions)
      } catch (error) {
        console.error('Failed to load Claude sessions:', error)
      }
    }

    loadSessions()
    // Refresh sessions every 10 seconds
    const interval = window.setInterval(loadSessions, 10000)
    return () => clearInterval(interval)
  }, [])

  return (
    <>
      {/* Mobile backdrop */}
      {isMobile && mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/60 backdrop-blur-sm"
          style={{ top: '56px' }}
          onClick={onMobileClose}
        />
      )}

      <aside
        id="sidebar"
        ref={sidebarRef}
        className={`bg-[#0a0b10] border-r border-[rgba(6,182,212,0.08)] flex flex-col flex-shrink-0 overflow-hidden ${
          isMobile
            ? `fixed left-0 bottom-0 z-40 w-72 transition-transform duration-200 ease-in-out ${mobileOpen ? 'translate-x-0' : '-translate-x-full'}`
            : `relative transition-none ${collapsed ? 'collapsed' : ''}`
        }`}
        style={isMobile ? { top: '56px' } : { width: collapsed ? '0' : `${width}px` }}
      >
        {/* Sidebar header */}
        <div className="h-14 border-b border-white/[0.06] flex items-center px-4">
          <span className="text-[10px] font-mono font-semibold tracking-widest text-slate-600 uppercase">Notes</span>
        </div>

      {/* Folder tree */}
      <div
        className="overflow-y-auto p-2"
        style={{
          height: chatHistoryCollapsed
            ? 'calc(100vh - 56px - 40px - 40px)' // header - footer - sessions header
            : `calc(100vh - 56px - 40px - ${sessionsHeight + 40}px)` // ... - sessions list - sessions header
        }}
      >
        <div className="space-y-1">
          {/* Recursive folder tree */}
          {folderTree.children && folderTree.children.map((folder) => (
            <FolderTreeNode
              key={folder.path}
              folder={folder}
              level={0}
              notes={notes || []}
              currentNote={currentNote}
              collapsedFolders={collapsedFolders}
              dragOverFolder={dragOverFolder}
              onToggleFolder={toggleFolder}
              onNoteClick={handleNoteClick}
              onFolderContextMenu={handleFolderContextMenu}
              onNoteContextMenu={handleNoteContextMenu}
              onFolderDragStart={handleFolderDragStart}
              onFolderDragEnd={handleFolderDragEnd}
              onFolderDragOver={handleFolderDragOver}
              onFolderDragLeave={handleFolderDragLeave}
              onFolderDrop={handleFolderDrop}
              onNoteDragStart={handleNoteDragStart}
              onNoteDragEnd={handleNoteDragEnd}
            />
          ))}

          {/* Root notes (notes without folder) */}
          {(notes || [])
            .filter((note) => !note.folder || note.folder === '')
            .map((note) => (
              <button
                key={note.id}
                draggable
                onClick={() => handleNoteClick(note)}
                onContextMenu={(e) => handleNoteContextMenu(e, note)}
                onDragStart={(e) => handleNoteDragStart(e, note)}
                onDragEnd={handleNoteDragEnd}
                className={`w-full flex items-center gap-2 px-2 py-1.5 rounded text-left transition ${
                  currentNote?.id === note.id
                    ? 'bg-cyan-500/10 border-l-2 border-cyan-500/50 pl-1.5'
                    : 'hover:bg-white/[0.04] border-l-2 border-transparent'
                }`}
              >
                <FileText className={`w-3.5 h-3.5 flex-shrink-0 ${currentNote?.id === note.id ? 'text-cyan-500' : 'text-slate-600'}`} />
                <span className={`text-xs font-mono truncate ${currentNote?.id === note.id ? 'text-cyan-300' : 'text-slate-400'}`}>{note.title}</span>
              </button>
            ))}
        </div>

        {/* Invisible drop zone for root */}
        <div
          className={`h-12 mt-2 rounded transition-colors ${
            dragOverFolder === 'root' ? 'bg-cyan-500/[0.05] border border-dashed border-cyan-500/30' : ''
          }`}
          onDragOver={handleRootDragOver}
          onDragLeave={handleRootDragLeave}
          onDrop={handleRootDrop}
        >
          {dragOverFolder === 'root' && (
            <div className="flex items-center justify-center h-full text-[10px] font-mono text-cyan-600 tracking-wider uppercase">
              drop to root
            </div>
          )}
        </div>
      </div>

      {/* Sidebar footer */}
      <div className="border-t border-white/[0.06] px-4 py-2">
        <span className="text-[10px] font-mono text-slate-700">
          {(notes || []).length} notes
        </span>
      </div>

      {/* Resize handle for sessions */}
      {!chatHistoryCollapsed && (
        <div
          ref={sessionsResizeRef}
          className="h-px bg-white/[0.06] hover:bg-cyan-500/30 cursor-ns-resize transition-colors"
          onMouseDown={(e) => {
            e.preventDefault()

            const handleMouseMove = (moveEvent: MouseEvent) => {
              handleSessionsResize(moveEvent.clientY)
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

      {/* Claude Sessions */}
      <div className="border-t border-white/[0.06]">
        {/* Sessions header */}
        <div
          className="flex items-center justify-between px-4 py-2.5 hover:bg-white/[0.02] cursor-pointer transition"
          onClick={() => setChatHistoryCollapsed(!chatHistoryCollapsed)}
        >
          <div className="flex items-center gap-2">
            <Terminal className="w-3.5 h-3.5 text-purple-500" />
            <span className="text-[10px] font-mono font-semibold tracking-widest text-slate-600 uppercase">Claude Sessions</span>
            {claudeSessions && claudeSessions.length > 0 && (
              <span className="text-[10px] font-mono bg-purple-500/10 text-purple-400 px-1.5 py-0.5 rounded border border-purple-500/20">
                {claudeSessions.length}
              </span>
            )}
          </div>
          <div className="flex items-center">
            {chatHistoryCollapsed ? (
              <ChevronDown className="w-3.5 h-3.5 text-slate-600" />
            ) : (
              <ChevronUp className="w-3.5 h-3.5 text-slate-600" />
            )}
          </div>
        </div>

        {/* Sessions list */}
        {!chatHistoryCollapsed && (
          <div className="overflow-y-auto px-2 pb-2" style={{ height: `${sessionsHeight}px` }}>
            {!claudeSessions || claudeSessions.length === 0 ? (
              <div className="text-[10px] font-mono text-slate-700 px-2 py-4 text-center tracking-wider uppercase">
                no active sessions
              </div>
            ) : (
              <div className="space-y-0.5">
                {claudeSessions.map(session => {
                  const sessionName = session.name && session.name !== 'Terminal Session'
                    ? session.name
                    : session.id.startsWith('note-') ? 'Note Session' : 'Global Session'

                  const isActive = activeSessionId === session.id

                  return (
                    <button
                      key={session.id}
                      onClick={() => {
                        if (onOpenChatWithNote) {
                          const noteId = session.id.startsWith('note-') ? session.id.replace('note-', '') : null
                          if (noteId) {
                            onOpenChatWithNote(noteId)
                          } else {
                            onOpenChatWithNote('')
                          }
                        }
                      }}
                      onContextMenu={(e) => handleSessionContextMenu(e, session)}
                      className={`w-full flex items-start gap-2 px-2 py-1.5 rounded transition text-left ${
                        isActive
                          ? 'bg-purple-500/10 border border-purple-500/20'
                          : 'hover:bg-white/[0.03] border border-transparent'
                      }`}
                    >
                      <Terminal className={`w-3 h-3 flex-shrink-0 mt-0.5 ${
                        isActive ? 'text-purple-400' : 'text-purple-600'
                      }`} />
                      <div className="flex-1 min-w-0">
                        <div className={`text-xs font-mono truncate ${
                          isActive ? 'text-purple-300' : 'text-slate-400'
                        }`}>
                          {sessionName}
                        </div>
                        <div className="text-[10px] font-mono text-slate-700 truncate">
                          {session.workingDir}
                        </div>
                      </div>
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </div>
    </aside>

      {/* Sidebar toggle button — hidden on mobile via CSS */}
      <button
        ref={toggleRef}
        className={`sidebar-toggle ${toggleVisible ? 'visible' : ''} ${
          collapsed ? 'collapsed' : ''
        }`}
        style={{ left: collapsed ? '0' : `${width}px` }}
        onClick={toggleSidebar}
        title={collapsed ? 'Show sidebar' : 'Hide sidebar'}
      >
        {collapsed ? (
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
        folders={flattenedFolders(folderTree)}
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

      <FolderProjectPathModal
        visible={folderProjectPathModal.visible}
        folderPath={folderProjectPathModal.folderPath}
        currentProjectPath={folderProjectPathModal.currentProjectPath}
        onClose={() => setFolderProjectPathModal({ visible: false, folderPath: '', currentProjectPath: '' })}
        onSave={handleUpdateFolderProjectPath}
      />
    </>
  )
}

export default Sidebar
