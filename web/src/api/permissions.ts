import { api } from './client'

export interface UserPermission {
  user_id: string
  can_play_media: boolean
  can_favorite: boolean
  can_view_history: boolean
  can_view_dashboard: boolean
  can_view_discover: boolean
  can_manage_downloads: boolean
  can_manage_subscriptions: boolean
  can_manage_sites: boolean
  can_manage_files: boolean
  can_manage_strm: boolean
  can_cast: boolean
  can_use_ai_assistant: boolean
  can_access_settings: boolean
  updated_at: string
}

type ApiEnvelope<T> = {
  code?: number
  message?: string
  data?: T
}

function unwrap<T>(raw: T | ApiEnvelope<T>): T {
  if (raw && typeof raw === 'object' && 'data' in raw && (raw as ApiEnvelope<T>).data !== undefined) {
    return (raw as ApiEnvelope<T>).data as T
  }
  return raw as T
}

export const permissionsAPI = {
  // Caller's effective permissions; admins always get the all-true set.
  mine: () => api.get<UserPermission | ApiEnvelope<UserPermission>>('/auth/permissions').then((r) => unwrap(r.data)),

  // Admin endpoints
  get: (userID: string) =>
    api
      .get<UserPermission | ApiEnvelope<UserPermission>>(`/admin/users/${userID}/permissions`)
      .then((r) => unwrap(r.data)),

  save: (userID: string, p: UserPermission) =>
    api
      .put<UserPermission | ApiEnvelope<UserPermission>>(`/admin/users/${userID}/permissions`, p)
      .then((r) => unwrap(r.data)),

  reset: (userID: string) =>
    api
      .post<UserPermission | ApiEnvelope<UserPermission>>(`/admin/users/${userID}/permissions/reset`)
      .then((r) => unwrap(r.data)),
}
