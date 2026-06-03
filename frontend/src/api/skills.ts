import apiClient from './client'

export type SkillOverrideState = 'on' | 'name-only' | 'user-invocable-only' | 'off'

export interface ValidationIssue {
  field: string
  message: string
}

export interface SkillSummary {
  name: string
  description: string
  state: SkillOverrideState
  enabled: boolean
  valid: boolean
  issues?: ValidationIssue[]
  frontmatter?: Record<string, unknown>
}

export interface CreateSkillRequest {
  name: string
  description: string
  content: string
  frontmatter?: Record<string, unknown>
}

export const skillsAPI = {
  list: async (): Promise<SkillSummary[]> => {
    const { data } = await apiClient.get<SkillSummary[]>('/skills')
    return data
  },

  create: async (req: CreateSkillRequest): Promise<void> => {
    await apiClient.post('/skills', req)
  },

  remove: async (name: string): Promise<void> => {
    await apiClient.delete(`/skills/${encodeURIComponent(name)}`)
  },

  setState: async (name: string, state: SkillOverrideState): Promise<void> => {
    await apiClient.post(`/skills/${encodeURIComponent(name)}/state`, { state })
  },

  refresh: async (): Promise<void> => {
    await apiClient.post('/skills/refresh')
  },
}
