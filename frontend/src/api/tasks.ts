import type { Task, Project, TaskStatus, TaskPriority, KanbanColumn } from '../types/task'

const BASE = '/api'

// Kanban Columns
export const columnsAPI = {
  get: (): Promise<KanbanColumn[]> =>
    fetch(`${BASE}/task-columns`).then(r => r.json()),

  set: (cols: KanbanColumn[]): Promise<KanbanColumn[]> =>
    fetch(`${BASE}/task-columns`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cols),
    }).then(r => r.json()),
}

// Task Project Folders (persisted in MongoDB)
export const taskProjectFoldersAPI = {
  get: (): Promise<string[]> =>
    fetch(`${BASE}/task-project-folders`).then(r => r.json()),

  set: (folders: string[]): Promise<string[]> =>
    fetch(`${BASE}/task-project-folders`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(folders),
    }).then(r => r.json()),
}

// Projects
export const projectsAPI = {
  list: (): Promise<Project[]> =>
    fetch(`${BASE}/projects`).then(r => r.json()),

  create: (data: { title: string; description?: string; color?: string; linkedFolderPath?: string }): Promise<Project> =>
    fetch(`${BASE}/projects`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }).then(r => r.json()),

  update: (id: string, data: Partial<Project>): Promise<Project> =>
    fetch(`${BASE}/projects/${id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }).then(r => r.json()),

  delete: (id: string): Promise<void> =>
    fetch(`${BASE}/projects/${id}`, { method: 'DELETE' }).then(() => {}),
}

// Tasks
export const tasksAPI = {
  list: (params?: { projectId?: string; status?: TaskStatus; folderPath?: string }): Promise<Task[]> => {
    const q = new URLSearchParams()
    if (params?.projectId) q.set('projectId', params.projectId)
    if (params?.status) q.set('status', params.status)
    if (params?.folderPath) q.set('folderPath', params.folderPath)
    return fetch(`${BASE}/tasks?${q}`).then(r => r.json())
  },

  get: (id: string): Promise<Task> =>
    fetch(`${BASE}/tasks/${id}`).then(r => r.json()),

  search: (q: string, projectId?: string): Promise<Task[]> => {
    const params = new URLSearchParams({ q })
    if (projectId) params.set('projectId', projectId)
    return fetch(`${BASE}/tasks/search?${params}`).then(r => r.json())
  },

  create: (data: {
    type?: string; parentId?: string
    title: string; description?: string; projectId?: string
    status?: TaskStatus; priority?: TaskPriority
    linkedNoteIds?: string[]; linkedFolderPaths?: string[]; linkedTaskIds?: string[]
    tags?: string[]; dueDate?: string
  }): Promise<Task> =>
    fetch(`${BASE}/tasks`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }).then(r => r.json()),

  update: (id: string, data: {
    type?: string; parentId?: string; clearParentId?: boolean
    title?: string; description?: string; projectId?: string | null
    status?: TaskStatus; priority?: TaskPriority
    linkedNoteIds?: string[]; linkedFolderPaths?: string[]; linkedTaskIds?: string[]
    tags?: string[]; dueDate?: string; clearDueDate?: boolean
    recurring?: { enabled: boolean; intervalMinutes: number; cronExpr?: string }
    clearRecurring?: boolean
    setCronExpr?: string
  }): Promise<Task> =>
    fetch(`${BASE}/tasks/${id}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }).then(r => r.json()),

  delete: (id: string): Promise<void> =>
    fetch(`${BASE}/tasks/${id}`, { method: 'DELETE' }).then(() => {}),

  runNow: (id: string): Promise<void> =>
    fetch(`${BASE}/tasks/${id}/run-now`, { method: 'POST' }).then(() => {}),

  cancel: (id: string): Promise<void> =>
    fetch(`${BASE}/tasks/${id}/cancel`, { method: 'POST' }).then(() => {}),

  addComment: (id: string, content: string) =>
    fetch(`${BASE}/tasks/${id}/comments`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content }) }).then(r => r.json()),

  updateComment: (id: string, commentId: string, content: string) =>
    fetch(`${BASE}/tasks/${id}/comments/${commentId}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content }) }).then(r => r.json()),

  deleteComment: (id: string, commentId: string): Promise<void> =>
    fetch(`${BASE}/tasks/${id}/comments/${commentId}`, { method: 'DELETE' }).then(() => {}),
}
