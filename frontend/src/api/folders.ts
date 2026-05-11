import apiClient from './client'
import type { Folder, FolderNode, CreateFolderRequest, MoveFolderRequest } from '../types/folder'

export const foldersAPI = {
  // Get folder tree structure
  getFolders: async (): Promise<FolderNode> => {
    const response = await apiClient.get<FolderNode>('/folders')
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

  // Update folder metadata (projectPath)
  updateFolder: async (path: string, projectPath: string): Promise<Folder> => {
    const response = await apiClient.put<Folder>('/folders', { projectPath }, {
      params: { path },
    })
    return response.data
  },
}
