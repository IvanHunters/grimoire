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
  lastActivity: Date
}

export type WSMessageType =
  | 'init'
  | 'message'
  | 'stop'
  | 'switch_session'
  | 'message_start'
  | 'content_delta'
  | 'tool_use'
  | 'message_complete'
  | 'error'
  | 'stopped'

export interface WSMessage {
  type: WSMessageType
  sessionId?: string
  content?: string
  dangerousMode?: boolean
  currentNote?: {
    name: string
    content: string
    projectPath?: string
  }
  tool_name?: string
  tool_args?: string
  error?: string
}

export type ConnectionStatus = 'ready' | 'connecting' | 'generating' | 'error' | 'disconnected'

export interface ClaudeState {
  sessions: ClaudeSession[]
  currentSessionId: string | null
  connectionStatus: ConnectionStatus
  messageQueue: string[]
}
