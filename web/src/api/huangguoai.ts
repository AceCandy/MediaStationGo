import { api, LONG_REQUEST_TIMEOUT } from './client'
import type { Media } from '../types'

export const huangGuoCategories = [['ai-duanju', 'AI短剧'], ['ai-manju', 'AI漫剧'], ['ai-huanlian', 'AI换脸'], ['ai-mogai', 'AI魔改']] as const
export const huangGuoRanks = [['hot', '热播榜'], ['recommend', '推荐榜'], ['potential', '潜力榜']] as const
export interface HuangGuoAIWork {
  id: string; source_id: string; display_id: string; source_category: string; categories: string[]
  kind: 'movie' | 'series' | ''; title: string; overview: string; tags: string[]; rating: number
  episode_count: number; confirmed_episode_count: number; total_episodes: number | null; completed: boolean | null
  artwork_id: string; hydrated: boolean; downloaded: boolean; projection_error?: string; source_created_at: string
}
export interface HuangGuoAIUserCard {
  source_id: string; title: string; kind: 'movie' | 'series'; media_id: string; episode_number: number
  position_ms: number; duration_ms: number; completed: boolean; updated_at: string
}
export interface HuangGuoAIDownload { id: string; source_id: string; title: string; episode: number; status: 'pending_review' | 'downloading' | 'verifying' | 'publishing' | 'waiting_verify' | 'queued' | 'completed' | 'failed' | 'cancelled'; bytes: number; total_bytes: number; attempts: number; error: string; warning: string; review_token: string; confirmed_by: string; confirmed_at: string | null; relative_path: string }
export interface HuangGuoAIDownloadConfig { root: string; temporary_dir: string; output_dir: string; concurrency: number; verification_concurrency: number }
export interface HuangGuoAIDownloadWork { source_id: string; title: string; kind: 'movie' | 'series' | ''; total: number; completed: number; failed: number; pending_review: number; active: number; downloading: number; waiting_verify: number; verifying: number; publishing: number; queued: number; cancelled: number; bytes: number }
const root = '/catalogs/huangguoai'
export const huangguoaiAPI = {
  list: (params: { keyword: string; category: string; tag: string; rank: string; page: number; page_size?: number }, signal?: AbortSignal) => api.get<{ items: HuangGuoAIWork[]; total: number }>(`${root}/works`, { params: { ...params, page_size: params.page_size ?? 50 }, signal }).then(r => r.data),
  search: (keyword: string, page: number, signal?: AbortSignal) => api.get<{ items: HuangGuoAIWork[]; has_more: boolean }>(`${root}/search`, { params: { keyword, page }, signal }).then(r => r.data),
  detail: (id: string, signal?: AbortSignal) => api.get<HuangGuoAIWork>(`${root}/works/${encodeURIComponent(id)}`, { signal }).then(r => r.data),
  episodes: (id: string, page: number, signal?: AbortSignal) => api.get<{ items: { id: string; number: number }[]; total: number }>(`${root}/works/${encodeURIComponent(id)}/episodes`, { params: { page }, signal }).then(r => r.data),
  media: (id: string, page: number, signal?: AbortSignal) => api.get<{ items: Media[]; total: number }>(`${root}/works/${encodeURIComponent(id)}/media`, { params: { page }, signal }).then(r => r.data),
  artwork: (id: string) => `/api${root}/artwork/${encodeURIComponent(id)}`,
  refresh: (id: string) => api.post(`${root}/works/${encodeURIComponent(id)}/refresh`, undefined, { timeout: LONG_REQUEST_TIMEOUT }),
  status: (signal?: AbortSignal) => api.get<{ enabled: boolean }>(`${root}/status`, { signal }).then(r => r.data),
  setEnabled: (enabled: boolean) => api.put(`${root}/status`, { enabled }),
  enqueue: (source_id: string) => api.post<{ added: number }>(`${root}/downloads`, { source_id }).then(r => r.data),
  downloads: (page: number, signal?: AbortSignal) => api.get<{ items: HuangGuoAIDownload[]; total: number }>(`${root}/downloads`, { params: { page }, signal }).then(r => r.data),
  downloadWorks: (page: number, status: string, keyword: string, signal?: AbortSignal) => api.get<{ items: HuangGuoAIDownloadWork[]; total: number }>(`${root}/downloads/works`, { params: { page, status, keyword: keyword || undefined }, signal }).then(r => r.data),
  downloadEpisodes: (id: string, page: number, signal?: AbortSignal) => api.get<{ items: HuangGuoAIDownload[]; total: number }>(`${root}/downloads/works/${encodeURIComponent(id)}/episodes`, { params: { page }, signal }).then(r => r.data),
  downloadWorkAction: (id: string, action: 'retry' | 'cancel') => api.post<{ updated: number }>(`${root}/downloads/works/${encodeURIComponent(id)}/${action}`).then(r => r.data),
  downloadConfig: (signal?: AbortSignal) => api.get<HuangGuoAIDownloadConfig>(`${root}/downloads/config`, { signal }).then(r => r.data),
  saveDownloadConfig: (config: HuangGuoAIDownloadConfig) => api.put(`${root}/downloads/config`, config),
  confirmDownload: (id: string, review_token: string) => api.post(`${root}/downloads/${encodeURIComponent(id)}/confirm`, { review_token }),
  downloadAction: (id: string, action: 'retry' | 'cancel') => api.post(`${root}/downloads/${encodeURIComponent(id)}/${action}`),
  userCards: (tab: 'favourites' | 'history' | 'continue', page: number, signal?: AbortSignal) => api.get<{ items: HuangGuoAIUserCard[]; total: number }>(`${root}/me`, { params: { tab, page }, signal }).then(r => r.data),
  setFavorite: (id: string, favorite: boolean) => api.put(`${root}/works/${encodeURIComponent(id)}/favorite`, { favorite }),
  state: (id: string, signal?: AbortSignal) => api.get<{ favorite: boolean }>(`${root}/works/${encodeURIComponent(id)}/state`, { signal }).then(r => r.data),
  markPlayed: (id: string, played: boolean) => api.put(`${root}/media/${encodeURIComponent(id)}/played`, { played }),
}
