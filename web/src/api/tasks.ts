import { api } from './client'

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
  background_tasks?: BackgroundTaskSnapshot
}

export const tasksAPI = {
  snapshot: () => api.get<TasksSnapshot>('/tasks').then((r) => r.data),
}
