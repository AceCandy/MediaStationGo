import axios, { AxiosError, CanceledError, type InternalAxiosRequestConfig } from 'axios'

import { useAuthStore } from '../stores/auth'
import { getActivePlayProfileId, getActivePlayProfilePinToken } from '../stores/playProfile'

// Single axios instance used by every API helper. Adds the JWT to outgoing
// requests and routes 401s back to the login page.
export const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

export const LONG_REQUEST_TIMEOUT = 120_000
export const BATCH_REQUEST_TIMEOUT = 300_000

type SessionRequest = InternalAxiosRequestConfig & { _retry?: boolean; _sessionVersion?: number }
let refreshFlight: { version: number; promise: Promise<boolean> } | undefined

function isRefreshRequest(config?: InternalAxiosRequestConfig | null): boolean {
  return Boolean(config?.url?.includes('/auth/refresh'))
}

// Add auth token to requests
api.interceptors.request.use((config) => {
  const request = config as SessionRequest
  const { token, sessionVersion } = useAuthStore.getState()
  if (request._sessionVersion !== undefined && request._sessionVersion !== sessionVersion) {
    throw new CanceledError('会话已切换')
  }
  request._sessionVersion = sessionVersion
  if (token) {
    config.headers = config.headers ?? {}
    config.headers.Authorization = `Bearer ${token}`
  }
  const activeProfileId = getActivePlayProfileId()
  if (activeProfileId) {
    config.headers = config.headers ?? {}
    config.headers['X-Play-Profile-ID'] = activeProfileId
    const pinToken = getActivePlayProfilePinToken()
    if (pinToken) {
      config.headers['X-Play-Profile-PIN-Token'] = pinToken
    }
  }
  return config
})

// Handle 401 errors with token refresh
api.interceptors.response.use(
  (resp) => {
    if ((resp.config as SessionRequest)._sessionVersion !== useAuthStore.getState().sessionVersion) {
      throw new CanceledError('会话已切换')
    }
    return resp
  },
  async (err: AxiosError) => {
    const originalRequest = err.config as SessionRequest | undefined
    const version = useAuthStore.getState().sessionVersion
    if (originalRequest && originalRequest._sessionVersion !== version) {
      return Promise.reject(new CanceledError('会话已切换'))
    }

    // If 401 and not already retried
    if (
      err.response?.status === 401 &&
      originalRequest &&
      !originalRequest._retry &&
      !isRefreshRequest(originalRequest)
    ) {
      originalRequest._retry = true
      const currentToken = useAuthStore.getState().token
      if (currentToken && originalRequest.headers.Authorization !== `Bearer ${currentToken}`) {
        return api(originalRequest)
      }
      if (!refreshFlight || refreshFlight.version !== version) {
        const promise = useAuthStore.getState().tokenRefresh().finally(() => {
          if (refreshFlight?.promise === promise) refreshFlight = undefined
        })
        refreshFlight = { version, promise }
      }
      const refreshed = await refreshFlight.promise
      if (useAuthStore.getState().sessionVersion !== version) {
        return Promise.reject(new CanceledError('会话已切换'))
      }
      if (refreshed) return api(originalRequest)
      useAuthStore.getState().logout()
      if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
      return Promise.reject(err)
    }

    // For other errors, just reject
    return Promise.reject(err)
  },
)

const tokenQuery = () => {
  const t = useAuthStore.getState().token ?? ''
  return `token=${encodeURIComponent(t)}`
}

const profileQuery = () => {
  const id = getActivePlayProfileId()
  if (!id) return ''
  const pinToken = getActivePlayProfilePinToken()
  return `&profile_id=${encodeURIComponent(id)}${
    pinToken ? `&profile_pin_token=${encodeURIComponent(pinToken)}` : ''
  }`
}

// streamURL returns a direct-play URL for <video src>. The JWT is added as
// a query parameter because <video> elements cannot send Authorization
// headers.
export function streamURL(mediaId: string): string {
  return `/api/stream/${encodeURIComponent(mediaId)}?${tokenQuery()}${profileQuery()}`
}

// imageURL converts a remote poster URL into a same-origin proxy URL so it
// can never be blocked by CORS / GFW. Empty strings pass through unchanged.
export type ImageURLOptions =
  | boolean
  | {
      refreshCache?: boolean
      retryFailed?: boolean
      maxWidth?: number
      maxHeight?: number
      quality?: number
      format?: 'webp' | 'jpeg' | 'png'
      original?: boolean
    }

export function imageURL(remote?: string, version?: string, options: ImageURLOptions = false): string {
  if (!remote) return ''
  const versionQuery = version ? `v=${encodeURIComponent(version)}` : ''
  const retryFailed = typeof options === 'boolean' ? options : Boolean(options.retryFailed)
  const refreshCache = typeof options === 'boolean' ? false : Boolean(options.refreshCache)
  const retryQuery = retryFailed ? 'retry=1' : ''
  const refreshQuery = refreshCache ? 'refresh=1' : ''
  const sizing = typeof options === 'boolean' ? {} : options
  const variantQuery = sizing.original ? '' : new URLSearchParams({
    maxWidth: String(sizing.maxWidth ?? 640),
    ...(sizing.maxHeight ? { maxHeight: String(sizing.maxHeight) } : {}),
    quality: String(sizing.quality ?? 80),
    format: sizing.format ?? 'webp',
  }).toString()
  const imageQuery = [versionQuery, retryQuery, refreshQuery, variantQuery].filter(Boolean).join('&')
  // 同源图片由 HttpOnly Cookie 鉴权，避免令牌刷新改变图片缓存地址。
  if (remote.startsWith('/api/')) return withQuery(withoutAuthQuery(remote), imageQuery)
  return withQuery(`/api/img?url=${encodeURIComponent(remote)}`, imageQuery)
}

function withQuery(url: string, query: string): string {
  if (!query) return url
  const [beforeHash, hash] = url.split('#', 2)
  const [path, existing] = beforeHash.split('?', 2)
  const params = new URLSearchParams(existing)
  new URLSearchParams(query).forEach((value, key) => params.set(key, value))
  return `${path}?${params.toString()}${hash ? `#${hash}` : ''}`
}

function withoutAuthQuery(url: string): string {
  const hashIndex = url.indexOf('#')
  const beforeHash = hashIndex >= 0 ? url.slice(0, hashIndex) : url
  const hash = hashIndex >= 0 ? url.slice(hashIndex) : ''
  const queryIndex = beforeHash.indexOf('?')
  if (queryIndex < 0) return url

  const path = beforeHash.slice(0, queryIndex)
  const params = new URLSearchParams(beforeHash.slice(queryIndex + 1))
  const remove = new Set(['token', 'api_key', 'apikey', 'width', 'height', 'maxwidth', 'maxheight', 'fillwidth', 'fillheight', 'quality', 'format'])
  for (const key of [...params.keys()]) if (remove.has(key.toLowerCase())) params.delete(key)
  const query = params.toString()
  return `${path}${query ? `?${query}` : ''}${hash}`
}

// getToken returns the current auth token
export function getToken(): string | null {
  return useAuthStore.getState().token
}

// getRefreshToken returns the current refresh token
export function getRefreshToken(): string | null {
  return useAuthStore.getState().refreshToken
}
