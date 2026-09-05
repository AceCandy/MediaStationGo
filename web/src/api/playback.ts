import { api } from './client'
import type { HistoryItem, Media, Playlist } from '../types'
import { invalidateHistoryCache } from './history'
import { useAuthStore } from '../stores/auth'
import { getActivePlayProfileId, getActivePlayProfilePinToken } from '../stores/playProfile'

export interface PlaylistDetail {
  playlist: Playlist
  items: Media[]
}

export interface ExternalPlayer {
  name: string
  scheme: string
  url: string
}

function publicOriginHeader() {
  if (typeof window === 'undefined' || !window.location?.origin) return undefined
  return { 'X-MediaStation-Public-Origin': window.location.origin }
}

const favouritesCacheTTL = 5_000
let favouritesAccountID = ''
let favouritesGeneration = 0
let favouritesCache: { expiresAt: number; value: Media[] } | null = null
let favouritesRequest: Promise<Media[]> | null = null
let playlistsRequest: { key: string; promise: Promise<Playlist[]> } | null = null

function playlistsKey(): string {
  return `${useAuthStore.getState().user?.id ?? ''}:${getActivePlayProfileId() ?? ''}`
}

function invalidatePlaylistsRequest() {
  playlistsRequest = null
}

function favouritesAccountKey(): string {
  const accountID = useAuthStore.getState().user?.id ?? ''
  if (accountID !== favouritesAccountID) {
    favouritesCache = null
    favouritesRequest = null
    favouritesGeneration += 1
    favouritesAccountID = accountID
  }
  return accountID
}

function invalidateFavouritesCache() {
  favouritesGeneration += 1
  favouritesCache = null
  favouritesRequest = null
}

function listFavourites(): Promise<Media[]> {
  favouritesAccountKey()
  if (favouritesCache && favouritesCache.expiresAt > Date.now()) {
    return Promise.resolve(favouritesCache.value)
  }
  if (favouritesRequest) return favouritesRequest
  const generation = favouritesGeneration
  const request = api.get<{ items: Media[] }>('/favourites')
    .then((r) => {
      const items = r.data.items ?? []
      if (generation === favouritesGeneration) {
        favouritesCache = { expiresAt: Date.now() + favouritesCacheTTL, value: items }
      }
      return items
    })
    .finally(() => {
      if (favouritesRequest === request) favouritesRequest = null
    })
  favouritesRequest = request
  return request
}

export const playbackAPI = {
  recordProgress: (mediaId: string, sessionId: string, positionMs: number, durationMs: number) =>
    api
      .post('/history', {
        media_id: mediaId,
        session_id: sessionId,
        position_ms: positionMs,
        duration_ms: durationMs,
      })
      .then((r) => {
        invalidateHistoryCache()
        return r.data
      }),

  recordProgressKeepalive: (mediaId: string, sessionId: string, positionMs: number, durationMs: number) => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' }
    const token = useAuthStore.getState().token
    if (token) headers.Authorization = `Bearer ${token}`
    const profileID = getActivePlayProfileId()
    if (profileID) {
      headers['X-Play-Profile-ID'] = profileID
      const pinToken = getActivePlayProfilePinToken()
      if (pinToken) headers['X-Play-Profile-PIN-Token'] = pinToken
    }
    invalidateHistoryCache()
    return fetch('/api/history', {
      method: 'POST',
      headers,
      body: JSON.stringify({ media_id: mediaId, session_id: sessionId, position_ms: positionMs, duration_ms: durationMs }),
      keepalive: true,
    })
  },

  recentHistory: () =>
    api.get<{ items: HistoryItem[] }>('/history').then((r) => r.data.items),

  toggleFavourite: (mediaId: string) =>
    api
      .post<{ favourite: boolean }>(`/favourites/${mediaId}`)
      .then((r) => {
        invalidateFavouritesCache()
        return r.data.favourite
      }),

  favouriteStatus: (mediaId: string) =>
    api
      .get<{ favourite: boolean }>(`/media/${mediaId}/favorite/status`)
      .then((r) => r.data.favourite),

  setSeriesFavourite: (mediaId: string, favourite: boolean) =>
    api.put<{ favourite: boolean }>(`/media/${mediaId}/series/favorite`, { favourite }).then((r) => {
      invalidateFavouritesCache()
      return r.data.favourite
    }),

  listFavourites,

  listPlaylists: () => {
    const key = playlistsKey()
    if (playlistsRequest?.key === key) return playlistsRequest.promise
    const promise = api
      .get<{ items: Playlist[] }>('/playlists')
      .then((r) => r.data.items)
      .finally(() => {
        if (playlistsRequest?.promise === promise) playlistsRequest = null
      })
    playlistsRequest = { key, promise }
    return promise
  },

  createPlaylist: (name: string, isPublic = false) =>
    api
      .post<Playlist>('/playlists', { name, is_public: isPublic })
      .then((r) => {
        invalidatePlaylistsRequest()
        return r.data
      }),

  getPlaylist: (id: string) =>
    api.get<PlaylistDetail>(`/playlists/${id}`).then((r) => r.data),

  addToPlaylist: (id: string, mediaId: string) =>
    api.post(`/playlists/${id}/items`, { media_id: mediaId }).then((r) => r.data),

  removeFromPlaylist: (id: string, mediaId: string) =>
    api.delete(`/playlists/${id}/items/${mediaId}`).then((r) => r.data),

  deletePlaylist: (id: string) =>
    api.delete(`/playlists/${id}`).then((r) => {
      invalidatePlaylistsRequest()
      return r.data
    }),

  externalPlayers: (mediaId: string) =>
    api
      .get<{ players: ExternalPlayer[]; url?: string }>(`/playback/${mediaId}/external-players`, {
        headers: publicOriginHeader(),
      })
      .then((r) => r.data),

  externalURL: (mediaId: string) =>
    api
      .get<{ url: string }>(`/playback/${mediaId}/external-url`, {
        headers: publicOriginHeader(),
      })
      .then((r) => r.data),
}
