import { api, LONG_REQUEST_TIMEOUT } from './client'
import { useAuthStore } from '../stores/auth'
import { getActivePlayProfileId } from '../stores/playProfile'
import type { Media, MediaCredit } from '../types'

let sectionsRequest: { key: string; promise: Promise<DiscoverSection[]> } | null = null

// TMDb-derived "Match" rows used by trending/popular rails. We re-use the
// Media interface — only TMDb id / poster / overview are populated.
export interface DiscoverItem extends Partial<Media> {
  source?: string
  media_type?: string
  tmdb_id?: number
  douban_id?: string
  bangumi_id?: number
  title: string
  poster_url?: string
  backdrop_url?: string
  overview?: string
  year?: number
  rating?: number
}

export interface DiscoverSection {
  key: string
  label: string
  provider?: string
}

export interface DiscoverIdentity { tmdb_id: number; media_type: 'movie' | 'tv' }
export interface DiscoverDetail extends Omit<DiscoverItem, 'genres' | 'countries' | 'languages'> {
  genres?: string[]
  countries?: string[]
  languages?: string[]
  runtime_minutes: number[]
  credits: MediaCredit[]
  local_metadata?: boolean
}

export function discoverTMDbIdentity(item: DiscoverItem): DiscoverIdentity | null {
  return (!item.source || item.source === 'tmdb') && Number.isSafeInteger(item.tmdb_id) && (item.tmdb_id ?? 0) > 0 && (item.media_type === 'movie' || item.media_type === 'tv')
    ? { tmdb_id: item.tmdb_id!, media_type: item.media_type } : null
}

export function discoverIdentityKey(item: DiscoverIdentity): string { return `${item.media_type}:${item.tmdb_id}` }

export interface DiscoverFeedMeta {
  page: number
  has_next: boolean
  duration_ms?: number
  error?: string
  warning?: string
  stale?: boolean
  disabled?: boolean
}

export interface DiscoverFeedResult {
  items: Record<string, DiscoverItem[]>
  meta: Record<string, DiscoverFeedMeta>
}

// 后端在 TMDb 不可达 / API key 缺失时统一返回 { items: [], error: "..." }
// 200 状态码——前端必须能区分这两种情况，不能简单用 items.length === 0
// 推断"未配置 API key"。
export interface DiscoverResp {
  items: DiscoverItem[]
  error?: string
}

export const discoverAPI = {
  search: (query: string, kind: string, page: number, signal?: AbortSignal) => api.post<{ items: DiscoverItem[]; has_next: boolean; page: number }>('/discover/search', { query, kind, page }, { signal }).then((response) => response.data),
  detail: (item: DiscoverIdentity, signal?: AbortSignal) => api.get<DiscoverDetail>(`/discover/tmdb/${item.media_type}/${item.tmdb_id}`, { signal }).then((r) => r.data),
  refresh: (item: DiscoverIdentity, signal?: AbortSignal) => api.post<DiscoverDetail>(`/discover/tmdb/${item.media_type}/${item.tmdb_id}/refresh`, undefined, { signal, timeout: LONG_REQUEST_TIMEOUT }).then((r) => r.data),
  libraryStatus: (items: DiscoverIdentity[], signal?: AbortSignal) => api.post<{ items: DiscoverIdentity[] }>('/discover/library-status', { items }, { signal }).then((r) => r.data.items),
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
  sections: () => {
    const key = `${useAuthStore.getState().user?.id ?? ''}:${getActivePlayProfileId() ?? ''}`
    if (sectionsRequest?.key === key) return sectionsRequest.promise
    const promise = api
      .get<{ sections: DiscoverSection[] }>('/discover/sections')
      .then((r) => r.data.sections)
      .finally(() => {
        if (sectionsRequest?.promise === promise) sectionsRequest = null
      })
    sectionsRequest = { key, promise }
    return promise
  },
  feed: (sectionKeys: string[], page = 1, refresh = false, signal?: AbortSignal): Promise<DiscoverFeedResult> =>
    api
      .get<Record<string, DiscoverItem[] | DiscoverFeedMeta | Record<string, DiscoverFeedMeta> | null>>('/discover/feed', {
        params: { sections: sectionKeys.join(','), page, refresh: refresh ? 1 : undefined },
        signal,
      })
      .then((r) => {
        const raw = r.data
        const meta = ((raw._meta as Record<string, DiscoverFeedMeta> | undefined) ?? {})
        const items: Record<string, DiscoverItem[]> = {}
        for (const key of sectionKeys) {
          const row = raw[key]
          items[key] = Array.isArray(row) ? row : []
        }
        return { items, meta }
      }),
}
