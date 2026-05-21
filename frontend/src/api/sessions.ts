import apiClient from './client'
import type { ClaudeSession } from '../types/claude'

export interface SessionStats {
  totalSessions: number
  activeSessions: number
  totalMessages: number
  totalSizeMb: number
}

export interface SessionMeta {
  id: string
  name: string
  status: string
  workingDir: string
  lastActivity: string
  createdAt: string
  messageCount: number
  sizeBytes: number
}

export const sessionsAPI = {
  listActiveSessions: async (): Promise<ClaudeSession[]> => {
    const response = await apiClient.get<ClaudeSession[]>('/sessions')
    return response.data
  },

  listAllSessions: async (): Promise<SessionMeta[]> => {
    const response = await apiClient.get<SessionMeta[]>('/sessions/all')
    return response.data
  },

  getStats: async (): Promise<SessionStats> => {
    const response = await apiClient.get<SessionStats>('/sessions/stats')
    return response.data
  },

  rotate: async (olderThanDays = 2): Promise<{ rotated: number }> => {
    const response = await apiClient.post<{ rotated: number }>('/sessions/rotate', { older_than_days: olderThanDays })
    return response.data
  },

  clearHistory: async (sessionId: string): Promise<void> => {
    await apiClient.delete(`/sessions/${sessionId}/history`)
  },

  deleteSession: async (sessionId: string): Promise<void> => {
    await apiClient.delete(`/sessions/${sessionId}`)
  },

  renameSession: async (sessionId: string, name: string): Promise<void> => {
    await apiClient.put(`/sessions/${sessionId}/name`, { name })
  },
}
