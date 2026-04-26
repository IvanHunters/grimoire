export interface Folder {
  path: string
  name: string
  parent?: string
  created_at: string
  children?: Folder[]
  notes?: string[] // Note IDs in this folder
  isCollapsed?: boolean // UI state
}

export interface FolderTree {
  root: Folder[]
}

export interface CreateFolderRequest {
  path: string
}

export interface MoveFolderRequest {
  from: string
  to: string
}
