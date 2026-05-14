import { Navigate, Route, Routes } from 'react-router-dom'

import { Layout } from './components/Layout'
import { RequireAuth, RequireAdmin } from './components/RequireAuth'
import { AdminPage } from './pages/AdminPage'
import { HomePage } from './pages/HomePage'
import { LibraryPage } from './pages/LibraryPage'
import { LoginPage } from './pages/LoginPage'
import { MediaDetailPage } from './pages/MediaDetailPage'
import { PlayerPage } from './pages/PlayerPage'
import { SearchPage } from './pages/SearchPage'

// Top-level route table.
//
// Public:
//   /login
//
// Authenticated:
//   /                      → home (continue watching + recently added)
//   /library/:id           → library content grid
//   /search                → keyword search
//   /media/:id             → media detail
//   /play/:id              → fullscreen player
//
// Admin only:
//   /admin                 → users / settings / activity log
export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route index element={<HomePage />} />
        <Route path="library/:id" element={<LibraryPage />} />
        <Route path="search" element={<SearchPage />} />
        <Route path="media/:id" element={<MediaDetailPage />} />
        <Route path="play/:id" element={<PlayerPage />} />
        <Route
          path="admin"
          element={
            <RequireAdmin>
              <AdminPage />
            </RequireAdmin>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
