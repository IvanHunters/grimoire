import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || '/api'

export interface UploadResponse {
  url: string
  filename: string
}

export async function uploadFile(file: File): Promise<UploadResponse> {
  const formData = new FormData()
  formData.append('file', file)

  const response = await axios.post<UploadResponse>(`${API_URL}/upload`, formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
  })

  return response.data
}
