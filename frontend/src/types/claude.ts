export type MessageRole = 'user' | 'assistant' | 'system'

export interface Message {
  id: string
  role: MessageRole
  content: string
  toolUse?: string
  timestamp: Date
}

export interface ClaudeSession {
  id: string
  name: string
  workingDir: string
  dangerousMode: boolean
  messages: Message[]
  isActive: boolean
  lastActivity: string
  createdAt: string
  initialized: boolean // Whether backend session is initialized
  /** Live daemon state, set by ListActiveSessions for sidebar status badge. */
  tempo?: 'idle' | 'active' | 'blocked' | 'unknown' | string
  state?: 'working' | 'blocked' | 'done' | 'failed' | 'stopped' | 'running' | 'unknown' | string
  detail?: string
  needs?: string
}

export type WSMessageType =
  | 'init'
  | 'message'
  | 'stop'
  | 'switch_session'
  | 'restart_session'
  | 'close_session'
  | 'session_started'
  | 'message_start'
  | 'content_delta'
  | 'tool_use'
  | 'message_complete'
  | 'error'
  | 'stopped'
  | 'session_history'
  | 'session_not_found'
  | 'session_closed'
  | 'chat_history'
  | 'terminal_output'
  // Real-time events from backend
  | 'note_created'
  | 'note_updated'
  | 'note_deleted'
  | 'folder_created'
  | 'folder_deleted'

export interface WSMessage {
  type: WSMessageType
  sessionId?: string
  content?: string
  dangerousMode?: boolean
  currentNote?: {
    name: string
    content: string
    type?: string
    projectPath?: string
  }
  tool_name?: string
  tool_args?: string
  error?: string
  messages?: Message[]
  // Real-time event fields
  note?: any // Note from backend
  folder?: any // Folder from backend
  noteId?: string
  path?: string
}

export type ConnectionStatus = 'ready' | 'connecting' | 'generating' | 'error' | 'disconnected'

export interface ClaudeState {
  sessions: ClaudeSession[]
  currentSessionId: string | null
  connectionStatus: ConnectionStatus
  messageQueue: string[]
}
