import { useState, useEffect, useCallback, useRef } from 'react'
import {
  Plus, Search, X, ArrowLeft, ChevronRight, Layers, Folder, Settings,
  GripVertical, Trash2, Check, Pencil, Terminal, ChevronDown, ChevronUp, Pin,
} from 'lucide-react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import type { Task, TaskStatus, KanbanColumn } from '../types/task'
import { DEFAULT_COLUMNS } from '../types/task'
import { tasksAPI, columnsAPI, taskProjectFoldersAPI } from '../api/tasks'
import { sessionsAPI } from '../api/sessions'
import type { ClaudeSession } from '../types/claude'
import { TaskCard } from '../components/tasks/TaskCard'
import { StoryCard } from '../components/tasks/StoryCard'
import { TaskDetail } from '../components/tasks/TaskDetail'
import ChatPanel from '../components/chat/ChatPanel'
import { useNotes } from '../contexts/NotesContext'
import type { TaskContextPayload } from '../hooks/useTerminalWebSocket'
import { useEventsWebSocket } from '../hooks/useEventsWebSocket'
import { SessionStatusPill, formatSessionAge } from '../components/sessions/SessionStatusPill'

const LS_COL_WIDTHS = 'tasks_column_widths'
const LS_DETAIL_WIDTH = 'tasks_detail_width'
const DEFAULT_COL_WIDTH = 240
const DEFAULT_DETAIL_WIDTH = 420

const COLUMN_PRESETS = [
  { textColor: '#94a3b8', dotColor: '#64748b' },
  { textColor: '#60a5fa', dotColor: '#3b82f6' },
  { textColor: '#fbbf24', dotColor: '#f59e0b' },
  { textColor: '#4ade80', dotColor: '#22c55e' },
  { textColor: '#f87171', dotColor: '#ef4444' },
  { textColor: '#c084fc', dotColor: '#a855f7' },
  { textColor: '#f472b6', dotColor: '#ec4899' },
  { textColor: '#22d3ee', dotColor: '#06b6d4' },
]

function flattenFolders(node: any, result: string[] = []): string[] {
  if (node.path) result.push(node.path)
  for (const child of node.children || []) flattenFolders(child, result)
  return result
}

function findNode(node: any, path: string): any {
  if (node.path === path) return node
  for (const child of node.children || []) {
    const found = findNode(child, path)
    if (found) return found
  }
  return null
}

function defaultProjectFolders(folderTree: any): string[] {
  if (!folderTree) return []
  const node = findNode(folderTree, 'Projects')
  if (node) return (node.children || []).map((c: any) => c.path)
  return (folderTree.children || []).map((c: any) => c.path)
}

function slugify(label: string): string {
  return label.toLowerCase().replace(/\s+/g, '_').replace(/[^a-z0-9_]/g, '') || `col_${Date.now()}`
}

// ── Column Manager Modal ──────────────────────────────────────────────────────

interface ColumnManagerProps {
  columns: KanbanColumn[]
  onChange: (cols: KanbanColumn[]) => void
  onClose: () => void
}

function ColumnManager({ columns, onChange, onClose }: ColumnManagerProps) {
  const [cols, setCols] = useState<KanbanColumn[]>(columns)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editLabel, setEditLabel] = useState('')
  const [newLabel, setNewLabel] = useState('')
  const dragIdxRef = useRef<number | null>(null)

  const commit = (next: KanbanColumn[]) => { setCols(next); onChange(next) }

  const startEdit = (col: KanbanColumn) => { setEditingId(col.id); setEditLabel(col.label) }
  const saveEdit = (id: string) => {
    commit(cols.map(c => c.id === id ? { ...c, label: editLabel || c.label } : c))
    setEditingId(null)
  }

  const setColor = (id: string, preset: typeof COLUMN_PRESETS[0]) => {
    commit(cols.map(c => c.id === id ? { ...c, ...preset } : c))
  }

  const deleteCol = (id: string) => {
    if (cols.length <= 1) return
    commit(cols.filter(c => c.id !== id).map((c, i) => ({ ...c, order: i })))
  }

  const addCol = () => {
    if (!newLabel.trim()) return
    const id = slugify(newLabel)
    const preset = COLUMN_PRESETS[cols.length % COLUMN_PRESETS.length]
    const next = [...cols, { id, label: newLabel.trim(), ...preset, order: cols.length }]
    commit(next)
    setNewLabel('')
  }

  // Drag-to-reorder
  const handleDragStart = (i: number) => { dragIdxRef.current = i }
  const handleDragOver = (e: React.DragEvent, i: number) => {
    e.preventDefault()
    const from = dragIdxRef.current
    if (from === null || from === i) return
    const next = [...cols]
    const [item] = next.splice(from, 1)
    next.splice(i, 0, item)
    dragIdxRef.current = i
    commit(next.map((c, idx) => ({ ...c, order: idx })))
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="w-96 rounded-xl overflow-hidden shadow-2xl"
        style={{ background: '#0d0f17', border: '1px solid rgba(255,255,255,0.08)' }}
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/[0.06]">
          <span className="text-[10px] font-mono font-semibold tracking-widest text-slate-500 uppercase">Manage Columns</span>
          <button onClick={onClose} className="text-slate-600 hover:text-slate-400"><X className="w-3.5 h-3.5" /></button>
        </div>

        <div className="p-3 space-y-1 max-h-96 overflow-y-auto">
          {cols.map((col, i) => (
            <div
              key={col.id}
              draggable
              onDragStart={() => handleDragStart(i)}
              onDragOver={e => handleDragOver(e, i)}
              className="flex items-center gap-2 px-2 py-1.5 rounded-lg bg-white/[0.03] hover:bg-white/[0.05] group"
            >
              <GripVertical className="w-3 h-3 text-slate-700 cursor-grab shrink-0" />

              {/* Color dot picker */}
              <div className="flex gap-0.5 shrink-0">
                {COLUMN_PRESETS.map(p => (
                  <button
                    key={p.dotColor}
                    onClick={() => setColor(col.id, p)}
                    className="w-3 h-3 rounded-full transition-transform hover:scale-125"
                    style={{
                      background: p.dotColor,
                      outline: col.dotColor === p.dotColor ? `2px solid ${p.dotColor}` : 'none',
                      outlineOffset: '1px',
                    }}
                  />
                ))}
              </div>

              {/* Label */}
              {editingId === col.id ? (
                <input
                  value={editLabel}
                  onChange={e => setEditLabel(e.target.value)}
                  onBlur={() => saveEdit(col.id)}
                  onKeyDown={e => { if (e.key === 'Enter') saveEdit(col.id); if (e.key === 'Escape') setEditingId(null) }}
                  className="flex-1 bg-white/[0.06] border border-cyan-500/30 rounded px-1.5 py-0.5 text-xs font-mono text-slate-200 outline-none"
                  autoFocus
                />
              ) : (
                <span
                  className="flex-1 text-xs font-mono cursor-text"
                  style={{ color: col.textColor }}
                  onClick={() => startEdit(col)}
                >
                  {col.label}
                </span>
              )}

              <button
                onClick={() => startEdit(col)}
                className="opacity-0 group-hover:opacity-100 text-slate-600 hover:text-slate-400 p-0.5"
              >
                <Pencil className="w-2.5 h-2.5" />
              </button>
              <button
                onClick={() => deleteCol(col.id)}
                disabled={cols.length <= 1}
                className="opacity-0 group-hover:opacity-100 text-red-500/50 hover:text-red-400 p-0.5 disabled:opacity-0"
              >
                <Trash2 className="w-2.5 h-2.5" />
              </button>
            </div>
          ))}
        </div>

        {/* Add new column */}
        <div className="px-3 pb-3 flex gap-2">
          <input
            value={newLabel}
            onChange={e => setNewLabel(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') addCol() }}
            placeholder="New column name…"
            className="flex-1 bg-white/[0.04] border border-white/[0.08] rounded px-2 py-1 text-xs font-mono text-slate-300 outline-none focus:border-cyan-500/30 placeholder:text-slate-700"
          />
          <button
            onClick={addCol}
            disabled={!newLabel.trim()}
            className="px-2.5 py-1 text-[10px] font-mono bg-cyan-500/15 text-cyan-400 border border-cyan-500/25 rounded hover:bg-cyan-500/25 disabled:opacity-40 transition-colors"
          >
            <Check className="w-3 h-3" />
          </button>
        </div>
      </div>
    </div>
  )
}

// ── TasksPage ─────────────────────────────────────────────────────────────────

export default function TasksPage() {
  const navigate = useNavigate()
  const { taskId: taskIdParam } = useParams<{ taskId: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { folderTree, notes } = useNotes()

  const [columns, setColumns] = useState<KanbanColumn[]>(DEFAULT_COLUMNS)
  const [tasks, setTasks] = useState<Task[]>([])
  const [selectedFolder, setSelectedFolder] = useState<string | null>(() => searchParams.get('folder'))

  // Pinned tasks (stored in localStorage, per session)
  const [pinnedTaskIds, setPinnedTaskIds] = useState<Set<string>>(() => {
    try { return new Set(JSON.parse(localStorage.getItem('tasks_pinned') ?? '[]')) } catch { return new Set() }
  })
  const togglePin = (taskId: string) => {
    setPinnedTaskIds(prev => {
      const next = new Set(prev)
      next.has(taskId) ? next.delete(taskId) : next.add(taskId)
      localStorage.setItem('tasks_pinned', JSON.stringify([...next]))
      return next
    })
  }

  // Context menu
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; task: Task } | null>(null)
  useEffect(() => {
    if (!ctxMenu) return
    const close = () => setCtxMenu(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [!!ctxMenu])
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [searchResults, setSearchResults] = useState<Task[] | null>(null)
  const [creatingInColumn, setCreatingInColumn] = useState<TaskStatus | null>(null)
  const [newTaskTitle, setNewTaskTitle] = useState('')
  const [loadingTasks, setLoadingTasks] = useState(false)
  const [showColumnManager, setShowColumnManager] = useState(false)

  // Mobile responsive
  const [isMobile, setIsMobile] = useState(() => window.innerWidth < 768)
  const [showMobileSidebar, setShowMobileSidebar] = useState(false)
  useEffect(() => {
    const handler = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', handler)
    return () => window.removeEventListener('resize', handler)
  }, [])

  // Terminal chat for tasks
  const [chatTask, setChatTask] = useState<Task | null>(null)
  const [showChat, setShowChat] = useState(false)
  // For non-task sessions opened from the sidebar — overrides the noteId passed to ChatPanel
  const [chatNoteIdOverride, setChatNoteIdOverride] = useState<string | null>(null)

  // Claude sessions panel
  const [sessions, setSessions] = useState<ClaudeSession[]>([])
  const [sessionsCollapsed, setSessionsCollapsed] = useState(false)
  const [sessionsHeight, setSessionsHeight] = useState(180)
  const sidebarRef = useRef<HTMLDivElement>(null)

  // Project folder management
  const [projectFolders, setProjectFolders] = useState<string[]>([])
  const [foldersInitialized, setFoldersInitialized] = useState(false)
  const [showFolderManager, setShowFolderManager] = useState(false)
  const foldersFromDBRef = useRef(false)

  // Column widths (resizable)
  const [columnWidths, setColumnWidths] = useState<Record<string, number>>(() => {
    try { return JSON.parse(localStorage.getItem(LS_COL_WIDTHS) || '{}') } catch { return {} }
  })
  const getColWidth = (colId: string) => columnWidths[colId] ?? DEFAULT_COL_WIDTH
  const startColResize = (colId: string, startX: number) => {
    const startWidth = getColWidth(colId)
    const onMove = (e: MouseEvent) => {
      const newWidth = Math.max(160, Math.min(640, startWidth + e.clientX - startX))
      setColumnWidths(prev => {
        const next = { ...prev, [colId]: newWidth }
        localStorage.setItem(LS_COL_WIDTHS, JSON.stringify(next))
        return next
      })
    }
    const onUp = () => {
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }

  // Detail panel resize
  const [detailWidth, setDetailWidth] = useState<number>(() => {
    try { return parseInt(localStorage.getItem(LS_DETAIL_WIDTH) || String(DEFAULT_DETAIL_WIDTH)) } catch { return DEFAULT_DETAIL_WIDTH }
  })
  const startDetailResize = (startX: number) => {
    const startWidth = detailWidth
    const onMove = (e: MouseEvent) => {
      const newWidth = Math.max(300, Math.min(800, startWidth - (e.clientX - startX)))
      setDetailWidth(newWidth)
      localStorage.setItem(LS_DETAIL_WIDTH, String(newWidth))
    }
    const onUp = () => {
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
    }
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }

  // Story grouping state
  const [collapsedStories, setCollapsedStories] = useState<Set<string>>(new Set())
  const toggleStory = (id: string) => setCollapsedStories(prev => {
    const next = new Set(prev)
    next.has(id) ? next.delete(id) : next.add(id)
    return next
  })
  const [creatingUnderStory, setCreatingUnderStory] = useState<string | null>(null)

  // Drag state
  const [dragTaskId, setDragTaskId] = useState<string | null>(null)
  const [dragOverColumn, setDragOverColumn] = useState<TaskStatus | null>(null)
  const [dragOverFolder, setDragOverFolder] = useState<string | null>(null)

  const newTaskInputRef = useRef<HTMLInputElement>(null)
  const searchTimeout = useRef<number | null>(null)
  const folderManagerRef = useRef<HTMLDivElement>(null)
  // Persistent cache: taskId → title, survives folder switches
  const taskTitleCache = useRef<Record<string, string>>({})

  // Load columns from MongoDB
  useEffect(() => {
    columnsAPI.get().then(cols => {
      if (cols && cols.length > 0) setColumns(cols)
    }).catch(() => {})
  }, [])

  const saveColumns = useCallback(async (cols: KanbanColumn[]) => {
    setColumns(cols)
    try { await columnsAPI.set(cols) } catch {}
  }, [])

  // Load project folders from MongoDB on mount
  useEffect(() => {
    if (foldersFromDBRef.current) return
    foldersFromDBRef.current = true
    taskProjectFoldersAPI.get().then(folders => {
      if (folders && folders.length > 0) {
        setProjectFolders(folders)
        setFoldersInitialized(true)
      }
      // if empty, wait for folderTree to auto-detect defaults
    }).catch(() => {})
  }, [])

  // Auto-detect defaults once folderTree loads (only if DB had nothing)
  useEffect(() => {
    if (!folderTree || foldersInitialized) return
    setProjectFolders(defaultProjectFolders(folderTree))
    setFoldersInitialized(true)
  }, [folderTree, foldersInitialized])

  const saveProjectFolders = (folders: string[]) => {
    setProjectFolders(folders)
    taskProjectFoldersAPI.set(folders).catch(() => {})
  }

  const toggleFolder = (path: string) => {
    const next = projectFolders.includes(path)
      ? projectFolders.filter(p => p !== path)
      : [...projectFolders, path]
    saveProjectFolders(next)
  }

  useEffect(() => {
    if (!showFolderManager) return
    const handler = (e: MouseEvent) => {
      if (folderManagerRef.current && !folderManagerRef.current.contains(e.target as Node))
        setShowFolderManager(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showFolderManager])

  const loadTasks = useCallback(async () => {
    setLoadingTasks(true)
    try {
      const list = await tasksAPI.list(selectedFolder ? { folderPath: selectedFolder } : undefined)
      list.forEach(t => { taskTitleCache.current[t.id] = t.title })
      setTasks(list)
    } finally {
      setLoadingTasks(false)
    }
  }, [selectedFolder])

  useEffect(() => { loadTasks() }, [loadTasks])

  useEffect(() => {
    if (creatingInColumn) setTimeout(() => newTaskInputRef.current?.focus(), 50)
  }, [creatingInColumn])

  const handleSearch = (q: string) => {
    setSearchQuery(q)
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    if (!q.trim()) { setSearchResults(null); return }
    searchTimeout.current = window.setTimeout(async () => {
      setSearchResults(await tasksAPI.search(q))
    }, 300)
  }

  const createTask = async (status: TaskStatus) => {
    const title = newTaskTitle.trim()
    if (!title) { setCreatingInColumn(null); return }
    // Clear immediately — prevents double-trigger from onBlur firing after onKeyDown Enter
    setNewTaskTitle('')
    setCreatingInColumn(null)
    const parentId = creatingUnderStory
    setCreatingUnderStory(null)
    const task = await tasksAPI.create({
      title,
      status,
      linkedFolderPaths: selectedFolder ? [selectedFolder] : [],
      ...(parentId ? { parentId } : {}),
    })
    setTasks(prev => prev.find(t => t.id === task.id) ? prev : [task, ...prev])
  }

  // Drag & Drop between columns
  const handleDragStart = (e: React.DragEvent, taskId: string) => {
    setDragTaskId(taskId)
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', taskId)
  }

  const handleDrop = async (e: React.DragEvent, status: TaskStatus) => {
    e.preventDefault()
    const taskId = e.dataTransfer.getData('text/plain') || dragTaskId
    setDragOverColumn(null); setDragTaskId(null)
    if (!taskId) return
    const task = tasks.find(t => t.id === taskId)
    if (!task || task.status === status) return
    const prevStatus = task.status
    setTasks(prev => prev.map(t => t.id === taskId ? { ...t, status } : t))
    if (selectedTask?.id === taskId) setSelectedTask(prev => prev ? { ...prev, status } : null)
    try {
      const updated = await tasksAPI.update(taskId, { status })
      setTasks(prev => prev.map(t => t.id === taskId ? updated : t))
      if (selectedTask?.id === taskId) setSelectedTask(updated)
    } catch {
      // Rollback using captured prevStatus (not tasks closure which may be stale)
      setTasks(prev => prev.map(t => t.id === taskId ? { ...t, status: prevStatus } : t))
      setSelectedTask(prev => prev?.id === taskId ? { ...prev, status: prevStatus } : prev)
    }
  }

  // Drag task to folder (move to project)
  const handleDropOnFolder = async (e: React.DragEvent, folderPath: string) => {
    e.preventDefault()
    const taskId = e.dataTransfer.getData('text/plain') || dragTaskId
    setDragOverFolder(null); setDragTaskId(null)
    if (!taskId) return
    const task = tasks.find(t => t.id === taskId)
    if (!task || (task.linkedFolderPaths || []).includes(folderPath)) return
    const updated = await tasksAPI.update(taskId, { linkedFolderPaths: [folderPath] })
    if (selectedFolder && selectedFolder !== folderPath) {
      setTasks(prev => prev.filter(t => t.id !== taskId))
      if (selectedTask?.id === taskId) setSelectedTask(null)
    } else {
      setTasks(prev => prev.map(t => t.id === taskId ? updated : t))
      if (selectedTask?.id === taskId) setSelectedTask(updated)
    }
  }

  const handleDragEnd = () => { setDragTaskId(null); setDragOverColumn(null); setDragOverFolder(null) }

  const handleTaskUpdated = (updated: Task) => {
    setTasks(prev => prev.map(t => t.id === updated.id ? updated : t))
    setSelectedTask(updated)
    if (searchResults) setSearchResults(prev => prev ? prev.map(t => t.id === updated.id ? updated : t) : null)
  }

  const handleTaskDeleted = (id: string) => {
    setTasks(prev => prev.filter(t => t.id !== id))
    if (searchResults) setSearchResults(prev => prev ? prev.filter(t => t.id !== id) : null)
    setSelectedTask(null)
    const boardUrl = selectedFolder ? `/tasks?folder=${encodeURIComponent(selectedFolder)}` : '/tasks'
    navigate(boardUrl, { replace: true })
  }

  const selectTask = (task: Task | null) => {
    setSelectedTask(task)
    if (task) navigate(`/tasks/${task.id}`, { replace: true })
    else {
      const boardUrl = selectedFolder ? `/tasks?folder=${encodeURIComponent(selectedFolder)}` : '/tasks'
      navigate(boardUrl, { replace: true })
    }
  }

  // Open task from URL param after tasks load.
  // Also auto-switch to the task's folder so the board shows project context.
  useEffect(() => {
    if (!taskIdParam || tasks.length === 0) return
    const task = tasks.find(t => t.id === taskIdParam)
    if (!task) return
    if (selectedTask?.id !== taskIdParam) setSelectedTask(task)
    const taskFolder = task.linkedFolderPaths?.[0]
    if (taskFolder && selectedFolder !== taskFolder) {
      setSelectedFolder(taskFolder)
    }
  }, [taskIdParam, tasks])

  const handleOpenTerminal = (task: Task) => {
    setChatTask(task)
    setShowChat(true)
  }

  // Load sessions and poll — uses listByProject (same source as the
  // main Sidebar) and keeps only LIVE daemon-backed workers. Historical
  // transcripts on disk are intentionally excluded so the sidebar
  // stays focused on what the user is actively running; Cmd+K opens
  // SessionsModal for the full archive.
  useEffect(() => {
    const load = async () => {
      try {
        const all = await sessionsAPI.listByProject()
        const mapped: ClaudeSession[] = all
          .filter((s) => !!s.live)
          .map((s) => ({
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
        setSessions(mapped)
      } catch {}
    }
    load()
    const interval = window.setInterval(load, 3000)
    const refreshHandler = () => load()
    window.addEventListener('claude-sessions-refresh', refreshHandler)
    return () => {
      clearInterval(interval)
      window.removeEventListener('claude-sessions-refresh', refreshHandler)
    }
  }, [])

  const handleOpenSession = (session: ClaudeSession) => {
    if (session.id.startsWith('note-task-')) {
      const taskId = session.id.slice('note-task-'.length)
      setChatNoteIdOverride(null)
      const task = tasks.find(t => t.id === taskId)
      if (task) {
        setChatTask(task)
        setShowChat(true)
        return
      }
      // Task not in current view — open panel with minimal context
      setChatTask({ id: taskId, title: session.name !== 'Terminal Session' ? session.name : taskId } as Task)
      setShowChat(true)
    } else {
      // Non-task session (e.g. note session) — open without task context.
      // ChatPanel builds sessionId = "note-{noteId}", so strip the leading "note-" here.
      const noteId = session.id.startsWith('note-') ? session.id.slice('note-'.length) : session.id
      setChatNoteIdOverride(noteId)
      setChatTask({ id: noteId, title: session.name || session.id } as Task)
      setShowChat(true)
    }
  }

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await sessionsAPI.deleteSession(sessionId)
      setSessions(prev => prev.filter(s => s.id !== sessionId))
      const currentSessionId = chatNoteIdOverride ? `note-${chatNoteIdOverride}` : (chatTask ? `note-task-${chatTask.id}` : null)
      if (currentSessionId === sessionId) { setShowChat(false); setChatNoteIdOverride(null) }
    } catch {}
  }

  useEventsWebSocket({
    onTaskEvent: (event) => {
      if (event.type === 'task_created' && event.task) {
        taskTitleCache.current[event.task.id] = event.task.title
        setTasks(prev => {
          if (prev.find(t => t.id === event.task!.id)) return prev
          return [event.task!, ...prev]
        })
      } else if (event.type === 'task_updated' && event.task) {
        taskTitleCache.current[event.task.id] = event.task.title
        setTasks(prev => prev.map(t => t.id === event.task!.id ? event.task! : t))
        setSelectedTask(prev => prev?.id === event.task!.id ? event.task! : prev)
        if (searchResults) setSearchResults(prev => prev ? prev.map(t => t.id === event.task!.id ? event.task! : t) : null)
      } else if (event.type === 'task_deleted' && event.taskId) {
        setTasks(prev => prev.filter(t => t.id !== event.taskId))
        if (searchResults) setSearchResults(prev => prev ? prev.filter(t => t.id !== event.taskId) : null)
        setSelectedTask(prev => prev?.id === event.taskId ? null : prev)
      }
    },
  })

  const displayedTasks = searchResults ?? tasks
  const tasksByStatus = (status: TaskStatus) => {
    const list = displayedTasks.filter(t => t.status === status)
    return [...list.filter(t => pinnedTaskIds.has(t.id)), ...list.filter(t => !pinnedTaskIds.has(t.id))]
  }

  const handleCtxDelete = async (task: Task) => {
    setCtxMenu(null)
    try {
      await tasksAPI.delete(task.id)
      handleTaskDeleted(task.id)
    } catch (err) { console.error('Delete failed:', err) }
  }
  // Tasks that have at least one child — rendered as group cards regardless of type
  const allParentIds = new Set(tasks.filter(t => t.parentId).map(t => t.parentId!))
  const allFolders = folderTree ? flattenFolders(folderTree) : []
  const selectedFolderName = selectedFolder ? selectedFolder.split('/').pop() : null

  const chatTaskContext: TaskContextPayload | null = chatTask ? {
    id: chatTask.id,
    title: chatTask.title,
    status: chatTask.status,
    priority: chatTask.priority,
    description: chatTask.description,
    folderPath: (chatTask.linkedFolderPaths || [])[0],
  } : null

  return (
    <div className="flex h-screen overflow-hidden" style={{ background: '#07080d', height: '100dvh' }}>

      {/* Column manager modal */}
      {showColumnManager && (
        <ColumnManager
          columns={columns}
          onChange={saveColumns}
          onClose={() => setShowColumnManager(false)}
        />
      )}

      {/* Context menu */}
      {ctxMenu && (
        <div
          className="fixed z-[9999] rounded-lg overflow-hidden shadow-2xl"
          style={{
            top: Math.min(ctxMenu.y, window.innerHeight - 100),
            left: Math.min(ctxMenu.x, window.innerWidth - 180),
            minWidth: 160,
            background: '#0d0f17',
            border: '1px solid rgba(255,255,255,0.08)',
            boxShadow: '0 8px 32px rgba(0,0,0,0.7)',
          }}
          onClick={e => e.stopPropagation()}
        >
          <button
            onClick={() => { togglePin(ctxMenu.task.id); setCtxMenu(null) }}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs font-mono text-slate-300 hover:bg-white/[0.05] transition-colors text-left"
          >
            <Pin className="w-3.5 h-3.5 text-amber-400/70" />
            {pinnedTaskIds.has(ctxMenu.task.id) ? 'Unpin' : 'Pin to top'}
          </button>
          <div className="h-px mx-2" style={{ background: 'rgba(255,255,255,0.06)' }} />
          <button
            onClick={() => handleCtxDelete(ctxMenu.task)}
            className="w-full flex items-center gap-2.5 px-3 py-2 text-xs font-mono text-red-400 hover:bg-red-500/10 transition-colors text-left"
          >
            <Trash2 className="w-3.5 h-3.5" />
            Delete
          </button>
        </div>
      )}

      {/* Task terminal */}
      {chatTask && (
        <ChatPanel
          key={chatNoteIdOverride ?? chatTask.id}
          visible={showChat}
          onClose={() => { setShowChat(false); setChatNoteIdOverride(null) }}
          noteId={chatNoteIdOverride ?? `task-${chatTask.id}`}
          taskContext={chatNoteIdOverride ? null : chatTaskContext}
        />
      )}

      {/* Mobile sidebar overlay backdrop */}
      {isMobile && showMobileSidebar && (
        <div
          className="fixed inset-0 z-40 bg-black/60"
          onClick={() => setShowMobileSidebar(false)}
        />
      )}

      {/* Projects sidebar */}
      <div
        ref={sidebarRef}
        className={`flex-col border-r flex-shrink-0 ${isMobile ? 'fixed left-0 top-0 bottom-0 z-50 transition-transform duration-200' : 'flex'}`}
        style={{
          width: 208,
          background: '#090b11',
          borderColor: 'rgba(255,255,255,0.06)',
          transform: isMobile ? (showMobileSidebar ? 'translateX(0)' : 'translateX(-100%)') : undefined,
        }}
      >

        {/* Back */}
        <div className="px-3 py-3 border-b" style={{ borderColor: 'rgba(255,255,255,0.06)' }}>
          <button onClick={() => navigate('/')} className="flex items-center gap-1.5 text-[11px] font-mono text-slate-600 hover:text-slate-400 transition-colors">
            <ArrowLeft className="w-3 h-3" />
            <span style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              <span style={{ color: '#06b6d4' }}>md</span>
              <span style={{ color: 'rgba(100,116,139,0.4)' }}>/editor</span>
            </span>
          </button>
        </div>

        {/* Projects header */}
        <div className="px-3 py-2.5 flex items-center justify-between">
          <span className="text-[9px] font-mono font-semibold tracking-widest text-slate-700 uppercase">Projects</span>
          <div className="relative" ref={folderManagerRef}>
            <button onClick={() => setShowFolderManager(!showFolderManager)} className="text-slate-700 hover:text-cyan-500 transition-colors" title="Manage project folders">
              <Settings className="w-3 h-3" />
            </button>
            {showFolderManager && (
              <div className="absolute left-0 top-full mt-1 w-56 rounded-lg z-50 overflow-hidden" style={{ background: '#0d0f17', border: '1px solid rgba(255,255,255,0.08)', boxShadow: '0 8px 32px rgba(0,0,0,0.6)' }}>
                <div className="px-3 py-2 border-b border-white/[0.05]">
                  <span className="text-[9px] font-mono font-semibold tracking-widest text-slate-700 uppercase">Folders as projects</span>
                </div>
                <div className="max-h-64 overflow-y-auto py-1">
                  {allFolders.map(f => {
                    const depth = f.split('/').length - 1
                    return (
                      <label key={f} className="flex items-center gap-2 cursor-pointer hover:bg-white/[0.04]" style={{ paddingLeft: `${12 + depth * 10}px`, paddingRight: 12, paddingTop: 5, paddingBottom: 5 }}>
                        <input type="checkbox" checked={projectFolders.includes(f)} onChange={() => toggleFolder(f)} className="accent-cyan-500 w-3 h-3 shrink-0" />
                        <Folder className="w-3 h-3 text-slate-700 shrink-0" />
                        <span className={`text-[11px] font-mono truncate ${projectFolders.includes(f) ? 'text-slate-300' : 'text-slate-600'}`}>{f.split('/').pop()}</span>
                      </label>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        </div>

        {/* All tasks */}
        <div className="px-2 mb-1">
          <button
            onClick={() => { setSelectedFolder(null); setSearchParams({}, { replace: true }) }}
            className={`w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs font-mono transition-colors ${selectedFolder === null ? 'bg-white/[0.06] text-slate-200' : 'text-slate-500 hover:text-slate-300 hover:bg-white/[0.03]'}`}
          >
            <Layers className="w-3 h-3 text-slate-600 shrink-0" />
            <span className="truncate">All tasks</span>
            <span className="ml-auto text-[10px] text-slate-700">{tasks.length}</span>
          </button>
        </div>

        {/* Project folder list */}
        <div className="flex-1 overflow-y-auto px-2 space-y-0.5 min-h-0">
          {projectFolders.map(path => {
            const isDropTarget = dragOverFolder === path
            return (
              <button
                key={path}
                onClick={() => { setSelectedFolder(path); setSearchParams({ folder: path }, { replace: true }) }}
                onDragOver={e => { e.preventDefault(); setDragOverFolder(path) }}
                onDragLeave={() => setDragOverFolder(null)}
                onDrop={e => handleDropOnFolder(e, path)}
                className={`group w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs font-mono transition-all ${
                  isDropTarget ? 'bg-cyan-500/15 text-cyan-300'
                  : selectedFolder === path ? 'bg-white/[0.06] text-slate-200'
                  : 'text-slate-500 hover:text-slate-300 hover:bg-white/[0.03]'
                }`}
                style={{ border: isDropTarget ? '1px solid rgba(6,182,212,0.4)' : '1px solid transparent' }}
                title={path}
              >
                <Folder className={`w-3 h-3 shrink-0 ${isDropTarget ? 'text-cyan-400' : 'text-slate-600'}`} />
                <span className="truncate flex-1 text-left">{path.split('/').pop()}</span>
                <ChevronRight className={`w-3 h-3 shrink-0 transition-opacity ${selectedFolder === path ? 'opacity-40' : 'opacity-0 group-hover:opacity-30'}`} />
              </button>
            )
          })}
        </div>
        {/* Claude Sessions */}
        <div className="flex-shrink-0">
          {/* Resize handle */}
          {!sessionsCollapsed && (
            <div
              className="h-px hover:bg-cyan-500/40 cursor-ns-resize transition-colors"
              style={{ background: 'rgba(255,255,255,0.06)' }}
              onMouseDown={e => {
                e.preventDefault()
                const onMove = (mv: MouseEvent) => {
                  if (!sidebarRef.current) return
                  const bottom = sidebarRef.current.getBoundingClientRect().bottom
                  const newH = Math.max(80, Math.min(500, bottom - mv.clientY - 8))
                  setSessionsHeight(newH)
                }
                const onUp = () => {
                  document.removeEventListener('mousemove', onMove)
                  document.removeEventListener('mouseup', onUp)
                }
                document.addEventListener('mousemove', onMove)
                document.addEventListener('mouseup', onUp)
              }}
            />
          )}
          <div
            className="flex items-center justify-between px-3 py-2 cursor-pointer hover:bg-white/[0.02] transition border-t"
            style={{ borderColor: 'rgba(255,255,255,0.06)' }}
            onClick={() => setSessionsCollapsed(v => !v)}
          >
            <div className="flex items-center gap-2">
              <Terminal className="w-3 h-3 text-cyan-600" />
              <span className="text-[9px] font-mono font-semibold tracking-widest text-slate-700 uppercase">Sessions</span>
              {sessions.length > 0 && (
                <span className="text-[9px] font-mono bg-cyan-500/10 text-cyan-500 px-1 py-0.5 rounded border border-cyan-500/20">
                  {sessions.length}
                </span>
              )}
            </div>
            {sessionsCollapsed
              ? <ChevronDown className="w-3 h-3 text-slate-700" />
              : <ChevronUp className="w-3 h-3 text-slate-700" />}
          </div>

          {!sessionsCollapsed && (
            <div className="px-2 pb-2 overflow-y-auto" style={{ height: sessionsHeight }}>
              {sessions.length === 0 ? (
                <div className="text-[10px] font-mono text-slate-800 text-center py-3 tracking-wider uppercase">no sessions</div>
              ) : (
                <div className="space-y-0.5">
                  {sessions.map(session => {
                    const currentSessionId = chatNoteIdOverride ? `note-${chatNoteIdOverride}` : (chatTask ? `note-task-${chatTask.id}` : null)
                    const isActive = !!currentSessionId && currentSessionId === session.id && showChat
                    const taskId = session.id.startsWith('note-task-') ? session.id.slice('note-task-'.length) : null
                    const linkedTask = taskId ? tasks.find(t => t.id === taskId) : null
                    const cachedTitle = taskId ? taskTitleCache.current[taskId] : undefined
                    const noteId = !taskId && session.id.startsWith('note-') ? session.id.slice('note-'.length) : null
                    const linkedNote = noteId ? (notes || []).find(n => n.id === noteId) : null
                    const label = linkedTask?.title
                      ?? cachedTitle
                      ?? linkedNote?.title
                      ?? (session.name && session.name !== 'Terminal Session' ? session.name : null)
                      ?? (taskId ? `task:${taskId.slice(0, 8)}` : session.id.slice(0, 12))
                    return (
                      <div
                        key={session.id}
                        className={`w-full flex items-start gap-2 px-2 py-1.5 rounded transition text-left select-none cursor-pointer ${
                          isActive
                            ? 'bg-purple-500/10 border border-purple-500/20'
                            : 'hover:bg-white/[0.03] border border-transparent'
                        }`}
                        onClick={() => handleOpenSession(session)}
                      >
                        <Terminal className={`w-3 h-3 flex-shrink-0 mt-0.5 ${isActive ? 'text-purple-400' : 'text-purple-600'}`} />
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-1.5 min-w-0">
                            <div className={`flex-1 text-xs font-mono truncate ${isActive ? 'text-purple-300' : 'text-slate-400'}`}>
                              {label}
                            </div>
                            <SessionStatusPill state={session.state} tempo={session.tempo} detail={session.detail} needs={session.needs} />
                          </div>
                          {session.workingDir && (
                            <div className="text-[10px] font-mono text-slate-700 truncate">{session.workingDir}</div>
                          )}
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
                    )
                  })}
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Main area */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Top bar */}
        <div className="h-14 flex items-center gap-3 px-4 flex-shrink-0 border-b" style={{ borderColor: 'rgba(255,255,255,0.06)', background: '#09090f' }}>
          {/* Mobile hamburger / back */}
          {isMobile && (
            <button
              onClick={() => setShowMobileSidebar(v => !v)}
              className="flex flex-col gap-1 p-1.5 text-slate-600 hover:text-slate-400 transition-colors flex-shrink-0"
            >
              <div className="w-4 h-px bg-current" />
              <div className="w-4 h-px bg-current" />
              <div className="w-4 h-px bg-current" />
            </button>
          )}
          <div className="flex items-center gap-2 mr-2 min-w-0">
            {selectedFolder && <Folder className="w-3.5 h-3.5 text-slate-600 flex-shrink-0" />}
            <span className="text-sm font-mono font-semibold text-slate-300 truncate" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              {selectedFolderName ?? 'All Tasks'}
            </span>
            {loadingTasks && <span className="text-[10px] font-mono text-slate-700 animate-pulse flex-shrink-0">loading…</span>}
          </div>

          <div className="flex-1 max-w-sm relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3 h-3 text-slate-700" />
            <input
              value={searchQuery}
              onChange={e => handleSearch(e.target.value)}
              placeholder="Search tasks & comments…"
              className="w-full h-8 bg-white/[0.04] border border-white/[0.06] rounded-lg pl-8 pr-8 text-xs font-mono text-slate-300 outline-none focus:border-cyan-500/30 placeholder:text-slate-700"
            />
            {searchQuery && (
              <button onClick={() => { setSearchQuery(''); setSearchResults(null) }} className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-600 hover:text-slate-400">
                <X className="w-3 h-3" />
              </button>
            )}
          </div>

          {searchResults && <span className="text-[10px] font-mono text-slate-600">{searchResults.length} result{searchResults.length !== 1 ? 's' : ''}</span>}

          {/* Manage columns button */}
          <button
            onClick={() => setShowColumnManager(true)}
            className="ml-auto flex items-center gap-1.5 px-2.5 py-1 text-[11px] font-mono text-slate-600 hover:text-slate-300 hover:bg-white/[0.04] rounded-lg transition-colors"
            title="Manage columns"
          >
            <Settings className="w-3 h-3" />
            <span className="hidden md:inline">Columns</span>
          </button>
        </div>

        {/* Kanban + Detail */}
        <div className="flex-1 flex overflow-hidden">
          <div className="flex-1 overflow-x-auto overflow-y-hidden">
            <div className="flex gap-3 p-5 h-full">
              {columns.sort((a, b) => a.order - b.order).map(col => {
                const columnTasks = tasksByStatus(col.id)
                const isDragTarget = dragOverColumn === col.id
                const colWidth = getColWidth(col.id)

                return (
                  <div
                    key={col.id}
                    className="flex flex-col flex-shrink-0 relative"
                    style={{ width: colWidth }}
                    onDragOver={e => { e.preventDefault(); setDragOverColumn(col.id) }}
                    onDragLeave={() => setDragOverColumn(null)}
                    onDrop={e => handleDrop(e, col.id)}
                  >
                    <div className="flex items-center gap-2 mb-3 px-1">
                      <span className="w-1.5 h-1.5 rounded-full flex-shrink-0" style={{ background: col.dotColor }} />
                      <span className="text-[11px] font-mono font-semibold uppercase tracking-wider truncate" style={{ color: col.textColor }}>
                        {col.label}
                      </span>
                      <span className="text-[10px] font-mono text-slate-700 ml-auto flex-shrink-0">{columnTasks.length}</span>
                    </div>

                    <div
                      className="flex-1 overflow-y-auto overflow-x-hidden rounded-xl p-2 space-y-2 transition-colors duration-150"
                      style={{
                        background: isDragTarget ? 'rgba(6,182,212,0.05)' : 'rgba(255,255,255,0.015)',
                        border: isDragTarget ? '1px solid rgba(6,182,212,0.25)' : '1px solid rgba(255,255,255,0.04)',
                      }}
                    >
                      {creatingInColumn === col.id && (
                        <div className="bg-[#0f1117] border border-cyan-500/30 rounded-lg p-2.5">
                          <input
                            ref={newTaskInputRef}
                            value={newTaskTitle}
                            onChange={e => setNewTaskTitle(e.target.value)}
                            onKeyDown={e => {
                              if (e.key === 'Enter') { e.preventDefault(); createTask(col.id) }
                              if (e.key === 'Escape') { setCreatingInColumn(null); setNewTaskTitle('') }
                            }}
                            onBlur={() => {
                              if (newTaskTitle.trim()) createTask(col.id)
                              else setCreatingInColumn(null)
                            }}
                            placeholder="Task title…"
                            className="w-full bg-transparent text-xs font-mono text-slate-200 outline-none placeholder:text-slate-700"
                          />
                          <div className="flex items-center justify-between mt-2">
                            <div className="flex gap-1">
                              <button
                                onMouseDown={e => e.preventDefault()}
                                onClick={() => createTask(col.id)}
                                className="flex items-center gap-1 px-2 py-0.5 text-[10px] font-mono bg-cyan-500/15 text-cyan-400 border border-cyan-500/25 rounded hover:bg-cyan-500/25 transition-colors"
                              >
                                <Check className="w-2.5 h-2.5" />Add
                              </button>
                              <button
                                onMouseDown={e => e.preventDefault()}
                                onClick={() => { setCreatingInColumn(null); setNewTaskTitle('') }}
                                className="px-2 py-0.5 text-[10px] font-mono text-slate-600 hover:text-slate-400 transition-colors"
                              >
                                Cancel
                              </button>
                            </div>
                            <span className="text-[9px] font-mono text-slate-800">↵ Enter</span>
                          </div>
                        </div>
                      )}

                      {(() => {
                        // Group cards = explicit stories OR any task that has children
                        const groupCards = columnTasks.filter(t =>
                          t.type === 'story' || allParentIds.has(t.id)
                        )
                        const groupIdSet = new Set(groupCards.map(g => g.id))
                        // Orphans = tasks that are not group cards and whose parent is not a group card in this column
                        const orphans = columnTasks.filter(t =>
                          !groupIdSet.has(t.id) && (!t.parentId || !groupIdSet.has(t.parentId))
                        )
                        // Actually: render stories first, then orphan tasks
                        return (
                          <>
                            {groupCards.map(story => {
                              const isStoryType = story.type === 'story'
                              const allChildren = displayedTasks.filter(t => t.parentId === story.id)
                              const colChildren = columnTasks.filter(t => t.parentId === story.id)
                              const isCreatingUnder = creatingUnderStory === story.id
                              const isStoryPinned = pinnedTaskIds.has(story.id)
                              return (
                                <div
                                  key={story.id}
                                  draggable
                                  onDragStart={e => handleDragStart(e, story.id)}
                                  onDragEnd={handleDragEnd}
                                  onContextMenu={e => { e.preventDefault(); e.stopPropagation(); setCtxMenu({ x: e.clientX, y: e.clientY, task: story }) }}
                                  className="relative"
                                  style={isStoryPinned ? { borderTop: '2px solid rgba(251,191,36,0.45)', borderRadius: 8 } : undefined}
                                >
                                  {isStoryPinned && <Pin className="absolute top-2 right-2 z-10 w-3 h-3 text-amber-400/50 pointer-events-none" />}
                                  <StoryCard
                                    story={story}
                                    allChildren={allChildren}
                                    columnChildren={colChildren}
                                    collapsed={collapsedStories.has(story.id)}
                                    onToggle={() => toggleStory(story.id)}
                                    onClick={() => selectTask(story)}
                                    onChildClick={child => selectTask(child)}
                                    onAddChild={() => {
                                      setCreatingUnderStory(story.id)
                                      setCreatingInColumn(col.id)
                                      setNewTaskTitle('')
                                    }}
                                    isDragging={dragTaskId === story.id}
                                    draggable={dragTaskId !== story.id}
                                    onDragStart={e => handleDragStart(e, story.id)}
                                    onDragEnd={handleDragEnd}
                                    isStory={isStoryType}
                                    draggingTaskId={dragTaskId}
                                    onChildDragStart={(e, id) => handleDragStart(e, id)}
                                    onChildDragEnd={handleDragEnd}
                                    creatingChild={isCreatingUnder}
                                    childInput={isCreatingUnder ? (
                                      <div className={`bg-[#0f1117] border rounded-lg p-2 ${isStoryType ? 'border-amber-500/20' : 'border-white/[0.08]'}`}>
                                        <input
                                          ref={newTaskInputRef}
                                          value={newTaskTitle}
                                          onChange={e => setNewTaskTitle(e.target.value)}
                                          onKeyDown={e => {
                                            if (e.key === 'Enter') { e.preventDefault(); createTask(col.id) }
                                            if (e.key === 'Escape') { setCreatingInColumn(null); setCreatingUnderStory(null); setNewTaskTitle('') }
                                          }}
                                          onBlur={() => {
                                            if (newTaskTitle.trim()) createTask(col.id)
                                            else { setCreatingInColumn(null); setCreatingUnderStory(null) }
                                          }}
                                          placeholder="Subtask title…"
                                          className="w-full bg-transparent text-xs font-mono text-slate-200 outline-none placeholder:text-slate-700"
                                          autoFocus
                                        />
                                      </div>
                                    ) : undefined}
                                  />
                                </div>
                              )
                            })}
                            {orphans.map(task => {
                              const parentLabel = task.parentId
                                ? tasks.find(t => t.id === task.parentId)?.title
                                : undefined
                              return (
                                <div
                                  key={task.id}
                                  draggable
                                  onDragStart={e => handleDragStart(e, task.id)}
                                  onDragEnd={handleDragEnd}
                                  onContextMenu={e => { e.preventDefault(); e.stopPropagation(); setCtxMenu({ x: e.clientX, y: e.clientY, task }) }}
                                  className={`transition-opacity duration-150 ${dragTaskId === task.id ? 'opacity-40' : 'opacity-100'}`}
                                >
                                  <TaskCard
                                    task={task}
                                    onClick={() => selectTask(task)}
                                    parentLabel={parentLabel}
                                    pinned={pinnedTaskIds.has(task.id)}
                                  />
                                </div>
                              )
                            })}
                          </>
                        )
                      })()}

                      {columnTasks.length === 0 && creatingInColumn !== col.id && (
                        <div className="flex items-center justify-center h-16">
                          <span className="text-[10px] font-mono text-slate-800">{isDragTarget ? 'drop here' : 'empty'}</span>
                        </div>
                      )}
                    </div>

                    {creatingInColumn !== col.id && (
                      <button
                        onClick={() => { setCreatingInColumn(col.id); setNewTaskTitle('') }}
                        className="mt-2 flex items-center gap-1.5 px-2 py-1.5 text-[11px] font-mono text-slate-700 hover:text-slate-400 hover:bg-white/[0.03] rounded-lg transition-colors"
                      >
                        <Plus className="w-3 h-3" />Add task
                      </button>
                    )}

                    {/* Column resize handle */}
                    <div
                      className="absolute top-0 right-0 bottom-0 w-3 flex items-stretch justify-center group cursor-col-resize z-10"
                      onMouseDown={e => { e.preventDefault(); startColResize(col.id, e.clientX) }}
                    >
                      <div className="w-px bg-white/[0.04] group-hover:bg-cyan-500/40 transition-colors" />
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          {selectedTask && (
            isMobile ? (
              /* Mobile: full-screen overlay */
              <div className="fixed inset-0 z-30 flex flex-col" style={{ background: '#07080d' }}>
                <TaskDetail
                  task={selectedTask}
                  allTasks={tasks}
                  columns={columns}
                  onClose={() => selectTask(null)}
                  onUpdated={handleTaskUpdated}
                  onDeleted={handleTaskDeleted}
                  onOpenTerminal={handleOpenTerminal}
                  onOpenNote={noteId => navigate(`/notes/${noteId}`)}
                  onOpenTask={selectTask}
                />
              </div>
            ) : (
              /* Desktop: resizable side panel */
              <div
                className="flex-shrink-0 border-l overflow-hidden flex"
                style={{ width: detailWidth, borderColor: 'rgba(255,255,255,0.06)' }}
              >
                {/* Left-edge drag handle */}
                <div
                  className="w-1 flex-shrink-0 cursor-col-resize group relative"
                  onMouseDown={e => { e.preventDefault(); startDetailResize(e.clientX) }}
                >
                  <div className="absolute inset-0 group-hover:bg-cyan-500/30 transition-colors" />
                </div>
                <div className="flex-1 min-w-0 overflow-hidden">
                  <TaskDetail
                    task={selectedTask}
                    allTasks={tasks}
                    columns={columns}
                    onClose={() => selectTask(null)}
                    onUpdated={handleTaskUpdated}
                    onDeleted={handleTaskDeleted}
                    onOpenTerminal={handleOpenTerminal}
                    onOpenNote={noteId => navigate(`/notes/${noteId}`)}
                    onOpenTask={selectTask}
                  />
                </div>
              </div>
            )
          )}
        </div>
      </div>
    </div>
  )
}
