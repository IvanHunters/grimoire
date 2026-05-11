// Folder from backend (flat structure)
export interface Folder {
  path: string
  projectPath?: string
  createdAt: string
}

// FolderNode for tree structure (from backend /api/folders response)
export interface FolderNode {
  name: string
  path: string
  projectPath?: string
  children?: FolderNode[]
  // UI state (not from backend)
  isCollapsed?: boolean
}

export interface FolderTree {
  root: FolderNode
}

export interface CreateFolderRequest {
  path: string
}

export interface MoveFolderRequest {
  from: string
  to: string
}
