import { api } from './client'

export interface BackgroundTask {
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
  key: string
  name: string
  description: string
  trigger: string
  schedule?: string
  current_state: 'idle' | 'running'
  next_run?: string
  action?: 'scheduler' | 'people_backfill'
  latest?: BackgroundTask
}

export interface TaskHistory {
  items: BackgroundTask[]
  page: number
  page_size: number
  total: number
}

export interface TaskLog {
  content: string
  truncated: boolean
}

export const tasksAPI = {
  snapshot: (page = 1, pageSize = 30) =>
    api.get<TasksSnapshot>('/tasks', { params: { page, page_size: pageSize } }).then((r) => r.data),
  log: (id: string) => api.get<TaskLog>(`/tasks/${id}/log`).then((r) => r.data),
  history: (key: string, page = 1, pageSize = 20) =>
    api.get<TaskHistory>(`/tasks/definitions/${key}/executions`, { params: { page, page_size: pageSize } }).then((r) => r.data),
  run: (key: string) => api.post<{ status: string }>(`/tasks/definitions/${key}/run`).then((r) => r.data),
}
