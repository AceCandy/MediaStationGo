import { api } from './client'

export interface DownloadConfig { root: string; temporary_dir: string; output_dir: string; concurrency: number; verification_concurrency: number; full_verification: boolean; hardware_verification: boolean; priority: string }
export interface HongGuoDownload {
  id: string; source_id: string; title: string; episode: number; relative_path: string
  status: 'queued' | 'downloading' | 'waiting_verify' | 'verifying' | 'publishing' | 'completed' | 'failed' | 'cancelled'
  bytes: number; total_bytes: number; attempts: number; error: string
  source?: string; quality?: number; width?: number; height?: number; codec?: string
  source_errors?: Record<string, string> | null
}
export type HongGuoDownloadWork = { source_id: string; title: string; total: number } & Record<HongGuoDownload['status'], number>

const base = '/catalogs/hongguo/downloads'
export const hongguoDownloadsAPI = {
  config: (signal?: AbortSignal) => api.get<DownloadConfig>(`${base}/config`, { signal }).then((r) => r.data),
  save: (config: Pick<DownloadConfig, 'root' | 'concurrency' | 'verification_concurrency' | 'full_verification' | 'hardware_verification' | 'priority'>) => api.put<DownloadConfig>(`${base}/config`, config).then((r) => r.data),
  list: (page: number, signal?: AbortSignal) => api.get<{ items: HongGuoDownload[]; total: number }>(base, { params: { page }, signal }).then((r) => r.data),
  works: (page: number, failedOnly: boolean, signal?: AbortSignal) => api.get<{ items: HongGuoDownloadWork[]; total: number }>(`${base}/works`, { params: { page, failed_only: failedOnly || undefined }, signal }).then((r) => r.data),
  episodes: (source: string, page: number, signal?: AbortSignal) => api.get<{ items: HongGuoDownload[]; total: number }>(`${base}/works/${encodeURIComponent(source)}/episodes`, { params: { page }, signal }).then((r) => r.data),
  retryWork: (source: string) => api.post<{ added: number; skipped: number }>(`${base}/works/${encodeURIComponent(source)}/retry`).then((r) => r.data),
  enqueue: (source_id: string) => api.post<{ added: number }>(base, { source_id }).then((r) => r.data),
  action: (id: string, action: 'cancel' | 'retry') => api.post(`${base}/${encodeURIComponent(id)}/${action}`),
}
