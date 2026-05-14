import { useEffect, useState } from 'react'

import { mediaAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

// Landing page: shows the most recently added media items across every
// library. Once the scraper / continue-watching subsystems land we will
// fold in additional rows here.
export function HomePage() {
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    mediaAPI
      .search('', 24)
      .then((d) => setItems(d.items))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="space-y-8">
      <header>
        <h1 className="font-display text-3xl font-bold text-white">最近添加</h1>
        <p className="text-sm text-slate-400">最新被扫描入库的媒体内容</p>
      </header>

      {loading && <p className="text-slate-500">加载中…</p>}
      {!loading && items.length === 0 && (
        <div className="glass-panel">
          <p className="text-slate-300">
            还没有任何媒体。前往 <span className="text-primary-400">管理后台</span>{' '}
            创建媒体库,然后触发一次扫描。
          </p>
        </div>
      )}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </div>
    </div>
  )
}
