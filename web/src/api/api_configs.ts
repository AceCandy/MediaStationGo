import { api } from './client'

export interface APIConfig {
  id: string
  provider: string
  base_url?: string
  model?: string
  extra?: string
  enabled: boolean
  image_direct?: boolean
  use_proxy_pool: boolean
  web_search_enabled: boolean
  description?: string
  has_key: boolean
  masked_key?: string
  created_at: string
  updated_at: string
}

export interface APIConfigPatch {
  api_key?: string
  base_url?: string
  model?: string
  extra?: string
  enabled?: boolean
  image_direct?: boolean
  use_proxy_pool?: boolean
  web_search_enabled?: boolean
  description?: string
}

export interface ProxyPoolItem {
  id: string
  display_url: string
  has_auth: boolean
}

export interface ProxyPoolInput {
  id?: string
  url?: string
}

export const apiConfigsAPI = {
  list: () => api.get<{ items: APIConfig[] }>('/admin/api-configs').then((r) => r.data.items),
  get: (provider: string) => api.get<APIConfig>(`/admin/api-configs/${provider}`).then((r) => r.data),
  update: (provider: string, patch: APIConfigPatch) =>
    api.put<APIConfig>(`/admin/api-configs/${provider}`, patch).then((r) => r.data),
  remove: (provider: string) => api.delete(`/admin/api-configs/${provider}`).then((r) => r.data),
  listProxyPool: () =>
    api.get<{ items: ProxyPoolItem[] }>('/admin/api-proxy-pool').then((r) => r.data.items),
  replaceProxyPool: (items: ProxyPoolInput[]) =>
    api.put<{ items: ProxyPoolItem[] }>('/admin/api-proxy-pool', { items }).then((r) => r.data.items),
}
