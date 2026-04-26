export interface GraphNode {
  id: string
  title: string
  path: string
  folder: string
  x?: number // Position for rendering
  y?: number
  isCurrent?: boolean
}

export interface GraphEdge {
  source: string // Note ID
  target: string // Note ID
  type?: 'wikilink' | 'mention' | 'tag' | 'semantic'
  strength?: number // 0-1
}

export interface GraphData {
  nodes: GraphNode[]
  links: GraphEdge[]
}

export interface GraphViewState {
  visible: boolean
  currentNoteId: string | null
  selectedNodeId: string | null
  zoom: number
}
