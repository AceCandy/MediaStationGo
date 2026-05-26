// 令牌刷新 API 模块
import { api } from './client'

// 刷新令牌请求/响应
export interface RefreshTokenRequest {
  refresh_token: string
}

export interface RefreshTokenResponse {
  token: string
  refresh_token: string
  expires_in: number
  token_type: string
}

// 刷新访问令牌。
//
// 后端响应封装在 { code, message, data } 里，需解包 .data。
// /auth/login 的响应是直接展开的（{tokens:..., user:...}），
// /auth/refresh 的响应是包装过的 — 这里负责拉平成前端使用的 shape。
export async function refreshToken(refreshToken: string): Promise<RefreshTokenResponse> {
  const resp = await api.post<{
    code: number
    message: string
    data: RefreshTokenResponse
  }>('/auth/refresh', { refresh_token: refreshToken })
  const body = resp.data
  if (!body || !body.data || !body.data.token) {
    throw new Error(body?.message || 'refresh failed')
  }
  return body.data
}

// 登出
export async function logout(): Promise<void> {
  await api.post('/me/logout')
}
