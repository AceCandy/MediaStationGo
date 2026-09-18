import { Link } from 'react-router-dom'
import { motion } from 'framer-motion'
import { ArrowRight, Clock, Film, Play, Sparkles } from 'lucide-react'

import { imageURL } from '../api/client'
import { MediaCard } from '../components/MediaCard'
import type { HistoryItem } from '../types'
import type { Media } from '../types'
import type { SeriesCard } from '../utils/groupSeries'
import { mediaDetailLink, seriesCardLink } from '../utils/groupSeries'
import { episodePresentation } from './seriesDetailModel'

export function HomeLoadingState() {
  return (
    <div className="flex items-center justify-center py-48">
      <motion.div animate={{ opacity: [0.4, 1, 0.4] }} transition={{ repeat: Infinity, duration: 1.5 }} className="flex flex-col items-center gap-4">
        <div className="relative flex items-center justify-center">
          <div className="h-10 w-10 animate-spin rounded-full border-2 border-[var(--app-border)] border-t-[var(--app-active-bg)]" />
          <Film className="absolute h-4 w-4 text-brand-500" />
        </div>
        <span className="text-sm font-semibold uppercase tracking-widest text-[var(--app-muted)]">首页内容准备中…</span>
      </motion.div>
    </div>
  )
}

export function HomeEmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-32 text-center max-w-md mx-auto">
      <div className="mb-6 flex h-24 w-24 items-center justify-center rounded-3xl border border-[var(--app-border)] bg-[var(--app-panel-soft)] shadow-sm">
        <Film className="h-10 w-10 text-[var(--app-muted)]" />
      </div>
      <p className="text-xl font-bold text-[var(--app-text)]">您的家庭影视站暂无内容</p>
      <p className="mt-2 text-sm leading-relaxed text-[var(--app-muted)]">
        前往管理后台添加媒体目录，扫描后首页将展示本周力荐、继续观看和最近入库。
      </p>
      <Link to="/admin" className="mt-8 btn-primary">
        前往管理后台
      </Link>
    </div>
  )
}

export function HomeFeaturedSection({
  featuredItem,
  featuredVisual,
  featuredPoster,
  showDiscover,
}: {
  featuredItem: Media
  featuredVisual: string
  featuredPoster: string
  showDiscover: boolean
}) {
  const presentation = episodePresentation(featuredItem)
  return (
    <section className="relative overflow-hidden rounded-[2rem] border border-[var(--app-border)] bg-[var(--app-panel)] shadow-elevated">
      {/* 背景：氛围底 + backdrop 大图 + 可读性遮罩 */}
      <div className="absolute inset-0 z-0">
        <div className="theme-hero-bg h-full w-full" />
        {featuredVisual && (
          <img
            src={imageURL(featuredVisual, featuredItem.updated_at, { maxWidth: 1920 })}
            alt=""
            className="absolute inset-0 h-full w-full scale-105 object-cover object-[center_20%] opacity-80"
            referrerPolicy="no-referrer"
            onError={(event) => { event.currentTarget.style.display = 'none' }}
          />
        )}
        <div className="theme-hero-overlay absolute inset-0" />
        <div className="theme-hero-fade absolute inset-x-0 bottom-0 h-40" />
      </div>

      <div className="relative z-10 grid gap-10 px-6 py-10 sm:px-10 md:grid-cols-[minmax(0,1fr)_240px] md:py-14 lg:grid-cols-[minmax(0,1fr)_280px] lg:px-14">
        <div className="flex min-w-0 flex-col justify-center">
          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, ease: [0.21, 0.47, 0.32, 0.98] }}
            className="inline-flex w-fit items-center gap-2 rounded-full border border-[var(--app-brand-border)] bg-[var(--app-brand-soft)] px-3.5 py-1.5 text-[11px] font-bold uppercase tracking-[0.2em] text-[var(--app-brand-text)] backdrop-blur"
          >
            <Sparkles size={12} fill="currentColor" />
            <span>本周力荐 · Featured</span>
          </motion.div>

          <motion.h1
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.55, delay: 0.08, ease: [0.21, 0.47, 0.32, 0.98] }}
            className="mt-5 max-w-3xl font-display text-[clamp(1.5rem,3.4vw,2.6rem)] font-extrabold leading-[1.18] tracking-tight text-[var(--app-text)] line-clamp-3 [text-wrap:balance]"
          >
            {presentation.title}
          </motion.h1>
          {presentation.subtitle && <p className="mt-2 text-sm text-[var(--app-subtle)]">{presentation.subtitle}</p>}

          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.16, ease: [0.21, 0.47, 0.32, 0.98] }}
            className="mt-4 flex flex-wrap items-center gap-2 text-xs font-bold"
          >
            {featuredItem.year > 0 && (
              <span className="rounded-lg border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-2.5 py-1 text-[var(--app-text)] backdrop-blur">{featuredItem.year}</span>
            )}
            {featuredItem.rating > 0 && (
              <span className="badge-gold">★ {featuredItem.rating.toFixed(1)}</span>
            )}
            {featuredItem.video_codec && (
              <span className="rounded-lg border border-[var(--app-brand-border)] bg-[var(--app-brand-soft)] px-2.5 py-1 uppercase text-[var(--app-brand-text)] backdrop-blur">
                {featuredItem.video_codec}
              </span>
            )}
            {featuredItem.container && (
              <span className="rounded-lg border border-[var(--app-border)] bg-[var(--app-panel)]/70 px-2.5 py-1 font-mono text-[10px] uppercase text-[var(--app-subtle)] backdrop-blur">
                {featuredItem.container}
              </span>
            )}
          </motion.div>

          <motion.p
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.24, ease: [0.21, 0.47, 0.32, 0.98] }}
            className="mt-4 line-clamp-2 max-w-2xl text-sm font-medium leading-relaxed text-[var(--app-subtle)]"
          >
            {featuredItem.overview || '家庭私人媒体中心收藏。支持多端播放、外部播放器与智能刮削。'}
          </motion.p>

          <motion.div
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.32, ease: [0.21, 0.47, 0.32, 0.98] }}
            className="mt-7 flex flex-wrap items-center gap-3.5"
          >
            <Link to={mediaDetailLink(featuredItem)} className="btn-primary px-7 py-3.5">
              <Play size={16} fill="currentColor" />
              <span>立即播放</span>
            </Link>
            {showDiscover && (
              <Link to="/discover" className="btn-outline px-5 py-3.5">
                <span>发现更多精彩</span>
                <ArrowRight size={16} />
              </Link>
            )}
          </motion.div>
        </div>

        {/* 右侧海报：紫罗兰光晕 + 悬浮 */}
        <div className="relative order-first mx-auto flex w-full max-w-[200px] items-center md:order-none md:max-w-none">
          <motion.div
            initial={{ opacity: 0, y: 24, rotate: 2 }}
            animate={{ opacity: 1, y: 0, rotate: 0 }}
            whileHover={{ y: -8, rotate: 1 }}
            transition={{ duration: 0.6, delay: 0.2, type: 'spring', stiffness: 120, damping: 18 }}
            className="relative w-full"
          >
            <div className="absolute -inset-6 rounded-[2.5rem] bg-brand-500/20 blur-3xl" />
            <div className="relative aspect-[2/3] w-full overflow-hidden rounded-[1.5rem] border border-white/15 shadow-[0_32px_80px_rgba(0,0,0,0.45)]">
              <div className="flex h-full w-full flex-col items-center justify-center text-center" style={{ background: 'var(--app-poster-empty)' }}>
                <Film className="mb-3 h-10 w-10 text-brand-400" />
                <span className="px-5 font-display text-xl font-black leading-snug tracking-tight text-[var(--app-text)] [text-wrap:balance]">{presentation.title}</span>
              </div>
              {featuredPoster && (
                <img
                  src={imageURL(featuredPoster, featuredItem.updated_at)}
                  alt={presentation.title}
                  className="absolute inset-0 h-full w-full object-cover"
                  referrerPolicy="no-referrer"
                  onError={(event) => { event.currentTarget.style.display = 'none' }}
                />
              )}
            </div>
          </motion.div>
        </div>
      </div>
    </section>
  )
}

export function ContinueWatchingSection({ history }: { history: HistoryItem[] }) {
  return (
    <section className="space-y-5">
      <div className="flex items-center gap-2.5">
        <span className="rounded-xl border border-[var(--app-border)] bg-[var(--app-panel-soft)] p-1.5 text-[var(--app-text)]">
          <Clock size={16} />
        </span>
        <h2 className="font-display text-xl font-extrabold tracking-tight text-[var(--app-text)]">继续观看</h2>
        <span className="rounded-full border border-[var(--app-border)] bg-[var(--app-panel-soft)] px-2.5 py-0.5 text-xs font-bold text-[var(--app-muted)]">{history.length} 个记录</span>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-4">
        {history.slice(0, 8).map((h) => {
          const media = h.media!
          const progress = h.duration_ms > 0 ? h.position_ms / h.duration_ms : 0
          return <ContinueCard key={h.id} media={media} progress={progress} />
        })}
      </div>
    </section>
  )
}

export function RecentMediaSection({ recentCards }: { recentCards: SeriesCard[] }) {
  return (
    <section className="space-y-5">
      <div className="flex items-center justify-between border-b border-[var(--app-border)] pb-3">
        <div className="flex items-center gap-2.5">
          <span className="rounded-xl border border-[var(--app-border)] bg-[var(--app-panel-soft)] p-1.5 text-[var(--app-text)]">
            <Clock size={18} />
          </span>
          <div>
            <h2 className="font-display text-xl font-extrabold tracking-tight text-[var(--app-text)]">最近入库</h2>
            <p className="text-xs text-[var(--app-muted)]">按整部电影、剧集、番剧和综艺合集展示新增内容。</p>
          </div>
        </div>
        <Link to="/libraries?view=poster" className="group inline-flex items-center gap-1 text-xs font-bold text-[var(--app-subtle)] transition-colors hover:text-brand-500">
          <span>海报视图</span>
          <ArrowRight size={14} className="transition-transform group-hover:translate-x-0.5" />
        </Link>
      </div>

      <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
        {recentCards.map((card, index) => (
          <MediaCard
            key={card.key}
            media={card.rep}
            count={card.count}
            linkTo={seriesCardLink(card)}
            staggerIndex={index}
          />
        ))}
      </div>
    </section>
  )
}

function ContinueCard({ media, progress }: { media: Media; progress: number }) {
  const presentation = episodePresentation(media)
  return (
    <Link to={mediaDetailLink(media)} className="group flex items-center gap-4 rounded-2xl border border-[var(--app-border)] bg-[var(--app-panel)] p-3.5 shadow-[0_1px_3px_rgba(0,0,0,0.01)] transition-all duration-300 hover:border-brand-500/30 hover:bg-[var(--app-panel-soft)] hover:shadow-md">
      <div className="relative h-18 w-12 shrink-0 overflow-hidden rounded-xl border border-[var(--app-border)] bg-[var(--app-panel-soft)]">
        {media.poster_url ? (
          <img
            src={imageURL(media.poster_url, media.updated_at)}
            alt=""
            loading="lazy"
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
            referrerPolicy="no-referrer"
          />
        ) : (
          <div className="flex h-full items-center justify-center bg-[var(--app-panel-soft)] text-[var(--app-muted)]">
            <Film size={16} />
          </div>
        )}
        <div className="absolute inset-0 flex items-center justify-center bg-black/30 opacity-0 transition-opacity group-hover:opacity-100">
          <Play size={14} fill="white" className="text-white" />
        </div>
      </div>
      <div className="min-w-0 flex-1 space-y-1.5">
        <p className="truncate text-sm font-bold text-[var(--app-text)] transition-colors group-hover:text-brand-500">
          {presentation.title}
        </p>
        {presentation.subtitle && <p className="truncate text-xs text-[var(--app-muted)]" title={presentation.subtitle}>{presentation.subtitle}</p>}
        <div className="space-y-1">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--app-hover)]">
            <motion.div
              initial={{ width: 0 }}
              animate={{ width: `${Math.round(progress * 100)}%` }}
              transition={{ duration: 0.6, delay: 0.1, ease: [0.21, 0.47, 0.32, 0.98] }}
              className="h-full rounded-full"
              style={{ background: 'linear-gradient(90deg, #8b5cf6, #d946ef)', boxShadow: '0 0 8px rgba(139,92,246,0.5)' }}
            />
          </div>
          <p className="text-[10px] font-bold uppercase tracking-wide text-[var(--app-muted)]">
            已观看到 {Math.round(progress * 100)}%
          </p>
        </div>
      </div>
    </Link>
  )
}
