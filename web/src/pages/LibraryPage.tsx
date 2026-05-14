import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import toast from 'react-hot-toast'

import { libraryAPI } from '../api/library'
import type { Media } from '../types'
import { MediaCard } from '../components/MediaCard'
import { useAuthStore } from '../stores/auth'

// Browse a single library. Admins also get a "scan" button that triggers
// a fresh scan run on the backend.
export function LibraryPage() {
  const { id = '' } = useParams()
  const role = useAuthStore((s) => s.user?.role)
  const [items, setItems] = useState<Media[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)

  useEffect(() => {
    if (!id) return
    setLoading(true)
    libraryAPI
      .listMedia(id)
      .then((d) => {
        setItems(d.items)
        setTotal(d.total)
      })
      .finally(() => setLoading(false))
  }, [id])

  const handleScan = async () => {
    setScanning(true)
    try {
      const r = await libraryAPI.scan(id)
      toast.success(`扫描完成:新增 ${r.added} 个文件`)
      const d = await libraryAPI.listMedia(id)
      setItems(d.items)
      setTotal(d.total)
    } catch {
      toast.error('扫描失败')
    } finally {
      setScanning(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="font-display text-3xl font-bold text-white">
          媒体库 <span className="text-slate-500">({total})</span>
        </h1>
        {role === 'admin' && (
          <button onClick={handleScan} disabled={scanning} className="neon-button">
            {scanning ? '扫描中…' : '立即扫描'}
          </button>
        )}
      </div>

      {loading && <p className="text-slate-500">加载中…</p>}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </div>
    </div>
  )
}
