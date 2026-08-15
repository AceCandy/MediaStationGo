import { api } from './client'
import type { AccessLog, Setting, User } from '../types'

export interface PlayerRequestLog {
  id: string
  requested_at: string
  method: string
  route: string
  status: number
  duration_ms: number
  ip: string
  path_params: Record<string, string[]>
  headers: Record<string, string[]>
  query: Record<string, string[]>
}

export interface PlayerRequestLogPage {
  items: PlayerRequestLog[]
  page: number
  page_size: number
  total: number
}

export interface PlayerRequestLogQuery {
  month: string
  page?: number
  page_size?: number
  path?: string
  method?: string
  status?: number
}

export const adminAPI = {
  listUsers: () => api.get<User[]>('/admin/users').then((r) => r.data),

  createUser: (payload: { username: string; password: string }) =>
    api.post<User>('/admin/users', payload).then((r) => r.data),

  updateUser: (id: string, payload: { username: string }) =>
    api.patch<User>(`/admin/users/${id}`, payload).then((r) => r.data),

  resetUserPassword: (id: string, password: string) =>
    api.patch(`/admin/users/${id}/password`, { password }).then((r) => r.data),

  setUserStatus: (id: string, isActive: boolean) =>
    api.patch<User>(`/admin/users/${id}/status`, { is_active: isActive }).then((r) => r.data),

  deleteUser: (id: string) => api.delete(`/admin/users/${id}`).then((r) => r.data),

  listSettings: () => api.get<Setting[]>('/admin/settings').then((r) => r.data),

  updateSetting: (key: string, value: string) =>
    api.put('/admin/settings', { key, value }).then((r) => r.data),

  recentLogs: () => api.get<AccessLog[]>('/admin/logs').then((r) => r.data),

  playerRequestLogs: (params: PlayerRequestLogQuery) =>
    api.get<PlayerRequestLogPage>('/admin/player-request-logs', { params }).then((r) => r.data),
}
