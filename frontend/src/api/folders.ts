import apiClient from './client'
import type { Folder, CreateFolderRequest, MoveFolderRequest } from '../types/folder'

export const foldersAPI = {
  // Get folder tree structure
  getFolders: async (): Promise<Folder[]> => {
    const response = await apiClient.get<Folder[]>('/folders')
    return response.data
  },

  // Create new folder
  createFolder: async (data: CreateFolderRequest): Promise<Folder> => {
    const response = await apiClient.post<Folder>('/folders', data)
    return response.data
  },

  // Delete empty folder
  deleteFolder: async (path: string): Promise<void> => {
    await apiClient.delete('/folders', {
      params: { path },
    })
  },

  // Move note or folder to different location
  moveFolder: async (data: MoveFolderRequest): Promise<void> => {
    await apiClient.put('/folders/move', data)
  },
}
