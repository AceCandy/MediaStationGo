import { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { AnimatePresence, motion } from 'framer-motion'
import { Library as LibraryIcon, Search, Sparkles } from 'lucide-react'
import clsx from 'clsx'

import { imageURL } from '../api/client'
import { seriesCardLink, type SeriesCard } from '../utils/groupSeries'

type LayoutSearchBoxProps = {
  aiOn: boolean
  aiAvailable: boolean
  onToggleAI: () => void
  query: string
  focused: boolean
  loading: boolean
  error: string
  cards: SeriesCard[]
  total: number
  onQueryChange: (value: string) => void
  onFocusedChange: (focused: boolean) => void
  onSubmit: (event: FormEvent) => void
}

export function LayoutSearchBox({
  aiOn,
  aiAvailable,
  onToggleAI,
  query,
  focused,
  loading,
  error,
  cards,
  total,
  onQueryChange,
  onFocusedChange,
  onSubmit,
}: LayoutSearchBoxProps) {
  const trimmedQuery = query.trim()

  return (
    <form onSubmit={onSubmit} className="relative min-w-0 w-full">
      <span className={clsx(
        'absolute left-0 top-1/2 -translate-y-1/2 transition-colors duration-200',
        focused ? 'text-brand-500' : 'text-[var(--app-muted)]',
      )}>
        <button type="submit" aria-label="提交搜索" className="flex min-h-11 min-w-11 items-center justify-center rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"><Search size={16} /></button>
      </span>
      <input
        type="text"
        name="global-search"
        aria-label="搜索媒体"
        autoComplete="off"
        value={query}
        onChange={(event) => onQueryChange(event.target.value)}
        onMouseDown={() => onFocusedChange(true)}
        onClick={() => onFocusedChange(true)}
        onFocus={() => onFocusedChange(true)}
        onBlur={() => window.setTimeout(() => onFocusedChange(false), 120)}
        placeholder={aiOn ? '描述想找的影片…' : '搜索电影、电视剧、演员…'}
        className="w-full rounded-full border border-[var(--app-border)] bg-[var(--app-control-bg)] py-2.5 pl-11 pr-12 text-sm text-[var(--app-text)] placeholder:text-[var(--app-muted)] outline-none transition-all duration-300 focus:border-[var(--app-accent-border)] focus:bg-[var(--app-panel)] focus:ring-4 focus:ring-brand-500/15"
      />
      {aiAvailable && (
        <button
          type="button"
          aria-label={aiOn ? '关闭智能搜索' : '开启智能搜索'}
          aria-pressed={aiOn}
          title={aiOn ? '智能搜索已开启，点击关闭' : '开启智能搜索'}
          onClick={onToggleAI}
          className={clsx('absolute right-0 top-1/2 flex min-h-11 min-w-11 -translate-y-1/2 items-center justify-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500', aiOn ? 'bg-[var(--app-brand-soft)] text-brand-500' : 'text-[var(--app-muted)] hover:text-brand-500')}
        >
          <Sparkles size={18} aria-hidden="true" />
        </button>
      )}
      <AnimatePresence>
        {focused && trimmedQuery && !aiOn && (
          <motion.div
            initial={{ opacity: 0, y: 8, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 6, scale: 0.98 }}
            transition={{ duration: 0.14 }}
            onMouseDown={(event) => event.preventDefault()}
            className="absolute left-0 right-0 top-full z-50 mt-3 overflow-hidden rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] shadow-2xl"
          >
            <div className="max-h-[420px] overflow-y-auto p-2">
              {loading && (
                <div className="flex items-center gap-2 px-3 py-4 text-sm text-[var(--app-muted)]">
                  <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-brand-500 border-t-transparent" />
                  搜索中…
                </div>
              )}
              {!loading && error && (
                <div className="px-3 py-4 text-sm text-red-500">{error}</div>
              )}
              {!loading && !error && cards.length === 0 && (
                <div className="px-3 py-4 text-sm text-[var(--app-muted)]">没有找到匹配的本地媒体</div>
              )}
              {!loading && !error && cards.length > 0 && (
                <div className="space-y-1">
                  {cards.map((card) => (
                    <SearchResultItem
                      key={card.key}
                      card={card}
                      onClick={() => onFocusedChange(false)}
                    />
                  ))}
                </div>
              )}
            </div>
            <Link
              to={`/search?q=${encodeURIComponent(trimmedQuery)}`}
              onClick={() => onFocusedChange(false)}
              className="flex items-center justify-between border-t border-[var(--app-border)] px-4 py-3 text-sm font-semibold text-brand-500 hover:bg-[var(--app-hover)]"
            >
              <span>查看全部搜索结果</span>
              <span className="text-xs text-[var(--app-muted)]">
                {total > 0 ? `${total} 个条目` : ''}
              </span>
            </Link>
          </motion.div>
        )}
      </AnimatePresence>
    </form>
  )
}

function SearchResultItem({ card, onClick }: { card: SeriesCard; onClick: () => void }) {
  return (
    <Link
      to={seriesCardLink(card)}
      onClick={onClick}
      className="flex items-center gap-3 rounded-xl px-2.5 py-2 transition-colors hover:bg-[var(--app-hover)]"
    >
      <div className="h-14 w-10 shrink-0 overflow-hidden rounded-lg bg-[var(--app-panel-soft)]">
        {card.rep.poster_url ? (
          <img
            src={imageURL(card.rep.poster_url, card.rep.updated_at)}
            alt={card.rep.title}
            loading="lazy"
            decoding="async"
            className="h-full w-full object-cover"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-[var(--app-muted)]">
            <LibraryIcon size={16} />
          </div>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-semibold text-[var(--app-text)]">
          {card.rep.title || card.rep.original_name || '未命名媒体'}
        </div>
        <div className="mt-1 flex items-center gap-2 text-[11px] text-[var(--app-muted)]">
          {card.rep.year ? <span>{card.rep.year}</span> : null}
          <span>{card.count > 1 ? `${card.count} 集/条目` : '单条媒体'}</span>
          {card.rep.width ? <span>{card.rep.width}x{card.rep.height}</span> : null}
        </div>
      </div>
    </Link>
  )
}
