import { useState, useEffect, useRef, useCallback } from 'react'
import { ChevronLeft, ChevronRight, Folder, FileText, Terminal, ChevronDown, ChevronUp, Settings2, Sparkles, Plus } from 'lucide-react'
import { useNotes } from '../../contexts/NotesContext'
import type { FolderNode } from '../../types/folder'
import type { ClaudeSession } from '../../types/claude'
import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu'
import { SwipeableItem } from '../common/SwipeableItem'
import NewNoteModal from '../modals/NewNoteModal'
import NewFolderModal from '../modals/NewFolderModal'
import NewSkillModal from '../modals/NewSkillModal'
import RenameModal from '../modals/RenameModal'
import DeleteConfirmModal from '../modals/DeleteConfirmModal'
import FolderProjectPathModal from '../modals/FolderProjectPathModal'
import { sessionsAPI } from '../../api/sessions'
import { foldersAPI } from '../../api/folders'
import { skillsAPI, type SkillSummary } from '../../api/skills'
import { exportFolderToZip } from '../../utils/export'
import { SessionStatusPill, formatSessionAge } from '../sessions/SessionStatusPill'

interface SidebarProps {
  width?: number
  collapsed?: boolean
  onToggleCollapse?: (collapsed: boolean) => void
  onNoteSelect?: (note: any) => void
  onOpenChatWithNote?: (noteId: string) => void
  /**
   * Called when the user clicks a non-note session in the sidebar
   * list (Quick Terminal tabs, daemon-only sessions, etc). The parent
   * should open an attach modal so the user can interact with the
   * existing live session. sessionId here is whatever the backend
   * keyed the session by (often the daemon UUID).
   */
  onAttachToSession?: (sessionId: string, sessionName: string) => void
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
  skillStates?: Map<string, SkillSummary>
  onNewSkill?: () => void
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
  skillStates,
  onNewSkill,
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
  const isSkillsRoot = folder.isSystem && folder.source === 'skills'
  // A skill folder is a direct child of Skills/ — show enabled/disabled badge from skillStates.
  const isSkillFolder = folder.path.startsWith('Skills/') && !folder.path.slice('Skills/'.length).includes('/')
  const skillState = isSkillFolder ? skillStates?.get(folder.path.slice('Skills/'.length)) : undefined

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
        {isSkillsRoot ? (
          <Sparkles className="w-3.5 h-3.5 text-amber-500" />
        ) : (
          <Folder className={`w-3.5 h-3.5 ${isSkillFolder ? 'text-amber-700' : 'text-cyan-700'}`} />
        )}
        <span className={`text-xs font-mono ${isSkillsRoot ? 'text-amber-400' : 'text-slate-400'}`}>{folder.name}</span>
        {skillState && !skillState.enabled && (
          <span className="text-[9px] font-mono text-slate-700 uppercase ml-1">off</span>
        )}
        {skillState && !skillState.valid && (
          <span className="text-[9px] font-mono text-red-500/80 uppercase ml-1" title="invalid frontmatter">!</span>
        )}
        {isSkillsRoot && onNewSkill && (
          <span
            role="button"
            tabIndex={0}
            onClick={(e) => { e.stopPropagation(); onNewSkill() }}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); onNewSkill() } }}
            title="New skill"
            className="ml-auto p-0.5 text-amber-500/70 hover:text-amber-400 cursor-pointer"
          >
            <Plus className="w-3 h-3" />
          </span>
        )}
        {!isSkillsRoot && (
          <span className="ml-auto text-[10px] font-mono text-slate-700 pr-1">
            {totalNotesCount}
          </span>
        )}
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
              skillStates={skillStates}
              onNewSkill={onNewSkill}
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

function Sidebar({ width = 256, collapsed = false, onToggleCollapse, onNoteSelect, onOpenChatWithNote, onAttachToSession, onSessionDeleted, activeSessionId, mobileOpen = false, onMobileClose }: SidebarProps) {
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768)
  useEffect(() => {
    const handler = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [])
  const { notes, folderTree, currentNote, fetchNotes, fetchFolders, createNote, createFolder, deleteNote, deleteFolder } = useNotes()

  // Skills state: map of skill name to summary, used to render badges in the Skills/ subtree.
  const [skillStates, setSkillStates] = useState<Map<string, SkillSummary>>(new Map())
  const [newSkillVisible, setNewSkillVisible] = useState(false)

  const refreshSkills = useCallback(async () => {
    try {
      const list = await skillsAPI.list()
      const map = new Map<string, SkillSummary>()
      for (const s of list) map.set(s.name, s)
      setSkillStates(map)
    } catch (err) {
      console.warn('[Sidebar] failed to load skills:', err)
    }
  }, [])

  useEffect(() => {
    refreshSkills()
  }, [refreshSkills])

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
  // Track all folder paths we've ever seen to detect genuinely new folders
  const seenFolderPathsRef = useRef<Set<string>>(new Set())

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

  // Update collapsed folders when folder tree changes — preserve expanded state
  useEffect(() => {
    if (folderTree) {
      const allPaths = getAllFolderPaths(folderTree)
      const allPathsSet = new Set(allPaths)
      setCollapsedFolders(prev => {
        const next = new Set(prev)
        // Collapse only genuinely new folders (never seen before)
        allPaths.forEach(path => {
          if (!seenFolderPathsRef.current.has(path)) {
            next.add(path)
          }
        })
        // Remove paths that no longer exist
        next.forEach(path => {
          if (!allPathsSet.has(path)) next.delete(path)
        })
        return next
      })
      seenFolderPathsRef.current = allPathsSet
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

  const [newFolderModal, setNewFolderModal] = useState<{
    visible: boolean
    parentPath: string
  }>({ visible: false, parentPath: '' })
  const [renamingSessionId, setRenamingSessionId] = useState<string | null>(null)
  const [renamingSessionValue, setRenamingSessionValue] = useState('')
  const [sessionOrder, setSessionOrder] = useState<string[]>(() => {
    try { return JSON.parse(localStorage.getItem('session-order') ?? '[]') } catch { return [] }
  })
  const [dragSessionId, setDragSessionId] = useState<string | null>(null)
  const [dragOverIdx, setDragOverIdx] = useState<number | null>(null)

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
      // deleteTranscript:true so the JSONL on disk goes too. Without
      // this, the live worker is killed but the historical row keeps
      // coming back on the next sidebar poll — looks like "delete
      // didn't work".
      await sessionsAPI.deleteSession(sessionId, { deleteTranscript: true })
      // Remove from local state immediately
      setClaudeSessions(prev => prev.filter(s => s.id !== sessionId))
      // Notify parent that session was deleted (to close chat panel if needed)
      if (onSessionDeleted) {
        onSessionDeleted(sessionId)
      }
      // Drop the matching tab from GlobalTerminalPanel's localStorage.
      // Without this, the panel restores the (now-dead) tab on its
      // next open and respawns the session, which looks to the user
      // like "I deleted it but it keeps coming back".
      try {
        const raw = localStorage.getItem('global-terminal-tabs')
        if (raw) {
          const tabs = JSON.parse(raw) as Array<{ sessionId: string; label: string }>
          const filtered = tabs.filter(t => t.sessionId !== sessionId)
          if (filtered.length !== tabs.length) {
            localStorage.setItem('global-terminal-tabs', JSON.stringify(filtered))
            // Notify any open GlobalTerminalPanel to refresh its tabs.
            window.dispatchEvent(new CustomEvent('global-terminal-tabs-changed'))
          }
        }
      } catch {}
    } catch (error) {
      console.error('Failed to delete session:', error)
      alert('Failed to delete session')
    }
  }

  const handleCommitRename = async (sessionId: string, newName: string) => {
    const trimmed = newName.trim()
    setRenamingSessionId(null)
    if (!trimmed) return
    try {
      await sessionsAPI.renameSession(sessionId, trimmed)
      setClaudeSessions(prev => prev.map(s => s.id === sessionId ? { ...s, name: trimmed } : s))
    } catch {
      // silently ignore
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
          text: 'Rename',
          icon: 'edit',
          action: () => {
            setRenamingSessionId(session.id)
            setRenamingSessionValue(session.name || '')
          },
        },
        {
          text: 'Fork…',
          icon: 'code-branch',
          action: () => {
            const suggested = session.name ? `${session.name} (fork)` : 'fork'
            const name = window.prompt('Имя для форка:', suggested)
            if (name == null) return
            const trimmed = name.trim()
            if (!trimmed) return
            // HomePage listens for this event and runs the same
            // onForked flow as the in-terminal kebab Fork action.
            window.dispatchEvent(new CustomEvent('fork-session-request', {
              detail: { sourceId: session.id, name: trimmed },
            }))
          },
        },
        {
          text: 'Export .jsonl',
          icon: 'download',
          action: () => {
            // Stream the session's JSONL transcript directly from the
            // backend via Content-Disposition. Same flow as the chat
            // panel's kebab Export — no JS heap allocation for big
            // transcripts.
            const a = document.createElement('a')
            a.href = `/api/sessions/${session.id}/jsonl`
            a.download = `${session.name || session.id}.jsonl`
            document.body.appendChild(a)
            a.click()
            a.remove()
          },
        },
        {
          text: 'Compact + restart',
          icon: 'archive',
          action: async () => {
            // Deterministic transcript shrink + restart so the live
            // daemon worker reloads from the smaller JSONL. Without
            // the restart, the in-memory conversation still carries
            // the full context and the user sees zero benefit until a
            // separate restart.
            try {
              const r = await fetch(`/api/sessions/${encodeURIComponent(session.id)}/compact`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ generate_ledger: true }),
              })
              if (!r.ok) throw new Error(`HTTP ${r.status}`)
              const j = await r.json()
              console.info('[compact]', {
                bytes: `${j.bytes_before} to ${j.bytes_after}`,
                tokens: `${j.approx_tokens_before} to ${j.approx_tokens_after}`,
                evicted: `${j.tool_results_evicted}/${j.tool_results}`,
                archive: j.archive_path, ledger: j.ledger_path,
              })
              // Ask whatever ChatPanel currently owns this session to
              // restart. HomePage / GlobalTerminalPanel listen and call
              // the standard restart path (terminalRef.current.restart)
              // which the kebab Restart button also uses.
              window.dispatchEvent(new CustomEvent('claude-session-restart-request', {
                detail: { sessionId: session.id },
              }))
              // Sidebar list refresh — daemon worker may briefly drop
              // from listing while it cycles.
              window.dispatchEvent(new CustomEvent('claude-sessions-refresh'))
            } catch (err) {
              console.error('compact failed', err)
              alert('Compact failed: ' + (err as Error).message)
            }
          },
        },
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

  const handleCreateFolder = (parentPath: string) => {
    setNewFolderModal({ visible: true, parentPath })
  }

  const handleCreateFolderConfirm = async (folderName: string) => {
    const { parentPath } = newFolderModal
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

  // Handle sessions resize (delta-based to avoid cursor jump)
  const handleSessionsResize = useCallback((startY: number, startHeight: number, currentY: number) => {
    const newHeight = startHeight + (startY - currentY)
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

  // Load live Claude sessions from the in-memory manager. We keep
  // using /api/sessions (not /api/sessions/by-project) so the IDs we
  // get back match the grimoire-side keys in manager.sessions —
  // critical because clicking a session reconnects via that exact
  // key, and the daemon UUID (from by-project) wouldn't match.
  //
  // Name accuracy is handled backend-side: handler.init no longer
  // overrides session.Name with the currently-open note's title for
  // non-note sessions, so "Quick Terminal" / daemon names stay intact.
  useEffect(() => {
    const loadSessions = async () => {
      try {
        // Sidebar shows ONLY live sessions (daemon worker alive in
        // the claude daemon). Historical transcripts on disk are
        // available via Cmd+K (SessionsModal) — keeping them out
        // of the sidebar avoids the "300 ghost rows" clutter and
        // keeps the menu focused on what the user is actively
        // working on right now. Click a historical from Cmd+K to
        // resume it; once it spawns a daemon worker it shows here.
        const all = await sessionsAPI.listByProject()
        const top = all.filter((s) => !!s.live)
        // Map SessionListItem → ClaudeSession shape so the existing
        // render code below doesn't need to branch on data source.
        const mapped: ClaudeSession[] = top.map((s) => ({
          id: s.sessionId,
          name: s.name,
          workingDir: s.cwd,
          dangerousMode: true,
          messages: [],
          isActive: !!s.live,
          lastActivity: s.lastActivity,
          createdAt: s.startedAt,
          initialized: true,
          tempo: s.live?.tempo,
          state: s.live?.state,
          detail: s.live?.detail,
          needs: s.live?.needs,
        }))
        setClaudeSessions(mapped)
      } catch (error) {
        console.error('Failed to load Claude sessions:', error)
      }
    }

    loadSessions()
    // Refresh every 3s — status badges (working / waits input / done)
    // need to feel responsive when claude finishes a task.
    const interval = window.setInterval(loadSessions, 3000)
    // External event triggers an immediate refresh — used by fork
    // and other "I just changed something, show me the result now"
    // flows so the user doesn't wait up to 3s for the next poll tick.
    const refreshHandler = () => loadSessions()
    window.addEventListener('claude-sessions-refresh', refreshHandler)
    return () => {
      clearInterval(interval)
      window.removeEventListener('claude-sessions-refresh', refreshHandler)
    }
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
          {/* Recursive folder tree — system folders (Skills/) sink to the bottom */}
          {folderTree.children && [...folderTree.children]
            .sort((a, b) => {
              const aSys = a.isSystem ? 1 : 0
              const bSys = b.isSystem ? 1 : 0
              if (aSys !== bSys) return aSys - bSys
              return 0
            })
            .map((folder) => (
            <FolderTreeNode
              key={folder.path}
              folder={folder}
              level={0}
              notes={notes || []}
              currentNote={currentNote}
              collapsedFolders={collapsedFolders}
              dragOverFolder={dragOverFolder}
              skillStates={skillStates}
              onNewSkill={() => setNewSkillVisible(true)}
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
          className="h-3 flex items-center justify-center cursor-ns-resize group"
          onMouseDown={(e) => {
            e.preventDefault()
            const startY = e.clientY
            const startHeight = sessionsHeight

            const handleMouseMove = (moveEvent: MouseEvent) => {
              handleSessionsResize(startY, startHeight, moveEvent.clientY)
            }

            const handleMouseUp = () => {
              document.removeEventListener('mousemove', handleMouseMove)
              document.removeEventListener('mouseup', handleMouseUp)
            }

            document.addEventListener('mousemove', handleMouseMove)
            document.addEventListener('mouseup', handleMouseUp)
          }}
        >
          <div className="w-8 h-px bg-white/[0.08] group-hover:bg-cyan-500/40 transition-colors rounded-full" />
        </div>
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
          <div className="flex items-center gap-1">
            <button
              onClick={e => {
                e.stopPropagation()
                // Open the Sessions Modal (Cmd+K) — same view as the
                // global hotkey, dispatched as a window event so we
                // don't have to plumb state up through HomePage.
                window.dispatchEvent(new CustomEvent('open-sessions-modal'))
              }}
              title="Open sessions (Cmd+K)"
              className="p-1 text-slate-700 hover:text-purple-400 transition-colors rounded"
            >
              <Settings2 className="w-3 h-3" />
            </button>
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
            ) : (() => {
              // Show every active session — note-bound chats, Quick
              // Terminal tabs (global-*), and any other live daemon
              // worker. User wants to see and re-attach to all open
              // work, not just note-linked stuff.
              const visibleSessions = claudeSessions
              const orderedSessions = [
                ...sessionOrder.map(id => visibleSessions.find(s => s.id === id)).filter(Boolean) as typeof visibleSessions,
                ...visibleSessions.filter(s => !sessionOrder.includes(s.id)),
              ]
              const handleDragStart = (id: string) => setDragSessionId(id)
              const handleDragOver = (e: React.DragEvent, idx: number) => {
                e.preventDefault()
                setDragOverIdx(idx)
              }
              const handleDrop = (e: React.DragEvent, targetIdx: number) => {
                e.preventDefault()
                if (!dragSessionId) return
                const ids = orderedSessions.map(s => s.id)
                const fromIdx = ids.indexOf(dragSessionId)
                if (fromIdx === -1 || fromIdx === targetIdx) { setDragSessionId(null); setDragOverIdx(null); return }
                ids.splice(fromIdx, 1)
                ids.splice(targetIdx, 0, dragSessionId)
                setSessionOrder(ids)
                localStorage.setItem('session-order', JSON.stringify(ids))
                setDragSessionId(null)
                setDragOverIdx(null)
              }
              return (
              <div className="space-y-0.5" onDragLeave={() => setDragOverIdx(null)}>
                {orderedSessions.map((session, sessionIdx) => {
                  // Names now come from listByProject which uses the
                  // daemon-side name (or JSONL ai-title) — that's the
                  // session's actual topic, not the note's. Fall back
                  // only if the name is empty or generic.
                  let sessionName: string
                  if (session.name && session.name !== 'Terminal Session' && session.name !== '(unnamed)') {
                    sessionName = session.name
                  } else if (session.id.startsWith('note-task-')) {
                    sessionName = 'Task Session'
                  } else if (session.id.startsWith('global-')) {
                    sessionName = 'Quick Terminal'
                  } else {
                    sessionName = session.id.slice(0, 16)
                  }

                  const isActive = activeSessionId === session.id

                  const isRenaming = renamingSessionId === session.id

                  return (
                    <div key={session.id}>
                      {/* Drop indicator above */}
                      {dragOverIdx === sessionIdx && dragSessionId !== session.id && (
                        <div className="h-px mx-2 mb-0.5 bg-cyan-500/60 rounded-full" />
                      )}
                      <div
                        draggable={!isRenaming}
                        onDragStart={() => handleDragStart(session.id)}
                        onDragEnd={() => { setDragSessionId(null); setDragOverIdx(null) }}
                        onDragOver={(e) => handleDragOver(e, sessionIdx)}
                        onDrop={(e) => handleDrop(e, sessionIdx)}
                        onContextMenu={(e) => handleSessionContextMenu(e, session)}
                        onClick={() => {
                          if (isRenaming) return
                          // Note-bound session → open its primary chat
                          // via the existing ChatPanel flow. Anything
                          // else (global-*, task-*, raw daemon UUIDs)
                          // → attach modal so we connect to the live
                          // PTY without spawning a new process.
                          if (session.id.startsWith('note-') && !session.id.startsWith('note-task-')) {
                            const noteId = session.id.replace('note-', '')
                            if (onOpenChatWithNote) onOpenChatWithNote(noteId)
                          } else if (onAttachToSession) {
                            onAttachToSession(session.id, sessionName)
                          }
                          // Close the mobile sidebar so the terminal
                          // gets the full viewport — matches the old
                          // note-click behaviour.
                          if (isMobile && onMobileClose) onMobileClose()
                        }}
                        className={`w-full flex items-start gap-2 px-2 py-1.5 rounded transition text-left select-none cursor-pointer ${
                          dragSessionId === session.id ? 'opacity-40' : ''
                        } ${
                          isActive
                            ? 'bg-purple-500/10 border border-purple-500/20'
                            : 'hover:bg-white/[0.03] border border-transparent'
                        }`}
                      >
                        <Terminal className={`w-3 h-3 flex-shrink-0 mt-0.5 cursor-grab active:cursor-grabbing ${
                          isActive ? 'text-purple-400' : 'text-purple-600'
                        }`} />
                        <div className="flex-1 min-w-0">
                          {isRenaming ? (
                            <input
                              autoFocus
                              value={renamingSessionValue}
                              onChange={e => setRenamingSessionValue(e.target.value)}
                              onBlur={() => handleCommitRename(session.id, renamingSessionValue)}
                              onKeyDown={e => {
                                if (e.key === 'Enter') handleCommitRename(session.id, renamingSessionValue)
                                if (e.key === 'Escape') setRenamingSessionId(null)
                                e.stopPropagation()
                              }}
                              onClick={e => e.stopPropagation()}
                              className="w-full text-xs font-mono bg-transparent border-b border-cyan-500/50 text-cyan-300 outline-none pb-px"
                            />
                          ) : (
                            <div className="flex items-center gap-1.5 min-w-0">
                              <div className={`flex-1 text-xs font-mono truncate ${isActive ? 'text-purple-300' : 'text-slate-400'}`}>
                                {sessionName}
                              </div>
                              <SessionStatusPill state={session.state} tempo={session.tempo} detail={session.detail} needs={session.needs} />
                            </div>
                          )}
                          <div className="text-[10px] font-mono text-slate-700 truncate">
                            {session.workingDir}
                          </div>
                          <div className="flex items-center gap-1.5 mt-0.5">
                            {session.createdAt && (
                              <span className="text-[9px] font-mono text-slate-800" title={`Created: ${new Date(session.createdAt).toLocaleString()}`}>
                                {'+'}{formatSessionAge(session.createdAt)}
                              </span>
                            )}
                            {session.lastActivity && (
                              <span className="text-[9px] font-mono text-slate-700" title={`Last active: ${new Date(session.lastActivity).toLocaleString()}`}>
                                {'·'} {formatSessionAge(session.lastActivity)} ago
                              </span>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
              )
            })()}
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

      <NewFolderModal
        visible={newFolderModal.visible}
        parentPath={newFolderModal.parentPath}
        onClose={() => setNewFolderModal({ visible: false, parentPath: '' })}
        onCreate={handleCreateFolderConfirm}
      />
      <NewSkillModal
        visible={newSkillVisible}
        onClose={() => setNewSkillVisible(false)}
        onCreated={() => { refreshSkills(); fetchFolders(); fetchNotes() }}
      />
    </>
  )
}

export default Sidebar
