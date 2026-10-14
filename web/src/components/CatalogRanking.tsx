import { useEffect, useRef, useState } from 'react'
import { Film, Flame, RefreshCw } from 'lucide-react'
import './CatalogRanking.css'

export function CatalogRankingHeader({ source, title, total, options, value, onChange, onRefresh, refreshing = false, refreshDisabled = false, updatedAt }: { source: string; title: string; total?: number; options: readonly { value: string; label: string }[]; value: string; onChange: (value: string) => void; onRefresh?: () => void; refreshing?: boolean; refreshDisabled?: boolean; updatedAt?: string | null }) {
  const headerRef = useRef<HTMLElement>(null)
  useEffect(() => { headerRef.current?.scrollIntoView({ block: 'start' }) }, [])
  return <header ref={headerRef} className="ranking-header card flex flex-wrap items-center justify-between gap-6 p-5 sm:p-8">
    <div className="w-full min-w-0 lg:w-auto lg:min-w-[280px] lg:flex-1">
      <div className="flex flex-wrap items-center gap-3"><h2 className="flex items-center gap-3 text-2xl font-bold text-ink-600"><span className="rounded-xl bg-gold-500/10 p-3 text-gold-500"><Flame size={24} aria-hidden="true" /></span>{title}</h2>
      {onRefresh && <button type="button" className="btn-outline inline-flex items-center gap-2 text-sm" aria-label="刷新当前榜单" disabled={refreshing || refreshDisabled} onClick={onRefresh}><RefreshCw size={16} aria-hidden="true" className={refreshing ? 'motion-safe:animate-spin' : ''} />{refreshing ? '刷新中…' : '刷新榜单'}</button>}</div>
      <p className="mt-3 text-sm text-ink-50"><span className="font-semibold text-gold-500">{source}榜单</span>{total != null ? ` · 共 ${total} 部` : ' · 按来源顺序展示'}</p>
      <p className="mt-2 text-xs text-ink-50" aria-live="polite">最近更新：{updatedAt && !Number.isNaN(Date.parse(updatedAt)) ? <time dateTime={updatedAt}>{new Date(updatedAt).toLocaleString('zh-CN', { hour12: false })}</time> : '暂无成功更新记录'}</p>
      <p className="mt-3 text-sm leading-relaxed text-ink-50">按来源榜单顺序展示作品，探索热门内容与推荐佳作。</p>
    </div>
    <nav className="tab-list max-w-full flex-wrap" aria-label={`${source}榜单切换`}>{options.map(option => <button key={option.value} type="button" className="tab-item" aria-pressed={value === option.value} onClick={() => onChange(option.value)}>{option.label}</button>)}</nav>
  </header>
}

export function CatalogRankingRow({ work, position, artworkURL, metadata, rankLabel, onOpen, onImageError }: {
  work: { title: string; overview: string; rating: number; hydrated: boolean; downloaded?: boolean; owned?: boolean }
  position: number; artworkURL: string; metadata: string; rankLabel: string; onOpen: () => void; onImageError?: () => void
}) {
  const [failed, setFailed] = useState('')
  return <li>
    <button type="button" className="ranking-row" onClick={onOpen} aria-label={`查看${work.title}`}>
      <span className="ranking-position tabular-nums font-bold text-gold-500">{String(position).padStart(2, '0')}</span>
      <span className="ranking-poster relative" aria-hidden="true">
        {artworkURL && failed !== artworkURL ? <img src={artworkURL} alt="" loading="lazy" decoding="async" onError={() => {setFailed(artworkURL); onImageError?.()}} /> : <span aria-label="暂无海报" className="flex h-full items-center justify-center bg-gray-100 text-ink-50"><Film size={28} /></span>}
        {work.downloaded && <span data-catalog-downloaded className="absolute bottom-3 left-2 rounded-md bg-emerald-700/90 px-1 py-1 text-[10px] font-semibold text-white backdrop-blur xl:px-2 xl:text-xs" title="存在已完成的分集下载记录，不代表全剧下载完成、文件仍在或已入库">↓ 已下载</span>}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block break-words text-base font-semibold text-ink-600 sm:text-lg">{work.title}</span>
        <span className="mt-2 block text-sm leading-relaxed text-ink-50">{metadata}{metadata && ' · '}{!work.hydrated ? '待补齐' : work.rating > 0 ? `${work.rating.toFixed(1)}分` : '暂无评分'}{work.downloaded && ' · ↓ 已下载'}{work.owned && ' · 已入库'}</span>
        <span className="ranking-overview"><span className="min-h-0 overflow-hidden"><span className="ranking-summary block pt-4 text-sm leading-7 text-ink-600">{work.overview || '暂无简介'}</span></span></span>
      </span>
      <span className="ranking-label badge-gold ml-5 shrink-0"><Flame size={14} aria-hidden="true" />{rankLabel}</span>
    </button>
  </li>
}

export function CatalogRankingSkeleton() {
  return <div role="status" aria-label="加载榜单" className="space-y-3">{Array.from({length:5}, (_, index) => <div key={index} aria-hidden="true" className="flex items-center gap-5 rounded-xl border border-[var(--app-border)] bg-[var(--app-panel-soft)] p-5 motion-safe:animate-pulse"><span className="h-8 w-8 shrink-0 rounded bg-[var(--app-glass)]" /><div className="min-w-0 flex-1 space-y-3"><div className="h-5 w-2/3 rounded bg-[var(--app-glass)]" /><div className="h-3 w-1/2 rounded bg-[var(--app-glass)]" /></div></div>)}</div>
}
