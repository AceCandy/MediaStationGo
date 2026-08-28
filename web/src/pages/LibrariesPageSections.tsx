import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowRight, Film, Library as LibraryIcon, Music, PlayCircle, Tv } from 'lucide-react'

import { MediaCard } from '../components/MediaCard'
import { seriesCardLink } from '../utils/groupSeries'
import { libraryDisplayPath } from './libraryDisplayModel'
import type { LibraryPreview } from './librariesPageModel'

const TYPE_ICONS: Record<string, ReactNode> = {
  movie: <Film size={18} />,
  tv: <Tv size={18} />,
  anime: <PlayCircle size={18} />,
  variety: <Tv size={18} />,
  music: <Music size={18} />,
  adult: <Film size={18} />,
  nfo_movie: <Film size={18} />,
  nfo_tv: <Tv size={18} />,
}

const TYPE_LABELS: Record<string, string> = {
  movie: '电影',
  tv: '剧集',
  anime: '动漫',
  variety: '综艺',
  music: '音乐',
  adult: '成人',
  nfo_movie: '非常规电影',
  nfo_tv: '非常规剧集',
}

export function LibrariesHeader({
  isAdmin,
  previewCount,
  total,
}: {
  isAdmin: boolean
  previewCount: number
  total: number
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div>
        <h1 className="font-display text-3xl font-bold text-ink-600">媒体库</h1>
        <p className="mt-1 text-sm text-ink-50">
          共 {previewCount} 个目录 · {total.toLocaleString()} 个条目。每个目录直接展示最新入库内容。
        </p>
      </div>
      {isAdmin && (
        <div>
          <Link to="/admin/media" className="btn-outline">
            管理媒体库
            <ArrowRight size={14} />
          </Link>
        </div>
      )}
    </div>
  )
}

export function LibrariesEmptyState() {
  return (
    <div className="flex flex-col items-center justify-center rounded-3xl border border-dashed border-sand-200 bg-white py-24 text-center">
      <LibraryIcon className="mb-4 h-12 w-12 text-gray-400" />
      <p className="text-sm text-ink-50">暂无媒体库，请到管理后台添加目录。</p>
    </div>
  )
}

export function LibrariesContent({ previews }: { previews: LibraryPreview[] }) {
  return (
    <section className="space-y-6">
      {previews.map((preview, index) => (
        <motion.div
          key={preview.library.id}
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: index * 0.03 }}
        >
          <LibraryShelf preview={preview} />
        </motion.div>
      ))}
    </section>
  )
}

function LibraryShelf({ preview }: { preview: LibraryPreview }) {
  const library = preview.library
  const cards = preview.cards.slice(0, 10)
  const displayPath = libraryDisplayPath(library.path)

  return (
    <section className="rounded-[1.7rem] border border-sand-200 bg-white/75 p-4 shadow-card">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <div className="mb-1 inline-flex items-center gap-2 rounded-full bg-brand-50 px-2.5 py-1 text-[11px] font-bold text-brand-700">
            {TYPE_ICONS[library.type] ?? <LibraryIcon size={14} />}
            {TYPE_LABELS[library.type] ?? library.type}
          </div>
          <h2 className="truncate font-display text-2xl font-black">
            <Link to={`/library/${library.id}`} className="text-ink-600 hover:text-brand-600">
              {library.name}
            </Link>
          </h2>
          <p className="mt-1 line-clamp-1 break-all text-xs text-ink-50">
            <span title={library.path}>{displayPath}</span> · {preview.total.toLocaleString()} 个条目 · 最新 {cards.length} 部
          </p>
        </div>
        <Link to={`/library/${library.id}`} className="btn-outline shrink-0">
          浏览全部
          <ArrowRight size={14} />
        </Link>
      </div>

      {cards.length > 0 ? (
        <div className="flex gap-4 overflow-x-auto pb-2 pr-1">
          {cards.map((card, index) => (
            <div key={card.key} className="w-[9.5rem] shrink-0 lg:w-[10rem] 2xl:w-[10.5rem]">
              <MediaCard
                media={card.rep}
                count={card.count}
                linkTo={seriesCardLink(card)}
                staggerIndex={index}
              />
            </div>
          ))}
        </div>
      ) : (
        <div className="rounded-2xl border border-dashed border-sand-200 bg-white px-6 py-10 text-center text-sm text-ink-50">
          该目录暂无可展示内容，扫描媒体库后会出现在这里。
        </div>
      )}
    </section>
  )
}
