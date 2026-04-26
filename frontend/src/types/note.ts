export interface Note {
  id: string
  title: string
  path: string
  folder: string
  content: string
  type?: 'project' | 'regular'
  project_path?: string
  created_at: string
  updated_at: string
  tags?: string[]
  outgoing_links?: string[]
  backlinks?: string[]
  metadata?: Record<string, unknown>
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
  content: string
}
