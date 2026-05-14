import { ChangeEvent, useEffect, useState } from 'react'

import { mediaAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import type { Media } from '../types'

// Search-as-you-type with a 300ms debounce.
export function SearchPage() {
  const [q, setQ] = useState('')
  const [items, setItems] = useState<Media[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const t = setTimeout(() => {
      setLoading(true)
      mediaAPI
        .search(q, 60)
        .then((d) => setItems(d.items))
        .finally(() => setLoading(false))
    }, 300)
    return () => clearTimeout(t)
  }, [q])

  return (
    <div className="space-y-6">
      <h1 className="font-display text-3xl font-bold text-white">搜索</h1>
      <input
        autoFocus
        className="input-base"
        placeholder="按标题搜索…"
        value={q}
        onChange={(e: ChangeEvent<HTMLInputElement>) => setQ(e.target.value)}
      />
      {loading && <p className="text-slate-500">搜索中…</p>}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((m) => (
          <MediaCard key={m.id} media={m} />
        ))}
      </div>
    </div>
  )
}
