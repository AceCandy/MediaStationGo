import axios, { AxiosError } from 'axios'

import { useAuthStore } from '../stores/auth'

// Single axios instance used by every API helper. Adds the JWT to outgoing
// requests and routes 401s back to the login page.
export const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

api.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token
  if (token) {
    config.headers = config.headers ?? {}
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (resp) => resp,
  (err: AxiosError) => {
    if (err.response?.status === 401) {
      useAuthStore.getState().logout()
      if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(err)
  },
)

// Helper that returns a streaming URL with the JWT appended as a query
// parameter so <video src> works without an Authorization header.
export function streamURL(mediaId: string): string {
  const token = useAuthStore.getState().token ?? ''
  return `/api/stream/${encodeURIComponent(mediaId)}?token=${encodeURIComponent(token)}`
}
