import { APIConfigsPanel } from '../components/APIConfigsPanel'
import { AdminLibraryPanel } from './AdminLibraryPanel'
import { AdminUsersPanel } from './AdminUsersPanel'

export function AdminMediaPage() {
  return <AdminLibraryPanel />
}

export function AdminUsersPage() {
  return <AdminUsersPanel />
}

export function AdminAPIsPage() {
  return <APIConfigsPanel />
}
