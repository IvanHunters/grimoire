import apiClient from './client'
import type { Note } from '../types/note'

export interface TagCount {
  tag: string
  count: number
}

export const tagsAPI = {
  // Search notes by tags
  searchByTags: async (tags: string[], limit = 50): Promise<Note[]> => {
    const response = await apiClient.get<Note[]>('/search/tags', {
      params: {
        tags: tags.join(','),
        limit,
      },
    })
    return response.data
  },

  // Get all tags with counts
  getAllTags: async (): Promise<TagCount[]> => {
    const response = await apiClient.get<Record<string, number>>('/tags')
    // Convert map to sorted array
    const tags = Object.entries(response.data).map(([tag, count]) => ({
      tag,
      count,
    }))
    // Sort by count descending
    tags.sort((a, b) => b.count - a.count)
    return tags
  },
}
