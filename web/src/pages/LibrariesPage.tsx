import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowRight, Film, FolderOpen, Library as LibraryIcon, Music, PlayCircle, Tv } from 'lucide-react'

import { imageURL } from '../api/client'
import { libraryAPI } from '../api/library'
import { MediaCard } from '../components/MediaCard'
import type { Library, Media } from '../types'
import { artworkScore, groupSeries, type SeriesCard } from '../utils/groupSeries'

type LibraryPreview = {
  library: Library
  items: Media[]
  total: number
}

type CategorySummary = {
  type: string
  libraries: number
  total: number
  items: Media[]
}

const TYPE_ICONS: Record<string, ReactNode> = {
  movie: <Film size={18} />,
  tv: <Tv size={18} />,
  anime: <PlayCircle size={18} />,
  variety: <Tv size={18} />,
  music: <Music size={18} />,
}

const TYPE_LABELS: Record<string, string> = {
  movie: '电影',
  tv: '剧集',
  anime: '动漫',
  variety: '综艺',
  music: '音乐',
}

const TYPE_DESCRIPTIONS: Record<string, string> = {
  movie: '电影 / 动画电影 / 纪录片',
  tv: '国产剧 / 欧美剧 / 日韩剧',
  anime: '国漫 / 日番 / OVA',
  variety: '综艺 / 真人秀 / 晚会',
  music: '音乐 / MV / 演唱会',
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
            const page = await libraryAPI.listMedia(library.id, 1, 120)
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
    const map = new Map<string, CategorySummary>()
    for (const preview of previews) {
      const type = preview.library.type || 'other'
      const current = map.get(type) ?? { type, libraries: 0, total: 0, items: [] }
      current.libraries += 1
      current.total += preview.total
      current.items.push(...preview.items)
      map.set(type, current)
    }
    return Array.from(map.values()).sort((a, b) => b.total - a.total)
  }, [previews])

  const filtered = activeType === 'all'
    ? previews
    : previews.filter((preview) => preview.library.type === activeType)

  const activeItems = useMemo(() => filtered.flatMap((preview) => preview.items), [filtered])
  const latestCards = useMemo(() => {
    return groupSeries(activeItems)
      .sort((a, b) => mediaTime(b.rep) - mediaTime(a.rep))
      .slice(0, 9)
  }, [activeItems])

  const activeLabel = activeType === 'all'
    ? '全部媒体'
    : TYPE_LABELS[activeType] ?? activeType

  if (loading) {
    return <p className="px-2 py-8 text-sm text-sand-500">媒体库加载中…</p>
  }

  return (
    <div className="space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">媒体库</h1>
          <p className="mt-1 text-sm text-ink-50">先看每类最新入库，再通过目录入口进入完整文件夹。</p>
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
          <section className="overflow-x-auto pb-2">
            <div className="flex min-w-full gap-3">
              <CategoryCard
                active={activeType === 'all'}
                icon={<LibraryIcon size={18} />}
                label="全部媒体"
                description="统一浏览"
                total={previews.reduce((sum, preview) => sum + preview.total, 0)}
                libraries={previews.length}
                onClick={() => setActiveType('all')}
              />
              {categories.map((category) => (
                <CategoryCard
                  key={category.type}
                  active={activeType === category.type}
                  icon={TYPE_ICONS[category.type] ?? <LibraryIcon size={18} />}
                  label={TYPE_LABELS[category.type] ?? category.type}
                  description={TYPE_DESCRIPTIONS[category.type] ?? '自定义分类'}
                  total={category.total}
                  libraries={category.libraries}
                  onClick={() => setActiveType(category.type)}
                />
              ))}
            </div>
          </section>

          <section className="space-y-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h2 className="font-display text-2xl font-bold text-ink-600">{activeLabel} · 最近更新</h2>
                <p className="text-sm text-ink-50">显示前 9 部最新入库或更新的影片 / 剧集合集。</p>
              </div>
              {filtered.length === 1 && (
                <Link to={`/library/${filtered[0].library.id}`} className="btn-outline shrink-0">
                  浏览全部
                  <ArrowRight size={14} />
                </Link>
              )}
            </div>

            {latestCards.length > 0 ? (
              <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 lg:grid-cols-6 2xl:grid-cols-9">
                {latestCards.map((card) => (
                  <MediaCard
                    key={card.key}
                    media={card.rep}
                    count={card.count}
                    linkTo={mediaCardLink(card)}
                  />
                ))}
              </div>
            ) : (
              <div className="rounded-3xl border border-dashed border-sand-200 bg-white px-6 py-12 text-center text-sm text-ink-50">
                当前分类还没有入库内容，扫描媒体库后会显示在这里。
              </div>
            )}
          </section>

          <section className="space-y-4">
            <div>
              <h2 className="font-display text-2xl font-bold text-ink-600">目录入口</h2>
              <p className="text-sm text-ink-50">按你添加的媒体库文件夹进入，适合继续按文件分类查找。</p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {filtered.map((preview, index) => (
                <motion.div
                  key={preview.library.id}
                  initial={{ opacity: 0, y: 12 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: index * 0.03 }}
                >
                  <LibraryFolderCard preview={preview} />
                </motion.div>
              ))}
            </div>
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
        'min-w-[9.5rem] shrink-0 rounded-2xl border p-3 text-left transition-all lg:w-[calc((100%-3.75rem)/6)] ' +
        (active
          ? 'border-brand-300 bg-brand-50 shadow-card'
          : 'border-sand-200 bg-white hover:-translate-y-0.5 hover:border-brand-200 hover:shadow-card')
      }
    >
      <div className="mb-3 flex items-center justify-between">
        <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-white text-brand-600 shadow-sm">
          {icon}
        </span>
        <span className="rounded-full bg-white px-2 py-0.5 text-[10px] font-bold text-sand-600">
          {libraries} 库
        </span>
      </div>
      <h2 className="font-display text-base font-bold text-ink-600">{label}</h2>
      <p className="mt-0.5 truncate text-[11px] text-ink-50">{description}</p>
      <p className="mt-2 text-xs font-bold text-brand-600">{total.toLocaleString()} 项</p>
    </button>
  )
}

function LibraryFolderCard({ preview }: { preview: LibraryPreview }) {
  const library = preview.library
  const artwork = groupSeries(preview.items)
    .sort((a, b) => artworkScore(b.rep) - artworkScore(a.rep) || mediaTime(b.rep) - mediaTime(a.rep))
    .map((card) => card.rep.poster_url || card.rep.backdrop_url)
    .filter(Boolean)
    .slice(0, 4) as string[]

  return (
    <Link
      to={`/library/${library.id}`}
      className="group flex overflow-hidden rounded-3xl border border-sand-200 bg-white p-3 shadow-card transition-all hover:-translate-y-0.5 hover:border-brand-200 hover:shadow-card-hover"
    >
      <div className="grid h-24 w-36 shrink-0 grid-cols-2 gap-1 overflow-hidden rounded-2xl bg-[linear-gradient(135deg,#fff7ed,#f8fafc)]">
        {artwork.length > 0 ? (
          artwork.map((src, index) => (
            <img
              key={`${src}-${index}`}
              src={imageURL(src)}
              alt=""
              loading="lazy"
              referrerPolicy="no-referrer"
              className="h-full w-full object-cover"
              onError={(event) => { event.currentTarget.style.visibility = 'hidden' }}
            />
          ))
        ) : (
          <div className="col-span-2 flex h-full items-center justify-center text-brand-500">
            {TYPE_ICONS[library.type] ?? <FolderOpen size={34} />}
          </div>
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col justify-between px-4 py-1">
        <div>
          <div className="mb-1 inline-flex rounded-full bg-sand-100 px-2 py-0.5 text-[10px] font-bold text-sand-600">
            {TYPE_LABELS[library.type] ?? library.type}
          </div>
          <h2 className="truncate font-display text-xl font-black text-ink-600 group-hover:text-brand-600">
            {library.name}
          </h2>
          <p className="mt-1 line-clamp-1 break-all text-xs text-ink-50">{library.path}</p>
        </div>
        <div className="flex items-center justify-between text-xs font-bold">
          <span className="text-sand-600">{preview.total.toLocaleString()} 个条目</span>
          <span className="text-brand-600">进入文件夹</span>
        </div>
      </div>
    </Link>
  )
}

function mediaCardLink(card: SeriesCard): string {
  if (card.count > 1) {
    return `/library/${card.rep.library_id}?series=${encodeURIComponent(card.key)}`
  }
  return `/media/${card.rep.id}`
}

function mediaTime(media: Media): number {
  return Date.parse(media.updated_at || media.created_at || '') || 0
}
