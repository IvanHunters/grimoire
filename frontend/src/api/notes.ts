import apiClient from './client'
import type { Note, CreateNoteRequest, UpdateNoteRequest } from '../types/note'

export const notesAPI = {
  // Get all notes with folder structure
  listNotes: async (): Promise<Note[]> => {
    const response = await apiClient.get<Note[]>('/notes')
    return response.data
  },

  // Get single note by ID
  getNote: async (id: string): Promise<Note> => {
    const response = await apiClient.get<Note>(`/notes/${id}`)
    return response.data
  },

  // Create new note
  createNote: async (data: CreateNoteRequest): Promise<Note> => {
    const response = await apiClient.post<Note>('/notes', data)
    return response.data
  },

  // Update note
  updateNote: async (id: string, data: UpdateNoteRequest): Promise<Note> => {
    const response = await apiClient.put<Note>(`/notes/${id}`, data)
    return response.data
  },

  // Delete note
  deleteNote: async (id: string): Promise<void> => {
    await apiClient.delete(`/notes/${id}`)
  },

  // Search notes by content
  searchNotes: async (query: string): Promise<Note[]> => {
    const response = await apiClient.get<Note[]>('/search', {
      params: { q: query },
    })
    return response.data
  },

  // Get project suggestions for auto-discovery
  getProjectSuggestions: async (title: string): Promise<string[]> => {
    const response = await apiClient.get<{ suggestions: string[] }>(
      '/notes/project-suggestions',
      {
        params: { title },
      }
    )
    return response.data.suggestions
  },
}
