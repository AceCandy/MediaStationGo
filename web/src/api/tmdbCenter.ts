import { api } from './client'

export interface TMDbSubject {
  id: string; kind: 'movie' | 'series' | 'season' | 'episode'; tmdb_id: number; parent_id?: string; number?: number
  title: string; original_name: string; overview: string; release_date: string; rating?: number; vote_count: number
  status: string; quality: number; fetched_at?: string; fields: TMDbFields; poster_url: string; backdrop_url: string
}
export interface TMDbFields {
  runtime?: number; episode_run_time?: number[]; original_language?: string; genres?: { id: number; name: string }[]
  production_countries?: { iso_3166_1: string; name: string }[]; spoken_languages?: { iso_639_1: string; name: string }[]
  external_ids?: { imdb_id?: string }; imdb_id?: string; number_of_seasons?: number; number_of_episodes?: number
  last_air_date?: string; birthday?: string; deathday?: string; place_of_birth?: string; also_known_as?: string[]
}
export interface TMDbPerson {
  id: string; person_id: string; tmdb_id: number; name: string; original_name: string; biography: string; department: string
  quality: number; fetched_at?: string; fields: TMDbFields; profile_url: string
}
export interface TMDbJob { id: string; scope: string; language: string; status: string; last_success_at?: string; next_refresh_at?: string; next_retry_at?: string; error?: string }
export interface TMDbDetail { source_url?: string; parents?: TMDbSubject[]; subject?: TMDbSubject; person?: TMDbPerson; components: { component: string; language: string; fetched_at: string; origin: string; raw_json?: unknown }[]; jobs: TMDbJob[]; missing_scopes: string[] }
export interface TMDbCredit { id: string; category: string; character: string; job: string; department: string; person: TMDbPerson; subject: TMDbSubject }
export interface TMDbPage<T> { data: T[]; total: number; page: number; page_size: number }
export interface TMDbSearchRow { id: number; title?: string; name?: string; release_date?: string; first_air_date?: string }
const root = '/admin/tmdb'
export const tmdbCenterAPI = {
  subjects: (params: Record<string, string | number>, signal?: AbortSignal) => api.get<TMDbPage<TMDbSubject>>(`${root}/subjects`, { params, signal }).then(r => r.data),
  people: (params: Record<string, string | number>, signal?: AbortSignal) => api.get<TMDbPage<TMDbPerson>>(`${root}/people`, { params, signal }).then(r => r.data),
  detail: (id: string, person: boolean, signal?: AbortSignal) => api.get<{ data: TMDbDetail }>(`${root}/${person ? 'people' : 'subjects'}/${id}`, { signal }).then(r => r.data.data),
  children: (id: string, kind: string, page: number, signal?: AbortSignal) => api.get<TMDbPage<TMDbSubject>>(`${root}/subjects/${id}/${kind === 'series' ? 'seasons' : 'episodes'}`, { params: { page }, signal }).then(r => r.data),
  credits: (id: string, person: boolean, page: number, signal?: AbortSignal, filters?: { kind: string; category: string; order: string }) => api.get<TMDbPage<TMDbCredit>>(`${root}/${person ? 'people' : 'subjects'}/${id}/${person ? 'works' : 'credits'}`, { params: { page, ...filters }, signal }).then(r => r.data),
  ensure: (id: string, person: boolean, scope: string, signal?: AbortSignal) => api.post(`${root}/${person ? 'people' : 'subjects'}/${id}/ensure`, undefined, { params: { scope }, signal }),
  refresh: (id: string, person: boolean, scope: string, signal?: AbortSignal) => api.post<{ data: TMDbJob }>(`${root}/${person ? 'people' : 'subjects'}/${id}/refresh`, undefined, { params: { scope }, signal }).then(r => r.data.data),
  import: (kind: string, tmdb_id: number, signal?: AbortSignal) => api.post<{ data: { id: string } }>(`${root}/import`, { kind, tmdb_id }, { signal }).then(r => r.data.data),
  search: (kind: string, keyword: string, page: number, signal?: AbortSignal) => api.post<{ data: { results: TMDbSearchRow[]; total_pages: number } }>(`${root}/search`, { kind, keyword, page }, { signal }).then(r => r.data.data),
  retryImage: (kind: string, id: number, type: string, signal?: AbortSignal) => api.post(`${root}/images/${kind}/${id}/${type}/refresh`, undefined, { signal, responseType: 'blob' }),
}
