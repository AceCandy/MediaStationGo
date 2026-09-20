import { api } from './client'

export type TaskSystem = 'common' | 'catalog' | 'hongguo' | 'nfo'

export interface BackgroundTask {
	system: TaskSystem
  id: string
  kind: string
  trigger: 'manual' | 'scheduled' | 'event'
  name: string
  status: 'running' | 'completed' | 'failed' | 'interrupted'
  stage?: string
  source_path?: string
  dest_path?: string
  message?: string
  error?: string
  details?: string[]
  metrics?: Record<string, number>
  started_at: string
  updated_at: string
  finished_at?: string
}

export interface BackgroundTaskSnapshot {
  active: BackgroundTask[]
  recent: BackgroundTask[]
}

export interface TasksSnapshot {
  background_tasks?: BackgroundTaskSnapshot
  items: BackgroundTask[]
  page: number
  page_size: number
  total: number
  definitions: TaskDefinition[]
}

export interface TaskDefinition {
	system: TaskSystem
  key: string
  name: string
  description: string
  trigger: string
  schedule?: string
  current_state: 'idle' | 'running'
  next_run?: string
  action?: 'scheduler' | 'people_backfill' | 'probe_backfill' | 'media_scrape' | 'tmdb_snapshot_backfill' | 'series_local_correction'
  current?: BackgroundTask
  latest?: BackgroundTask
  schedule_config?: TaskScheduleConfig
}

export interface TaskScheduleConfig {
  count?: number
  enabled: boolean
  interval_seconds: number
  min_interval_seconds: number
  max_interval_seconds: number
}

export interface TaskHistory {
  items: BackgroundTask[]
  page: number
  page_size: number
  total: number
}

export interface TaskLog {
  date: string
  dates: string[]
  content: string
  truncated: boolean
}

export const tasksAPI = {
	recheckFiles: (id: string, page = 1, signal?: AbortSignal) =>
		api.get<TMDbRecheckFilesPage>(`/tasks/definitions/tmdb_episode_metadata_recheck/pending/${encodeURIComponent(id)}/files`, { params: { page, page_size: 20 }, signal }).then((r) => r.data),
	rechecks: (status = '', page = 1, signal?: AbortSignal, keyword = '', pageSize = 20) =>
		api.get<TMDbRecheckPage>('/tasks/definitions/tmdb_episode_metadata_recheck/pending', { params: { view: 'items', status, keyword: keyword || undefined, page, page_size: pageSize }, signal }).then((r) => r.data),
	recheckSummary: (signal?: AbortSignal) =>
		api.get<TMDbRecheckSummary>('/tasks/definitions/tmdb_episode_metadata_recheck/pending', { params: { view: 'summary' }, signal }).then((r) => r.data),
  snapshot: (page = 1, pageSize = 30, system?: TaskSystem, signal?: AbortSignal) =>
    api.get<TasksSnapshot>('/tasks', { params: { page, page_size: pageSize, system }, signal }).then((r) => r.data),
  log: (key: string, date?: string) =>
    api.get<TaskLog>(`/tasks/definitions/${key}/log`, { params: date ? { date } : undefined }).then((r) => r.data),
  history: (key: string, page = 1, pageSize = 20, signal?: AbortSignal) =>
    api.get<TaskHistory>(`/tasks/definitions/${key}/executions`, { params: { page, page_size: pageSize }, signal }).then((r) => r.data),
  run: (key: string, options?: { limit?: number; library_id?: string; all_libraries?: boolean; count?: number }) =>
    api.post<{ status: string; count?: number; libraries?: number }>(`/tasks/definitions/${key}/run`, options).then((r) => r.data),
  updateSchedule: (key: string, enabled: boolean, intervalSeconds: number, count?: number) =>
    api.put<TaskDefinition>(`/tasks/definitions/${key}/schedule`, {
      enabled,
      interval_seconds: intervalSeconds,
      ...(count !== undefined ? { count } : {}),
    }).then((r) => r.data),
}

export interface TMDbRecheckPage {
  items: { metadata_id: string; title: string; kind: string; series_title: string; season_num: number; episode_num: number; status: string; due_at: string | null; attempts: number; last_error: string }[]
  total: number
  page: number
  page_size: number
}

export interface TMDbRecheckSummary {
  counts: Record<string, number>
  changes: number
}

export interface TMDbRecheckFilesPage {
  items: { media_id: string; path: string; library_id: string; can_preview: boolean }[]
  page: number
  has_more: boolean
}
