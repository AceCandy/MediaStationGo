import { api } from './client'

export type GenerateSTRMInput = {
  library_id: string
  output_dir?: string
  base_url?: string
  enabled?: boolean
  overwrite?: boolean
  include_local?: boolean
}

export type GenerateSTRMResult = {
  library_id: string
  output_dir: string
  generated: number
  updated: number
  skipped: number
  errors?: string[]
  items?: Array<{
    media_id: string
    title: string
    file_path: string
    url?: string
    action: string
    reason?: string
  }>
}

export const strmAPI = {
  set: (mediaID: string, url: string) =>
    api.put(`/media/${mediaID}/strm`, { url }).then((r) => r.data),
  clear: (mediaID: string) => api.delete(`/media/${mediaID}/strm`).then((r) => r.data),
  importURL: (libraryID: string, title: string, url: string) =>
    api.post('/strm/import', { library_id: libraryID, title, url }).then((r) => r.data),
  generate: (input: GenerateSTRMInput) =>
    api.post<GenerateSTRMResult>('/strm/generate', input).then((r) => r.data),
}
