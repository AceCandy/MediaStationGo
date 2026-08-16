import { api } from './client'
import type { HistoryItem, HistoryStats, Media } from '../types'
import { useAuthStore } from '../stores/auth'

const historyCacheTTL = 5_000
const historyCache = new Map<string, { expiresAt: number; value: unknown }>()
const historyRequests = new Map<string, Promise<unknown>>()
let historyAccountID = ''
let historyGeneration = 0

function accountCacheKey(key: string): string {
  const accountID = useAuthStore.getState().user?.id ?? ''
  if (accountID !== historyAccountID) {
    historyCache.clear()
    historyRequests.clear()
    historyGeneration += 1
    historyAccountID = accountID
  }
  return `${accountID}:${key}`
}

function cachedHistoryRequest<T>(key: string, load: () => Promise<T>): Promise<T> {
  const cacheKey = accountCacheKey(key)
  const cached = historyCache.get(cacheKey)
  if (cached && cached.expiresAt > Date.now()) return Promise.resolve(cached.value as T)
  const pending = historyRequests.get(cacheKey)
  if (pending) return pending as Promise<T>
  const generation = historyGeneration
  const request = load()
    .then((value) => {
      if (generation === historyGeneration) {
        historyCache.set(cacheKey, { expiresAt: Date.now() + historyCacheTTL, value })
      }
      return value
    })
    .finally(() => {
      if (historyRequests.get(cacheKey) === request) historyRequests.delete(cacheKey)
    })
  historyRequests.set(cacheKey, request)
  return request
}

export function invalidateHistoryCache() {
  historyGeneration += 1
  historyCache.clear()
  historyRequests.clear()
}

// historyAPI wraps /watch-history and /history. The two share storage on
// the backend; we treat /watch-history as the rich admin/dashboard
// surface and /history as the legacy resume-position write.
export const historyAPI = {
  list: (limit = 50) => cachedHistoryRequest(`list:${limit}`, () =>
    api
      .get<HistoryItem[]>('/watch-history', { params: { limit } })
      .then((r) => r.data)),

  stats: () => cachedHistoryRequest('stats', () => api.get<HistoryStats>('/watch-history/stats').then((r) => r.data)),

  continueWatching: (limit = 10) => cachedHistoryRequest(`continue:${limit}`, () =>
    api
      .get<{ history: HistoryItem; media: Media }[]>('/watch-history/continue', {
        params: { limit },
      })
      .then((r) => r.data)),

  clear: (mediaID?: string, status?: 'completed' | 'incomplete') =>
    api
      .delete('/watch-history', {
        params: {
          ...(mediaID ? { media_id: mediaID } : {}),
          ...(status ? { status } : {}),
        },
      })
      .then((r) => {
        invalidateHistoryCache()
        return r.data
      }),

  remove: (id: string) => api.delete(`/watch-history/${id}`).then((r) => {
    invalidateHistoryCache()
    return r.data
  }),
}
