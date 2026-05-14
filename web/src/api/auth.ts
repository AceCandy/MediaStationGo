import { api } from './client'
import type { User } from '../types'

export interface LoginResponse {
  token: string
  user: User
}

export const authAPI = {
  login: (username: string, password: string) =>
    api.post<LoginResponse>('/auth/login', { username, password }).then((r) => r.data),

  register: (username: string, password: string) =>
    api.post<User>('/auth/register', { username, password }).then((r) => r.data),

  me: () => api.get<User>('/me').then((r) => r.data),

  changePassword: (oldPassword: string, newPassword: string) =>
    api
      .post('/me/password', { old_password: oldPassword, new_password: newPassword })
      .then((r) => r.data),
}
