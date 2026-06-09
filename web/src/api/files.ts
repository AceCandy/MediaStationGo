import { api } from './client'

export interface FileEntry {
  name: string
  path: string
  is_dir: boolean
  size: number
  modified: number
  ext?: string
}

export interface FileListing {
  path: string
  parent?: string
  roots?: { label: string; path: string }[]
  entries: FileEntry[] | null
}

export const filesAPI = {
  list: (path = '', max = 1000, recursive = false) =>
    api
      .get<FileListing>('/files', { params: { path, max, recursive } })
      .then((r) => r.data),

  createFolder: (path: string, name: string) =>
    api.post<{ path: string }>('/files/folders', { path, name }).then((r) => r.data),

  rename: (path: string, name: string) =>
    api.put<{ path: string }>('/files/rename', { path, name }).then((r) => r.data),

  remove: (path: string) =>
    api.delete<{ removed: boolean }>('/files', { params: { path } }).then((r) => r.data),

  transfer: (sourcePath: string, destPath: string, transferMode = 'copy') =>
    api
      .post<{ path: string }>('/files/transfer', {
        source_path: sourcePath,
        dest_path: destPath,
        transfer_mode: transferMode,
      })
      .then((r) => r.data),
}
