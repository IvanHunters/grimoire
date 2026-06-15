import apiClient from './client'
import type { ClaudeSession } from '../types/claude'

// SessionStatus is the live state of a session — fresh from the claude
// daemon (when daemon-backed) or synthesized (for subprocess sessions).
// Use the chat polling hook to keep this in sync with what the chat is
// actually doing.
export interface SessionStatus {
  sessionId: string
  daemonBacked: boolean
  daemonShort?: string
  daemonUuid?: string
  tempo: 'idle' | 'active' | 'blocked' | 'unknown'
  state: 'working' | 'blocked' | 'done' | 'failed' | 'stopped' | 'running' | 'unknown'
  detail: string
  needs: string
}

// SessionLiveState is the subset of the live daemon record useful for
// status badges in list views. Null when the session is historical-only.
export interface SessionLiveState {
  tempo: string
  state: string
  detail: string
  needs: string
}

// SessionListItem is the merged view of a session for SessionsPanel.
// Backend joins live (daemon) and historical (JSONL on disk) by sessionId.
export interface SessionListItem {
  sessionId: string
  daemonShort?: string
  name: string
  firstPrompt: string
  cwd: string
  gitBranch?: string
  startedAt: string
  lastActivity: string
  sizeBytes: number
  live?: SessionLiveState | null
  jsonlPath: string
  /** True for sessions imported from .jsonl uploads that landed in the
   *  "-imported" bucket (no cwd metadata in the source transcript). */
  imported?: boolean
}

// SessionSearchHit is one match from global transcript search.
export interface SessionSearchHit {
  sessionId: string
  sessionName: string // matches TranscriptViewer header (ai-title or first prompt)
  sessionDir: string
  cwd: string
  snippet: string
  role: 'user' | 'assistant'
  lineNumber: number
}

// TranscriptMessage is one chat-bubble-worthy event from a JSONL.
// LineNumber refers to the source JSONL line — useful for scroll-to-match
// when opening from a search hit.
export interface TranscriptMessage {
  uuid: string
  lineNumber: number
  role: 'user' | 'assistant'
  timestamp: string
  text: string
  hasTools: boolean
  toolUses?: string[]
  isError?: boolean
}

export interface TranscriptHeader {
  sessionId: string
  name: string
  cwd: string
  gitBranch?: string
  claudeVersion?: string
  startedAt: string
  messageCount: number
}

export interface Transcript {
  header: TranscriptHeader
  messages: TranscriptMessage[]
}

export const sessionsAPI = {
  listActiveSessions: async (): Promise<ClaudeSession[]> => {
    const response = await apiClient.get<ClaudeSession[]>('/sessions')
    return response.data
  },

  deleteSession: async (sessionId: string, opts: { deleteTranscript?: boolean } = {}): Promise<void> => {
    const params: Record<string, string> = {}
    if (opts.deleteTranscript) params.transcript = 'true'
    await apiClient.delete(`/sessions/${sessionId}`, { params })
  },

  renameSession: async (sessionId: string, name: string): Promise<void> => {
    await apiClient.put(`/sessions/${sessionId}/name`, { name })
  },

  getStatus: async (sessionId: string): Promise<SessionStatus> => {
    const response = await apiClient.get<SessionStatus>(`/sessions/${sessionId}/status`)
    return response.data
  },

  // Pass cwd to scope to one project, or omit for an all-projects view.
  listByProject: async (cwd?: string): Promise<SessionListItem[]> => {
    const params = cwd ? { cwd } : undefined
    const response = await apiClient.get<SessionListItem[]>('/sessions/by-project', { params })
    return response.data
  },

  // Substring search across user/assistant message bodies in every
  // transcript. Pass cwd to scope; default limit 100.
  search: async (q: string, opts: { cwd?: string; limit?: number } = {}): Promise<SessionSearchHit[]> => {
    const params: Record<string, string | number> = { q }
    if (opts.cwd) params.cwd = opts.cwd
    if (opts.limit) params.limit = opts.limit
    const response = await apiClient.get<SessionSearchHit[]>('/sessions/search', { params })
    return response.data
  },

  // Read the full parsed transcript for read-only viewing. Backend
  // filters out metadata events; we get clean user/assistant messages.
  getTranscript: async (sessionId: string): Promise<Transcript> => {
    const response = await apiClient.get<Transcript>(`/sessions/${sessionId}/transcript`)
    return response.data
  },

  // Upload one or more .jsonl transcripts to import them into the
  // claude projects tree. Backend picks the project subdirectory from
  // the JSONL's cwd field; sessions without one go to "-imported".
  // Returns per-file results so partial failures surface in UI.
  importTranscripts: async (files: File[]): Promise<ImportFileResult[]> => {
    const form = new FormData()
    for (const f of files) form.append('file', f, f.name)
    const response = await apiClient.post<ImportFileResult[]>('/sessions/import', form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    return response.data
  },
}

export interface ImportFileResult {
  filename: string
  result?: {
    sessionId: string
    path: string
    cwd: string
    messages: number
  }
  error?: string
}
