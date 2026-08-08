import { api } from './client'

// ─── TypeScript interfaces ──────────────────────────────────────────────
// Note: the canonical Site type lives in ../types/index.ts
// The backend uses json:"url" (not base_url) and json:"type" (not site_type).

export interface SiteSearchResult {
  site_name: string
  site_id: string
  title: string
  subtitle?: string
  detail_url?: string
  category?: string
  size: number
  seeders: number
  leechers: number
  free: boolean
}

export interface CreateSiteInput {
  name: string
  url: string
  type?: string
  auth_type?: string
  cookie?: string
  api_key?: string
  auth_header?: string
  enabled?: boolean
  is_default?: boolean
  extra?: string
}

// ─── API client ─────────────────────────────────────────────────────────

export const sitesAPI = {
  // List all sites
  list: () => api.get('/sites').then((r) => r.data),

  // Get single site with decrypted fields
  get: (id: string | number) => api.get(`/sites/${id}`).then((r) => r.data),

  // Create a new site
  create: (data: Record<string, unknown>) =>
    api.post('/sites', data).then((r) => r.data),

  // Update existing site
  update: (id: string | number, data: Record<string, unknown>) =>
    api.put(`/sites/${id}`, data).then((r) => r.data),

  // Delete a site
  remove: (id: string | number) =>
    api.delete(`/sites/${id}`).then((r) => r.data),

  // Test site connectivity
  test: (id: string | number) =>
    api.post(`/sites/${id}/test`).then((r) => r.data),

  // Get supported site types
  types: () => api.get('/sites/types').then((r) => r.data),

  // Get supported auth types
  authTypes: () => api.get('/sites/auth-types').then((r) => r.data),

  // Search across all sites
  search: (keyword: string) =>
    api
      .get('/sites/search', { params: { keyword } })
      .then((r) => r.data),
}
