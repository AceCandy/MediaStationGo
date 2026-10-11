import { api, BATCH_REQUEST_TIMEOUT } from './client'
import type { MediaCredit } from '../types'

export type DoubanRefreshMode = 'base' | 'full'

export type DoubanSubjectKind = 'movie' | 'series' | 'season' | 'episode'

export interface DoubanSubjectSummary {
  id: string
  douban_id: string
  entity_kind: DoubanSubjectKind
  title: string
  year: string
  rating: number
  quality: string
  has_payload: boolean
  poster_url: string
  fetched_at: string | null
  rich_fetched_at?: string | null
  last_attempt_at: string | null
  next_retry_at: string | null
}

export interface DoubanSubjectDetail extends DoubanSubjectSummary {
  original_name: string
  overview: string
  release_date: string
  genres: string[] | null
  languages: string[] | null
  countries: string[] | null
  directors?: string[] | null
  writers?: string[] | null
  actors?: string[] | null
  aliases?: string[] | null
  durations?: string[] | null
  release_dates?: string[] | null
  imdb_id?: string
  rating_count?: number
  rating_distribution?: { stars: number; percent: number }[] | null
  credits?: MediaCredit[] | null
  awards?: { event: string; category: string; people: string }[] | null
  honors?: { title: string; rank: number }[] | null
  raw_json?: unknown
}

export interface DoubanSubjectPage {
  items: DoubanSubjectSummary[]
  total: number
  page: number
  page_size: number
}

export const doubanAPI = {
  list: (keyword: string, type: string, page: number, signal?: AbortSignal) =>
    api.get<{ data: DoubanSubjectSummary[]; total: number; page: number; page_size: number }>('/admin/douban/subjects', {
      params: { keyword: keyword || undefined, type: type || undefined, page, page_size: 30 }, signal,
    }).then(({ data }) => ({ items: data.data, total: data.total, page: data.page, page_size: data.page_size })),
  detail: (id: string, signal?: AbortSignal) =>
    api.get<{ data: DoubanSubjectDetail }>(`/admin/douban/subjects/${encodeURIComponent(id)}`, { signal }).then(({ data }) => data.data),
  refresh: (id: string, mode: DoubanRefreshMode = 'full', signal?: AbortSignal) =>
    api.post<{ data: { subject: DoubanSubjectDetail; degraded: boolean; supplement_unavailable?: boolean } }>(`/admin/douban/subjects/${encodeURIComponent(id)}/refresh`, undefined, {
      params: { mode }, signal, timeout: BATCH_REQUEST_TIMEOUT,
    }).then(({ data }) => data.data),
}
