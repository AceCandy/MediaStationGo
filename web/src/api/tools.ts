import { api } from './client'

export const toolsAPI = {
  notifyTest: (title: string, body: string) =>
    api
      .post<{ message: string }>('/admin/notify/test', { title, body })
      .then((r) => r.data),
}
