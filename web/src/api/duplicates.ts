import { api } from './client'

export interface DuplicateMedia {
  id: string
  title: string
  path: string
  size_bytes: number
  library_name?: string
}

export interface DuplicateGroup {
  hash: string
  primary: DuplicateMedia
  duplicates: DuplicateMedia[]
}

export interface DuplicateReport {
  total_scanned: number
  groups_found: number
  items_marked: number
  missing_removed?: number
  groups: DuplicateGroup[]
}

export const duplicatesAPI = {
  list: (libraryID = '') =>
    api
      .get<DuplicateReport>('/duplicates', {
        params: libraryID ? { library_id: libraryID } : undefined,
      })
      .then((r) => ({ ...r.data, groups: r.data.groups ?? [] })),
  scan: (libraryID = '') =>
    api
      .post<DuplicateReport>('/duplicates/scan', null, {
        params: libraryID ? { library_id: libraryID } : undefined,
      })
      .then((r) => ({ ...r.data, groups: r.data.groups ?? [] })),
  unmark: (libraryID = '') =>
    api
      .post<{ unmarked: number }>('/duplicates/unmark', null, {
        params: libraryID ? { library_id: libraryID } : undefined,
      })
      .then((r) => r.data),
}
