export type TaskStatus = string  // dynamic — stored in MongoDB
export type TaskPriority = 'low' | 'medium' | 'high' | 'urgent'

export interface KanbanColumn {
  id: string
  label: string
  textColor: string  // CSS hex, e.g. '#94a3b8'
  dotColor: string   // CSS hex
  order: number
}

export const DEFAULT_COLUMNS: KanbanColumn[] = [
  { id: 'backlog',     label: 'Backlog',     textColor: '#94a3b8', dotColor: '#64748b', order: 0 },
  { id: 'todo',        label: 'To Do',       textColor: '#60a5fa', dotColor: '#3b82f6', order: 1 },
  { id: 'in_progress', label: 'In Progress', textColor: '#fbbf24', dotColor: '#f59e0b', order: 2 },
  { id: 'done',        label: 'Done',        textColor: '#4ade80', dotColor: '#22c55e', order: 3 },
]

export interface TaskComment {
  id: string
  content: string
  createdAt: string
  updatedAt: string
}

export interface Project {
  id: string
  title: string
  description?: string
  color?: string
  linkedFolderPath?: string
  createdAt: string
  updatedAt: string
}

export type TaskType = 'task' | 'story'

export interface RecurringConfig {
  enabled: boolean
  intervalMinutes: number
  lastRunAt?: string
  nextRunAt?: string
}

export interface Task {
  id: string
  type?: TaskType
  parentId?: string
  title: string
  description?: string
  projectId?: string
  status: TaskStatus
  priority: TaskPriority
  linkedNoteIds?: string[]
  linkedFolderPaths?: string[]
  linkedTaskIds?: string[]
  tags?: string[]
  comments?: TaskComment[]
  dueDate?: string
  recurring?: RecurringConfig
  createdAt: string
  updatedAt: string
}

export const TASK_STATUSES: { value: string; label: string }[] = [
  { value: 'backlog', label: 'Backlog' },
  { value: 'todo', label: 'To Do' },
  { value: 'in_progress', label: 'In Progress' },
  { value: 'done', label: 'Done' },
]

export const PRIORITY_CONFIG: Record<TaskPriority, { label: string; color: string }> = {
  low:    { label: 'Low',    color: 'text-slate-400' },
  medium: { label: 'Med',    color: 'text-blue-400' },
  high:   { label: 'High',   color: 'text-amber-400' },
  urgent: { label: 'Urgent', color: 'text-red-400' },
}
