import { api, BATCH_REQUEST_TIMEOUT, LONG_REQUEST_TIMEOUT } from './client'
import { useAuthStore } from '../stores/auth'
import { getActivePlayProfileId } from '../stores/playProfile'
import type { Library, LibraryRoot, Media, MediaCredit } from '../types'
import type { SeriesCard } from '../utils/groupSeries'

const recentRequests = new Map<string, Promise<SeriesCard[]>>()
const libraryRequests = new Map<string, Promise<unknown>>()

function libraryRequest<T>(key: string, load: () => Promise<T>): Promise<T> {
  const scope = `${useAuthStore.getState().user?.id ?? ''}:${getActivePlayProfileId() ?? ''}:${key}`
  const pending = libraryRequests.get(scope)
  if (pending) return pending as Promise<T>
  const request = load().finally(() => {
    if (libraryRequests.get(scope) === request) libraryRequests.delete(scope)
  })
  libraryRequests.set(scope, request)
  return request
}

export interface MediaPage {
  items: Media[]
  total: number
  page: number
  page_size: number
}

export interface MediaSearchPage {
  items: Media[]
  total?: number
  page?: number
  page_size?: number
}

export interface MediaScrapeIssue {
  id: string
  title: string
  path: string
  year: number
  season_num: number
  episode_num: number
  library_id: string
  library_name: string
  library_type: string
  scrape_status: 'error' | 'no_match'
  reason: string
}

export interface STRMDeleteTarget {
  target_path: string
  parent_path?: string
}

export interface MediaScrapeIssuePage {
  items: MediaScrapeIssue[]
  total: number
  page: number
  page_size: number
}

export interface SeriesPage {
  items: SeriesCard[]
  total: number
  page: number
  page_size: number
}

export interface LibraryRootInput {
  path: string
  enabled?: boolean
  sort_order?: number
}

export interface ManualScrapeCandidate {
  source: string
  media_type?: string
  title: string
  original_name?: string
  overview?: string
  poster_url?: string
  backdrop_url?: string
  year?: number
  release_date?: string
  rating?: number
  tmdb_id?: number
  bangumi_id?: number
  douban_id?: string
  thetvdb_id?: string
  languages?: string[]
  countries?: string[]
  genres?: string[]
  nsfw?: boolean
}

export interface ScrapeOptions {
  episode_artwork?: boolean
  episode_images?: boolean
  refresh_matched?: boolean
  include_matched?: boolean
}

export type ManualScrapeApplyOptions = ScrapeOptions

export interface LibraryMediaFilters {
  missingPoster?: boolean
  missingChineseTitle?: boolean
}

export interface MediaMetadataUpdate {
  title?: string
  original_name?: string
  overview?: string
  year?: number
  release_date?: string
  rating?: number
  season_num?: number
  episode_num?: number
  tmdb_id?: number
  bangumi_id?: number
  douban_id?: string
  thetvdb_id?: string
  languages?: string
  countries?: string
  genres?: string
  nsfw?: boolean
}

export const libraryAPI = {
  list: (options?: { includeHidden?: boolean }) =>
    libraryRequest(`list:${options?.includeHidden ? 1 : 0}`, () =>
      api
        .get<Library[]>('/libraries', {
          params: options?.includeHidden ? { include_hidden: 1 } : undefined,
        })
        .then((r) => r.data)),

  get: (id: string, options?: { includeHidden?: boolean }) =>
    libraryRequest(`get:${id}:${options?.includeHidden ? 1 : 0}`, () =>
      api
        .get<Library>(`/libraries/${id}`, {
          params: options?.includeHidden ? { include_hidden: 1 } : undefined,
        })
        .then((r) => r.data)),

  create: (name: string, path: string, type: string) =>
    api.post<Library>('/libraries', { name, path, type }).then((r) => r.data),

  createWithRoots: (name: string, type: string, roots: LibraryRootInput[]) =>
    api.post<Library>('/libraries', { name, type, roots }).then((r) => r.data),

  update: (id: string, payload: { cover_url: string }) =>
    api.patch<Library>(`/libraries/${id}`, payload).then((r) => r.data),

  uploadCover: (id: string, cover: File) => {
    const form = new FormData()
    form.append('cover', cover)
    return api.put<Library>(`/libraries/${id}/cover`, form).then((r) => r.data)
  },

  clearCover: (id: string) => api.delete<Library>(`/libraries/${id}/cover`).then((r) => r.data),

  remove: (id: string) => api.delete(`/libraries/${id}`).then((r) => r.data),

  listRoots: (id: string) => api.get<LibraryRoot[]>(`/libraries/${id}/roots`).then((r) => r.data),

  addRoot: (id: string, root: LibraryRootInput) =>
    api.post<LibraryRoot>(`/libraries/${id}/roots`, root).then((r) => r.data),

  updateRoot: (id: string, rootID: string, root: Partial<LibraryRootInput>) =>
    api.patch<LibraryRoot>(`/libraries/${id}/roots/${rootID}`, root).then((r) => r.data),

  removeRoot: (id: string, rootID: string) => api.delete(`/libraries/${id}/roots/${rootID}`).then((r) => r.data),

  listMedia: (id: string, page = 1, pageSize = 50, options?: LibraryMediaFilters & { groupVersions?: boolean }) =>
    libraryRequest(`media:${id}:${page}:${pageSize}:${options?.groupVersions === false ? 0 : 1}:${options?.missingPoster ? 1 : 0}:${options?.missingChineseTitle ? 1 : 0}`, () =>
      api
        .get<MediaPage>(`/libraries/${id}/media`, {
          params: {
            page,
            page_size: pageSize,
            group_versions: options?.groupVersions === false ? 0 : undefined,
            missing_poster: options?.missingPoster ? 1 : undefined,
            missing_chinese_title: options?.missingChineseTitle ? 1 : undefined,
          },
          timeout: LONG_REQUEST_TIMEOUT,
        })
        .then((r) => r.data)),

  listSeries: (id: string, page = 1, pageSize = 500, options?: LibraryMediaFilters) =>
    libraryRequest(`series:${id}:${page}:${pageSize}:${options?.missingPoster ? 1 : 0}:${options?.missingChineseTitle ? 1 : 0}`, () =>
      api
        .get<SeriesPage>(`/libraries/${id}/series`, {
          params: {
            page,
            page_size: pageSize,
            missing_poster: options?.missingPoster ? 1 : undefined,
            missing_chinese_title: options?.missingChineseTitle ? 1 : undefined,
          },
          timeout: LONG_REQUEST_TIMEOUT,
        })
        .then((r) => r.data)),

  listSeriesEpisodes: (id: string, key: string) =>
    libraryRequest(`episodes:${id}:${key}`, () =>
      api
        .get<{ items: Media[]; total: number }>(`/libraries/${id}/series/episodes`, {
          params: { key },
          timeout: LONG_REQUEST_TIMEOUT,
        })
        .then((r) => r.data)),
}

export const mediaAPI = {
  recent: (limit = 24) => {
    const accountID = useAuthStore.getState().user?.id ?? ''
    const key = `${accountID}:${getActivePlayProfileId() ?? ''}:${limit}`
    const pending = recentRequests.get(key)
    if (pending) return pending
    const request = api
      .get<SeriesCard[]>('/media/recent', { params: { limit } })
      .then((r) => r.data)
      .finally(() => {
        if (recentRequests.get(key) === request) recentRequests.delete(key)
      })
    recentRequests.set(key, request)
    return request
  },

  search: (q: string, limit = 50) =>
    api.get<MediaSearchPage>('/media', { params: { q, limit } }).then((r) => r.data),

  searchPage: (q: string, page = 1, pageSize = 50, options?: { groupVersions?: boolean }) =>
    api
      .get<MediaSearchPage>('/media', {
        params: {
          q,
          page,
          page_size: pageSize,
          group_versions: options?.groupVersions === false ? 0 : undefined,
        },
        timeout: LONG_REQUEST_TIMEOUT,
      })
      .then((r) => r.data),

  get: (id: string) => api.get<Media>(`/media/${id}`).then((r) => r.data),

  getSTRMTarget: (id: string) =>
    api.get<{ target: string }>(`/media/${id}/strm-target`).then((r) => r.data.target),

  getSTRMDeleteTarget: (id: string) =>
    api.get<STRMDeleteTarget>(`/admin/media/${id}/strm-delete-target`).then((r) => r.data),

  deleteSTRMTarget: (id: string, deleteParent: boolean) =>
    api.delete<{ removed: boolean; path: string }>(`/admin/media/${id}/strm-delete-target`, {
      data: { delete_parent: deleteParent },
    }).then((r) => r.data),

  listScrapeIssues: (options: { libraryID?: string; status?: 'error' | 'no_match'; page?: number; pageSize?: number }) =>
    api.get<MediaScrapeIssuePage>('/media/scrape-issues', {
      params: {
        library_id: options.libraryID || undefined,
        status: options.status || undefined,
        page: options.page ?? 1,
        page_size: options.pageSize ?? 30,
      },
    }).then((r) => r.data),

  retryScrape: (id: string) => api.post<{ status: string }>(`/media/${id}/scrape`).then((r) => r.data),

  enrichDouban: (id: string) =>
    api.post<{ status: 'complete' | 'degraded' }>(`/media/${id}/douban-enrichment`, null, { timeout: LONG_REQUEST_TIMEOUT }).then((r) => r.data),

  delete: (id: string) => api.delete(`/media/${id}`).then((r) => r.data),

  listVersions: (id: string) => api.get<Media[]>(`/media/${id}/versions`).then((r) => r.data),

  listCredits: (id: string) =>
    api.get<{ items: MediaCredit[] }>(`/media/${id}/credits`).then((r) => r.data.items ?? []),

  ensureProbe: (id: string) =>
    api.post<Media>(`/media/${id}/probe/ensure`, null, { timeout: LONG_REQUEST_TIMEOUT }).then((r) => r.data),

  updateMetadata: (id: string, payload: MediaMetadataUpdate) =>
    api.patch<Media>(`/media/${id}/metadata`, payload, { timeout: LONG_REQUEST_TIMEOUT }).then((r) => r.data),

  manualScrapeSearch: (id: string, params: { query: string; provider?: string; media_type?: string }) =>
    api
      .get<{ items: ManualScrapeCandidate[] }>(`/media/${id}/scrape/search`, { params })
      .then((r) => r.data.items),

  applyManualScrape: (id: string, match: ManualScrapeCandidate, options?: ManualScrapeApplyOptions) =>
    api
      .post<Media>(
        `/media/${id}/scrape/apply`,
        episodeImageOption(options) === undefined ? match : { ...match, episode_images: episodeImageOption(options) },
        { timeout: LONG_REQUEST_TIMEOUT },
      )
      .then((r) => r.data),

  applyManualScrapeBatch: (mediaIDs: string[], match: ManualScrapeCandidate, options?: ManualScrapeApplyOptions) =>
    api
      .post<{ applied: number; errors?: string[] }>(
        '/media/scrape/apply',
        { media_ids: mediaIDs, match, episode_images: episodeImageOption(options) },
        { timeout: BATCH_REQUEST_TIMEOUT },
      )
      .then((r) => r.data),
}

function episodeImageOption(options?: ScrapeOptions): boolean | undefined {
  return options?.episode_images ?? options?.episode_artwork
}
