import { api } from './client'

export interface LibraryUsage {
  library_id: string
  name: string
  type: string
  path: string
  movie_count: number
  series_count: number
  season_count: number
  episode_count: number
  total_bytes: number
}

export interface StorageBreakdown {
  total_bytes: number
  by_library: LibraryUsage[]
}

export const storageAPI = {
  breakdown: () => api.get<StorageBreakdown>('/storage').then((r) => r.data),
}
