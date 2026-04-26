import axios from 'axios'

// Base API client with axios
const apiClient = axios.create({
  baseURL: '/api', // Proxied by Vite to http://localhost:8080/api
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor (for auth tokens in future)
apiClient.interceptors.request.use(
  (config) => {
    // Add auth token here if needed
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// Response interceptor (for error handling)
apiClient.interceptors.response.use(
  (response) => {
    return response
  },
  (error) => {
    // Global error handling
    if (error.response) {
      // Server responded with error status
      console.error('API Error:', error.response.status, error.response.data)
    } else if (error.request) {
      // Request made but no response
      console.error('Network Error:', error.message)
    } else {
      // Something else happened
      console.error('Error:', error.message)
    }
    return Promise.reject(error)
  }
)

export default apiClient
