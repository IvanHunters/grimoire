export interface SearchOptions {
  regex: boolean
  caseSensitive: boolean
  wholeWord: boolean
}

export interface SearchResult {
  noteId: string
  noteTitle: string
  notePath: string
  matches: SearchMatch[]
}

export interface SearchMatch {
  line: number
  column: number
  text: string
  context: string // Surrounding text
}

export interface SearchState {
  query: string
  replaceQuery: string
  options: SearchOptions
  results: SearchResult[]
  currentMatchIndex: number
  totalMatches: number
  isSearching: boolean
}
