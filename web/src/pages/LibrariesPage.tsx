import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { Film, Library as LibraryIcon, Music, PlayCircle, Tv } from 'lucide-react'

import { libraryAPI } from '../api/library'
import type { Library } from '../types'

const TYPE_ICONS: Record<string, React.ReactNode> = {
  movie: <Film size={20} />,
  tv: <Tv size={20} />,
  anime: <PlayCircle size={20} />,
  variety: <Tv size={20} />,
  music: <Music size={20} />,
}

const TYPE_LABELS: Record<string, string> = {
  movie: '电影',
  tv: '剧集',
  anime: '动漫',
  variety: '综艺',
  music: '音乐',
}

export function LibrariesPage() {
  const [libraries, setLibraries] = useState<Library[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    libraryAPI.list().then(setLibraries).finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <p className="px-2 py-8 text-sm text-sand-500">媒体库加载中…</p>
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-3xl font-bold text-ink-600">媒体库</h1>
        <p className="mt-1 text-sm text-ink-50">所有电影、剧集、番剧、综艺分类统一在这里查看。</p>
      </div>

      {libraries.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-3xl border border-dashed border-sand-200 bg-white py-24 text-center">
          <LibraryIcon className="mb-4 h-12 w-12 text-gray-400" />
          <p className="text-sm text-ink-50">暂无媒体库，请到管理后台添加目录。</p>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {libraries.map((lib, index) => (
            <motion.div
              key={lib.id}
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: index * 0.03 }}
            >
              <Link
                to={`/library/${lib.id}`}
                className="group block rounded-3xl border border-sand-200 bg-white p-5 shadow-card transition-all hover:-translate-y-0.5 hover:border-brand-200 hover:shadow-card-hover"
              >
                <div className="mb-6 flex items-start justify-between gap-4">
                  <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-brand-50 text-brand-600">
                    {TYPE_ICONS[lib.type] ?? <LibraryIcon size={20} />}
                  </div>
                  <span className="rounded-full bg-sand-100 px-3 py-1 text-xs font-semibold text-sand-600">
                    {TYPE_LABELS[lib.type] ?? lib.type}
                  </span>
                </div>
                <h2 className="font-display text-xl font-bold text-ink-600 group-hover:text-brand-600">
                  {lib.name}
                </h2>
                <p className="mt-2 line-clamp-2 break-all text-xs text-ink-50">{lib.path}</p>
              </Link>
            </motion.div>
          ))}
        </div>
      )}
    </div>
  )
}
