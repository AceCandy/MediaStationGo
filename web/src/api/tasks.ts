import { api } from './client'

export interface ActiveTranscode {
  job_id: string
  media_id: string
  audio_stream_index: number
  encoder: string
  started_at: string
  playlist_ok: boolean
}

export interface BackgroundTask {
  id: string
  kind: string
  name: string
  status: 'running' | 'completed' | 'failed'
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
  transcodes: ActiveTranscode[]
  background_tasks?: BackgroundTaskSnapshot
}

export const tasksAPI = {
  snapshot: () => api.get<TasksSnapshot>('/tasks').then((r) => r.data),
}
