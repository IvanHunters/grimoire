export interface Note {
  id: string
  title: string
  path: string
  folder: string
  content: string
  type?: 'project' | 'regular'
  projectPath?: string // Changed to camelCase to match backend JSON
  tags?: string[]
  createdAt: string // Changed to camelCase
  updatedAt: string // Changed to camelCase
  outgoingLinks?: string[] // Changed to camelCase
  backlinks?: string[]
}

export interface Frontmatter {
  id?: string
  title?: string
  type?: 'project' | 'regular'
  project_path?: string
  created_at?: string
  updated_at?: string
  tags?: string[]
  [key: string]: unknown
}

export interface CreateNoteRequest {
  title: string
  folder?: string
  content?: string
  type?: 'project' | 'regular'
  project_path?: string
}

export interface UpdateNoteRequest {
  title?: string
  content?: string
  type?: 'project' | 'regular'
  projectPath?: string
  tags?: string[]
}
