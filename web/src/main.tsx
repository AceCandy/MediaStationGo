import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { Toaster } from 'react-hot-toast'

import App from './App'
import { GlobalEvents } from './components/GlobalEvents'
import { initializeThemeMode } from './components/useThemeMode'
import './index.css'

initializeThemeMode()

if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/artwork-cache-sw.js').catch(() => undefined)
  })
}

// Application root: BrowserRouter + global toast container.
ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <BrowserRouter>
      <GlobalEvents />
      <App />
      <Toaster
        position="top-right"
        toastOptions={{
          style: {
            background: 'var(--app-glass)',
            color: 'var(--app-text)',
            border: '1px solid var(--app-glass-border)',
            backdropFilter: 'blur(16px)',
            WebkitBackdropFilter: 'blur(16px)',
            boxShadow: '0 12px 40px var(--app-shadow)',
            borderRadius: '14px',
            fontSize: '13px',
            fontWeight: 600,
          },
          success: { iconTheme: { primary: '#8b5cf6', secondary: '#ffffff' } },
          error: { iconTheme: { primary: '#ef4444', secondary: '#ffffff' } },
        }}
      />
    </BrowserRouter>
  </React.StrictMode>,
)
