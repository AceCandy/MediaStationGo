import { api, LONG_REQUEST_TIMEOUT } from './client'
import type { Media } from '../types'

export interface HongGuoWork {
  id: string
  source_id: string
  source_category: string
  kind: 'movie' | 'series'
  title: string
  overview: string
  episode_count: number
  total_episodes: number
  accessible_episodes: number
  update_text: string
  completed: boolean
  first_visible_at?: string
  rating: number
  rating_count: number
  related_album_id: string
  season_index: number
  refreshed_at: string
}

export interface HongGuoDetail extends HongGuoWork {
  tags: string[]
  episodes: { id: string; work_id: string; number: number }[]
  credits: { person_id: string; subtitle: string; person: { source_id: string; name: string } }[]
  artwork: { id: string; work_id?: string; person_id?: string }[]
  group?: { group_id: string; work_id: string; season_number: number }
}

export interface HongGuoListWork extends HongGuoWork { artwork_id: string; tags: string[]; hydrated: boolean; group_id?: string; downloaded?: boolean }

export interface HongGuoGroup { id: string; title: string; members: (HongGuoWork & { season_number: number })[] }

export interface HongGuoUserCard {
  source_id: string; title: string; kind: 'movie' | 'series'; media_id: string
  season_number: number; episode_number: number; position_ms: number; duration_ms: number; completed: boolean; updated_at: string
}

export interface HongGuoLibraryCard { id: string; source_id: string; title: string; kind: 'movie' | 'series'; artwork_id: string }
export interface HongGuoPendingMedia { id: string; source_id: string; title: string; path: string; reason: string }

export const hongguoAPI = {
  search: (keyword: string, signal?: AbortSignal) => api.get<{ items: HongGuoListWork[]; total: number }>('/catalogs/hongguo/search', { params: { keyword }, signal }).then((r) => r.data),
  list: (keyword: string, sourceCategory: string, category: string, rank: string, page: number, signal?: AbortSignal) => api.get<{ items: HongGuoListWork[]; total: number }>('/catalogs/hongguo/works', { params: { keyword, source_category: sourceCategory || undefined, category: category || undefined, rank: rank || undefined, page, page_size: 50 }, signal }).then((r) => r.data),
  detail: (id: string, signal?: AbortSignal) => api.get<HongGuoDetail>(`/catalogs/hongguo/works/${encodeURIComponent(id)}`, { signal }).then((r) => r.data),
  media: (id: string, page: number, signal?: AbortSignal) => api.get<{ items: Media[]; total: number }>(`/catalogs/hongguo/works/${encodeURIComponent(id)}/media`, { params: { page }, signal }).then((r) => r.data),
  library: (id: string, page: number, signal?: AbortSignal) => api.get<{ items: HongGuoLibraryCard[]; total: number }>(`/catalogs/hongguo/libraries/${encodeURIComponent(id)}`, { params: { page }, signal }).then((r) => r.data),
  refresh: (id: string) => api.post(`/catalogs/hongguo/works/${encodeURIComponent(id)}/refresh`, undefined, { timeout: LONG_REQUEST_TIMEOUT }),
  setSourceCategory: (id: string, sourceCategory: string) => api.put(`/catalogs/hongguo/works/${encodeURIComponent(id)}/category`, { source_category: sourceCategory }),
  status: () => api.get<{ enabled: boolean }>('/catalogs/hongguo/status').then((r) => r.data),
  setEnabled: (enabled: boolean) => api.put('/catalogs/hongguo/status', { enabled }),
  cancel: () => api.post('/catalogs/hongguo/cancel'),
  pending: (page: number, signal?: AbortSignal) => api.get<{ items: HongGuoPendingMedia[]; total: number }>('/catalogs/hongguo/pending', { params: { page }, signal }).then((r) => r.data),
  artwork: (id: string) => `/api/catalogs/hongguo/artwork/${encodeURIComponent(id)}`,
  group: (id: string, signal?: AbortSignal) => api.get<HongGuoGroup>(`/catalogs/hongguo/groups/${encodeURIComponent(id)}`, { signal }).then((r) => r.data),
  userCards: (tab: 'favourites' | 'history' | 'continue', page: number, signal?: AbortSignal) => api.get<{ items: HongGuoUserCard[]; total: number }>('/catalogs/hongguo/me', { params: { tab, page }, signal }).then((r) => r.data),
  favorite: (id: string, signal?: AbortSignal) => api.get<{ favorite: boolean }>(`/catalogs/hongguo/works/${encodeURIComponent(id)}/favorite`, { signal }).then((r) => r.data.favorite),
  setFavorite: (id: string, favorite: boolean) => api.put(`/catalogs/hongguo/works/${encodeURIComponent(id)}/favorite`, { favorite }),
  markPlayed: (mediaID: string, played: boolean) => api.put(`/catalogs/hongguo/media/${encodeURIComponent(mediaID)}/played`, { played }),
}
