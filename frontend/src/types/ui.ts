export type ViewMode = 'editor' | 'split' | 'preview'

export interface UIState {
  viewMode: ViewMode
  sidebarVisible: boolean
  chatPanelVisible: boolean
  graphViewVisible: boolean
  sidebarWidth: number
  editorWidth: number
  previewWidth: number
}

export interface ModalState {
  newNote: boolean
  upload: boolean
  search: boolean
  mermaidEditor: boolean
  askClaude: boolean
  deleteConfirm: boolean
}

export interface ContextMenuState {
  visible: boolean
  x: number
  y: number
  target: 'note' | 'folder' | 'editor' | null
  targetId?: string
}
