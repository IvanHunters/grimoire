import apiClient from './client'
import type { ClaudeSession } from '../types/claude'

export const sessionsAPI = {
  // Get list of active Claude terminal sessions
  listActiveSessions: async (): Promise<ClaudeSession[]> => {
    const response = await apiClient.get<ClaudeSession[]>('/sessions')
    return response.data
  },
}
