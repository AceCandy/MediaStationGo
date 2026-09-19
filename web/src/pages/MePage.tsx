import { Clock, Heart, ListMusic } from 'lucide-react'
import { Link, Navigate, useSearchParams } from 'react-router-dom'
import { useEffect, useState } from 'react'
import toast from 'react-hot-toast'
import { hongguoAPI, type HongGuoUserCard } from '../api/hongguo'
import { Select } from '../components/Select'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'

import { FavouritesPage } from './FavouritesPage'
import { PlaylistsPage } from './PlaylistsPage'
import { WatchHistoryPage } from './WatchHistoryPage'

const TABS = [
  { id: 'favourites', label: '收藏', icon: Heart },
  { id: 'playlists', label: '播放列表', icon: ListMusic },
  { id: 'history', label: '观看历史', icon: Clock },
] as const

type MeTab = (typeof TABS)[number]['id']

export function MePage() {
  const accessKey = useMediaAccessKey()
  const [searchParams, setSearchParams] = useSearchParams()
  const source = searchParams.get('source') ?? 'catalog'
  const tabValues = searchParams.getAll('tab')
  const requestedTab = tabValues[0]
  const activeTab = (requestedTab ?? 'favourites') as MeTab
  const valid = tabValues.length <= 1 && TABS.some((tab) => tab.id === activeTab)

  if (!valid) return <Navigate to="/me?tab=favourites" replace />
  if (!['catalog', 'hongguo'].includes(source) || searchParams.getAll('source').length > 1) return <Navigate to={`/me?tab=${activeTab}`} replace />
  if (source === 'hongguo' && activeTab === 'playlists') return <Navigate to="/me?tab=favourites&source=hongguo" replace />

  return (
    <div className="space-y-6">
      <div>
        <h1 className="font-display text-3xl font-bold text-[var(--app-text)]">我的</h1>
        <p className="mt-1 text-sm text-[var(--app-muted)]">收藏内容、播放列表与观看记录</p>
      </div>

      <Select aria-label="用户记录资料体系" value={source} onChange={(value) => {
        const next = new URLSearchParams(searchParams); next.set('source', value); next.delete('page');
        if (value === 'hongguo' && activeTab === 'playlists') next.set('tab', 'favourites')
        setSearchParams(next)
      }}><option value="catalog">现有资料体系</option><option value="hongguo">红果短剧</option></Select>

      <nav aria-label="我的内容" className="tab-list w-fit">
        {TABS.filter((tab) => source !== 'hongguo' || tab.id !== 'playlists').map((tab) => {
          const Icon = tab.icon
          const active = tab.id === activeTab
          return (
            <Link
              key={tab.id}
              to={`/me?tab=${tab.id}${source === 'hongguo' ? '&source=hongguo' : ''}`}
              aria-current={active ? 'page' : undefined}
              className="tab-item"
            >
              <Icon size={16} />
              {tab.label}
            </Link>
          )
        })}
      </nav>

      {source === 'hongguo' ? <HongGuoMyItems key={`${accessKey}:${activeTab}`} tab={activeTab === 'history' ? 'history' : 'favourites'} /> : <>
        {activeTab === 'favourites' && <FavouritesPage embedded />}
        {activeTab === 'playlists' && <PlaylistsPage embedded />}
        {activeTab === 'history' && <WatchHistoryPage embedded />}
      </>}
    </div>
  )
}

function HongGuoMyItems({ tab }: { tab: 'favourites' | 'history' }) {
  const [params, setParams] = useSearchParams()
  const rawPage = Number(params.get('page') ?? 1)
  const page = Number.isInteger(rawPage) && rawPage >= 1 && rawPage <= 1000000 ? rawPage : 1
  const [data, setData] = useState<{ items: HongGuoUserCard[]; total: number; page: number } | null>(null)
  const [error, setError] = useState(false)
  const [retry, setRetry] = useState(0)
  const [busy, setBusy] = useState(false)
  const markPlayed = async (mediaID: string, played: boolean) => {
    if (busy) return
    setBusy(true)
    try {
      await hongguoAPI.markPlayed(mediaID, played)
      setData(null); setRetry((value) => value + 1)
      toast.success(played ? '已标记看完' : '已清除观看进度，播放统计记录保留')
    } catch { toast.error('观看状态更新失败') }
    finally { setBusy(false) }
  }
  useEffect(() => {
    if (rawPage === page && params.getAll('page').length <= 1) return
    const next = new URLSearchParams(params); next.set('page', String(page)); setParams(next, { replace: true })
  }, [rawPage, page, params, setParams])
  useEffect(() => {
    const controller = new AbortController(); setError(false)
    void hongguoAPI.userCards(tab, page, controller.signal).then((result) => { if (!controller.signal.aborted) setData({ ...result, page }) }).catch(() => { if (!controller.signal.aborted) setError(true) })
    return () => controller.abort()
  }, [tab, page, retry])
  if (error) return <p role="alert">红果记录读取失败 <button className="btn-outline" onClick={() => setRetry((v) => v + 1)}>重试</button></p>
  if (data?.page !== page) return <p role="status">读取红果记录中…</p>
  const goPage = (value: number) => { const next = new URLSearchParams(params); next.set('page', String(value)); setParams(next) }
  return <section className="space-y-4">
    {data.items.length === 0 ? <p>暂无可访问的红果{tab === 'favourites' ? '收藏' : '观看记录'}。</p> : <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{data.items.map((item) => <article className="card space-y-2 p-4" key={`${item.source_id}:${item.episode_number}`}>
      <Link className="font-semibold" to={`/discover?system=hongguo&id=${encodeURIComponent(item.source_id)}`}>{item.title}</Link>
      {tab === 'history' && <><p className="text-sm text-ink-50">{item.kind === 'series' ? `S${item.season_number}E${String(item.episode_number).padStart(3, '0')} · ` : ''}{item.completed ? '已看完' : `已观看 ${Math.floor(item.position_ms / 1000)} 秒`}</p><Link className="btn-outline" to={`/play/${encodeURIComponent(item.media_id)}?start_ms=${item.completed ? 0 : item.position_ms}`} state={{ from: `/me?${params.toString()}` }}>{item.completed ? '重新播放' : '继续播放'}</Link></>}
      {tab === 'history' && <div className="flex flex-wrap gap-2">{!item.completed && <button className="btn-outline" disabled={busy} onClick={() => void markPlayed(item.media_id, true)}>标记已看</button>}<button className="btn-outline" disabled={busy} onClick={() => void markPlayed(item.media_id, false)}>清除观看进度</button></div>}
    </article>)}</div>}
    <div className="flex items-center gap-3"><button className="btn-outline" disabled={page <= 1} onClick={() => goPage(page - 1)}>上一页</button><span>第 {page} 页 · 共 {data.total} 项</span><button className="btn-outline" disabled={page * 50 >= data.total} onClick={() => goPage(page + 1)}>下一页</button></div>
  </section>
}
