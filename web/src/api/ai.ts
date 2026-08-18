import { api } from './client'
import { useAuthStore } from '../stores/auth'
import { getActivePlayProfileId } from '../stores/playProfile'
import type { Media } from '../types'

let statusRequest: { key: string; promise: Promise<{ enabled: boolean; provider: string; model: string }> } | null = null

export interface SearchIntent {
  query: string
  year?: number
  genre?: string
  type?: string
  sort?: string
  language?: string
}

export interface ExternalMediaResult {
  source: string
  media_type?: string
  title: string
  original_name?: string
  overview?: string
  poster_url?: string
  backdrop_url?: string
  year?: number
  rating?: number
  tmdb_id?: number
  bangumi_id?: number
  douban_id?: string
}

export const aiAPI = {
  status: () => {
    const key = `${useAuthStore.getState().user?.id ?? ''}:${getActivePlayProfileId() ?? ''}`
    if (statusRequest?.key === key) return statusRequest.promise
    const promise = api
      .get<{ enabled: boolean; provider: string; model: string }>('/ai/status')
      .then((r) => r.data)
      .finally(() => {
        if (statusRequest?.promise === promise) statusRequest = null
      })
    statusRequest = { key, promise }
    return promise
  },

  smartSearch: (query: string) =>
    api
      .post<{ intent: SearchIntent; items: Media[]; external_items: ExternalMediaResult[] }>(
        '/ai/search',
        { query },
      )
      .then((r) => r.data),

  recommend: () => api.get<{ titles: string[] }>('/ai/recommend').then((r) => r.data.titles),
}
