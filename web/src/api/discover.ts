import { api } from './client'
import type { Media } from '../types'

// TMDb-derived "Match" rows used by trending/popular rails. We re-use the
// Media interface — only TMDb id / poster / overview are populated.
export interface DiscoverItem extends Partial<Media> {
  tmdb_id: number
  title: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  year: number
  rating: number
}

// 后端在 TMDb 不可达 / API key 缺失时统一返回 { items: [], error: "..." }
// 200 状态码——前端必须能区分这两种情况，不能简单用 items.length === 0
// 推断"未配置 API key"。
export interface DiscoverResp {
  items: DiscoverItem[]
  error?: string
}

export const discoverAPI = {
  trending: () =>
    api.get<DiscoverResp>('/discover/trending').then((r) => ({
      items: r.data.items ?? [],
      error: r.data.error,
    })),
  popular: () =>
    api.get<DiscoverResp>('/discover/popular').then((r) => ({
      items: r.data.items ?? [],
      error: r.data.error,
    })),
}
