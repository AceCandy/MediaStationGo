import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowRight, Film, Library as LibraryIcon, Music, PlayCircle, Tv } from 'lucide-react'

import { imageURL } from '../api/client'
import { libraryAPI } from '../api/library'
import type { Library, Media } from '../types'

type LibraryPreview = {
  library: Library
  items: Media[]
  total: number
}

const TYPE_ICONS: Record<string, ReactNode> = {
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

const TYPE_DESCRIPTIONS: Record<string, string> = {
  movie: '电影、动画电影、纪录片电影',
  tv: '电视剧、短剧、纪录片剧集',
  anime: '番剧、动画、OVA 与剧场版',
  variety: '综艺、真人秀、晚会与节目合集',
  music: '音乐、MV、演唱会与音频内容',
}

export function LibrariesPage() {
  const [previews, setPreviews] = useState<LibraryPreview[]>([])
  const [loading, setLoading] = useState(true)
  const [activeType, setActiveType] = useState('all')

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      try {
        const libs = await libraryAPI.list()
        const rows = await Promise.all(libs.map(async (library) => {
          try {
            const page = await libraryAPI.listMedia(library.id, 1, 16)
            return { library, items: page.items, total: page.total } satisfies LibraryPreview
          } catch {
            return { library, items: [], total: 0 } satisfies LibraryPreview
          }
        }))
        if (!cancelled) setPreviews(rows)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [])

  const categories = useMemo(() => {
    const map = new Map<string, { type: string; libraries: number; total: number }>()
    for (const preview of previews) {
      const type = preview.library.type || 'other'
      const current = map.get(type) ?? { type, libraries: 0, total: 0 }
      current.libraries += 1
      current.total += preview.total
      map.set(type, current)
    }
    return Array.from(map.values()).sort((a, b) => b.total - a.total)
  }, [previews])

  const filtered = activeType === 'all'
    ? previews
    : previews.filter((preview) => preview.library.type === activeType)

  if (loading) {
    return <p className="px-2 py-8 text-sm text-sand-500">媒体库加载中…</p>
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">媒体库</h1>
          <p className="mt-1 text-sm text-ink-50">按影视分类浏览所有目录；每个文件夹会自动使用库内海报生成封面。</p>
        </div>
        <Link to="/admin" className="btn-outline">
          管理媒体库
          <ArrowRight size={14} />
        </Link>
      </div>

      {previews.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-3xl border border-dashed border-sand-200 bg-white py-24 text-center">
          <LibraryIcon className="mb-4 h-12 w-12 text-gray-400" />
          <p className="text-sm text-ink-50">暂无媒体库，请到管理后台添加目录。</p>
        </div>
      ) : (
        <>
          <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
            <CategoryCard
              active={activeType === 'all'}
              icon={<LibraryIcon size={20} />}
              label="全部媒体"
              description="电影、剧集、番剧、综艺统一视图"
              total={previews.reduce((sum, preview) => sum + preview.total, 0)}
              libraries={previews.length}
              onClick={() => setActiveType('all')}
            />
            {categories.map((category) => (
              <CategoryCard
                key={category.type}
                active={activeType === category.type}
                icon={TYPE_ICONS[category.type] ?? <LibraryIcon size={20} />}
                label={TYPE_LABELS[category.type] ?? category.type}
                description={TYPE_DESCRIPTIONS[category.type] ?? '自定义媒体分类'}
                total={category.total}
                libraries={category.libraries}
                onClick={() => setActiveType(category.type)}
              />
            ))}
          </section>

          <section className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
            {filtered.map((preview, index) => (
              <motion.div
                key={preview.library.id}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: index * 0.03 }}
              >
                <LibraryCard preview={preview} />
              </motion.div>
            ))}
          </section>
        </>
      )}
    </div>
  )
}

function CategoryCard({
  active,
  icon,
  label,
  description,
  total,
  libraries,
  onClick,
}: {
  active: boolean
  icon: ReactNode
  label: string
  description: string
  total: number
  libraries: number
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        'rounded-3xl border p-4 text-left transition-all ' +
        (active
          ? 'border-brand-300 bg-brand-50 shadow-card'
          : 'border-sand-200 bg-white hover:-translate-y-0.5 hover:border-brand-200 hover:shadow-card')
      }
    >
      <div className="mb-4 flex items-center justify-between">
        <span className="flex h-11 w-11 items-center justify-center rounded-2xl bg-white text-brand-600 shadow-sm">
          {icon}
        </span>
        <span className="rounded-full bg-white px-2.5 py-1 text-[11px] font-bold text-sand-600">
          {libraries} 库
        </span>
      </div>
      <h2 className="font-display text-lg font-bold text-ink-600">{label}</h2>
      <p className="mt-1 line-clamp-2 text-xs text-ink-50">{description}</p>
      <p className="mt-4 text-xs font-bold text-brand-600">{total.toLocaleString()} 个条目</p>
    </button>
  )
}

function LibraryCard({ preview }: { preview: LibraryPreview }) {
  const library = preview.library
  const artwork = preview.items
    .map((item) => item.poster_url || item.backdrop_url)
    .filter(Boolean)
    .slice(0, 6) as string[]

  return (
    <Link
      to={`/library/${library.id}`}
      className="group block overflow-hidden rounded-[1.7rem] border border-sand-200 bg-white shadow-card transition-all hover:-translate-y-0.5 hover:border-brand-200 hover:shadow-card-hover"
    >
      <div className="relative aspect-[16/9] overflow-hidden bg-[linear-gradient(135deg,#fff7ed,#f8fafc)]">
        {artwork.length > 0 ? (
          <div className="grid h-full grid-cols-3 gap-1 p-1.5">
            {artwork.map((src, index) => (
              <img
                key={`${src}-${index}`}
                src={imageURL(src)}
                alt=""
                loading="lazy"
                referrerPolicy="no-referrer"
                className={`h-full w-full rounded-xl object-cover ${index === 0 ? 'col-span-2 row-span-2' : ''}`}
                onError={(event) => { event.currentTarget.style.visibility = 'hidden' }}
              />
            ))}
          </div>
        ) : (
          <div className="flex h-full items-center justify-center text-brand-500">
            {TYPE_ICONS[library.type] ?? <LibraryIcon size={42} />}
          </div>
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-ink-600/70 via-ink-600/10 to-transparent" />
        <div className="absolute left-4 top-4 rounded-full bg-white/90 px-3 py-1 text-xs font-bold text-ink-600 shadow-sm backdrop-blur">
          {TYPE_LABELS[library.type] ?? library.type}
        </div>
        <div className="absolute bottom-4 left-4 right-4">
          <h2 className="font-display text-2xl font-black text-white drop-shadow-sm group-hover:text-brand-100">
            {library.name}
          </h2>
          <p className="mt-1 text-xs font-semibold text-white/85">{preview.total.toLocaleString()} 个媒体条目</p>
        </div>
      </div>
      <div className="flex items-center justify-between gap-3 p-5">
        <p className="line-clamp-1 break-all text-xs text-ink-50">{library.path}</p>
        <span className="shrink-0 rounded-xl bg-sand-100 px-3 py-1 text-xs font-bold text-sand-600 transition-colors group-hover:bg-brand-100 group-hover:text-brand-700">
          进入
        </span>
      </div>
    </Link>
  )
}
