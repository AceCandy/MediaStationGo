import { useEffect, useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { motion, AnimatePresence } from 'framer-motion'
import toast from 'react-hot-toast'
import { ArrowLeft, Play, Film } from 'lucide-react'

import { libraryAPI } from '../api/library'
import type { Library, Media } from '../types'
import { MediaCard } from '../components/MediaCard'
import { ExternalPlayerButton } from '../components/ExternalPlayerButton'
import { imageURL } from '../api/client'
import { useAuthStore } from '../stores/auth'
import { getSeriesKey, groupSeries, isEpisodeLike, seriesTitle, type SeriesCard } from '../utils/groupSeries'

export function LibraryPage() {
  const { id = '' } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const role = useAuthStore((s) => s.user?.role)

  const [library, setLibrary] = useState<Library | null>(null)
  const [items, setItems] = useState<Media[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [scraping, setScraping] = useState(false)

  // 剧集模式：选中某个剧集后展开详情
  const [selectedSeries, setSelectedSeries] = useState<SeriesCard | null>(null)
  const [selectedSeason, setSelectedSeason] = useState<number | null>(null)

  const hasEpisodicItems = useMemo(() => items.some(isEpisodeLike), [items])
  const isSeries = library?.type === 'tv' || library?.type === 'anime' || library?.type === 'variety' || hasEpisodicItems

  // 折叠后的剧集卡片
  const seriesCards = useMemo(() => {
    if (!isSeries || items.length === 0) return []
    return groupSeries(items)
  }, [isSeries, items])

  // 选中的剧集：所有集按季分组
  const selectedEpisodes = useMemo(() => {
    if (!selectedSeries || items.length === 0) return []
    const eps = items.filter((m) => getSeriesKey(m) === selectedSeries.key)
    // 按季分组，按集排序
    const seasons = new Map<number, Media[]>()
    for (const ep of eps) {
      const s = ep.season_num || 1
      if (!seasons.has(s)) seasons.set(s, [])
      seasons.get(s)!.push(ep)
    }
    for (const [, list] of seasons) {
      list.sort((a, b) => (a.episode_num || 0) - (b.episode_num || 0))
    }
    return Array.from(seasons.entries())
      .sort(([a], [b]) => a - b)
      .map(([season, episodes]) => ({ season, episodes }))
  }, [selectedSeries, items])

  const visibleEpisodes = useMemo(() => {
    if (selectedSeason == null) return selectedEpisodes[0]?.episodes ?? []
    return selectedEpisodes.find((s) => s.season === selectedSeason)?.episodes ?? []
  }, [selectedEpisodes, selectedSeason])

  useEffect(() => {
    if (!id) return
    libraryAPI.list().then((all) => {
      const lib = all.find((l) => l.id === id) ?? null
      setLibrary(lib)
    })
  }, [id])

  useEffect(() => {
    if (!id || !library) return
    setLoading(true)
    // 所有库都可能包含季集型内容（综艺/纪录片/课程等），统一拉取较大页数用于前端分组。
    const limit = 2000
    libraryAPI
      .listMedia(id, 1, limit)
      .then((d) => {
        setItems(d.items)
        setTotal(d.total)
      })
      .finally(() => setLoading(false))
  }, [id, library])

  useEffect(() => {
    if (loading) return
    if (!isSeries) {
      setSelectedSeries(null)
      setSelectedSeason(null)
      return
    }

    const key = searchParams.get('series')
    if (!key) {
      setSelectedSeries(null)
      return
    }

    const next = seriesCards.find((card) => card.key === key)
    if (next) setSelectedSeries(next)
  }, [isSeries, loading, searchParams, seriesCards])

  useEffect(() => {
    if (!selectedSeries || selectedEpisodes.length === 0) {
      setSelectedSeason(null)
      return
    }
    if (selectedSeason == null || !selectedEpisodes.some((s) => s.season === selectedSeason)) {
      setSelectedSeason(selectedEpisodes[0].season)
    }
  }, [selectedSeries, selectedEpisodes, selectedSeason])

  const handleScan = async () => {
    setScanning(true)
    try {
      const r = await libraryAPI.scan(id)
      toast.success(`扫描完成:新增 ${r.added} 项，更新 ${r.updated ?? 0} 项`)
      setLibrary((l) => (l ? { ...l } : l))
    } catch {
      toast.error('扫描失败')
    } finally { setScanning(false) }
  }

  const handleScrape = async () => {
    setScraping(true)
    try {
      await libraryAPI.scrape(id)
      toast.success('刮削已加入后台队列')
    } catch { toast.error('刮削失败') }
    finally { setScraping(false) }
  }

  const handleSeriesClick = (card: SeriesCard) => {
    setSelectedSeries(card)
    const next = new URLSearchParams(searchParams)
    next.set('series', card.key)
    setSearchParams(next)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  const clearSelectedSeries = () => {
    setSelectedSeries(null)
    setSelectedSeason(null)
    const next = new URLSearchParams(searchParams)
    next.delete('series')
    setSearchParams(next)
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-32">
        <motion.div animate={{ opacity: [0.4, 1, 0.4] }} transition={{ repeat: Infinity, duration: 2 }} className="flex items-center gap-3">
          <div className="h-2 w-2 rounded-full bg-brand-500" />
          <span className="text-sm text-sand-500">加载中…</span>
        </motion.div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="font-display text-3xl font-bold text-ink-600">
            {library?.name ?? '媒体库'}
            <span className="text-sand-500"> ({isSeries ? seriesCards.length : total})</span>
          </h1>
          {library && <p className="text-sm text-ink-50">{library.type} · {library.path}</p>}
        </div>
        {role === 'admin' && (
          <div className="flex flex-wrap gap-2">
            <button onClick={handleScan} disabled={scanning} className="btn-outline">{scanning ? '扫描中…' : '立即扫描'}</button>
            <button onClick={handleScrape} disabled={scraping} className="btn-outline">{scraping ? '刮削中…' : '刮削元数据'}</button>
          </div>
        )}
      </div>

      {/* 非剧集：直接展示海报网格 */}
      {!isSeries && items.length > 0 && (
        <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
          {items.map((m) => (
            <MediaCard key={m.id} media={m} />
          ))}
        </div>
      )}

      {!isSeries && items.length === 0 && (
        <div className="flex flex-col items-center justify-center py-24 text-center">
          <Film className="h-12 w-12 text-gray-500 mb-4" />
          <p className="text-ink-50">该媒体库暂无内容，触发一次扫描后再来看看</p>
        </div>
      )}

      {/* 剧集模式：折叠卡片网格 */}
      {isSeries && seriesCards.length > 0 && !selectedSeries && (
        <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
          {seriesCards.map((s) => (
            <MediaCard key={s.key} media={s.rep} count={s.count} onClick={() => handleSeriesClick(s)} />
          ))}
        </div>
      )}

      {/* 剧集详情：季/集选择器 */}
      <AnimatePresence mode="wait">
        {selectedSeries && (
          <motion.div
            initial={{ opacity: 0, y: 16 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            className="space-y-6"
          >
            {/* 返回按钮 + 标题 */}
            <div className="flex items-center gap-4">
              <button
                onClick={clearSelectedSeries}
                className="btn-ghost gap-2"
              >
                <ArrowLeft size={16} />
                返回列表
              </button>
              <h2 className="font-display text-2xl font-bold text-ink-600 truncate">
                {seriesTitle(selectedSeries.rep)}
              </h2>
              <span className="text-sm text-sand-500">共 {selectedSeries.count} 集</span>
            </div>

            {/* 海报 + 从第一集开始播放 */}
            <div className="flex flex-col sm:flex-row gap-6">
              <div className="w-40 shrink-0 overflow-hidden rounded-xl bg-sand-200 shadow-card">
                {selectedSeries.rep.poster_url ? (
                  <img src={imageURL(selectedSeries.rep.poster_url)} alt={selectedSeries.rep.title} className="w-full aspect-[2/3] object-cover" referrerPolicy="no-referrer" />
                ) : (
                  <div className="flex items-center justify-center aspect-[2/3] text-gray-500"><Film size={40} /></div>
                )}
              </div>
              <div className="flex-1 space-y-3">
                <p className="text-sm text-ink-50 leading-relaxed">
                  {selectedSeries.rep.overview || '暂无简介'}
                </p>

                {/* 从第一集开始 */}
                {(() => {
                  const firstEps = [...(visibleEpisodes.length > 0 ? visibleEpisodes : selectedEpisodes.flatMap((s) => s.episodes))]
                  firstEps.sort((a, b) =>
                    (a.season_num || 0) - (b.season_num || 0)
                    || (a.episode_num || 0) - (b.episode_num || 0),
                  )
                  const first = firstEps.length > 0 ? firstEps[0] : null
                  return first ? (
                    <div className="flex flex-wrap gap-2">
                      <Link to={`/play/${first.id}`} className="btn-primary inline-flex">
                        <Play size={16} fill="currentColor" />
                        从第一集开始播放
                      </Link>
                      <ExternalPlayerButton mediaId={first.id} label="外部播放器播放" />
                    </div>
                  ) : null
                })()}
              </div>
            </div>

            {/* 季 / 集列表 */}
            <div className="space-y-6">
              <div className="flex flex-wrap items-center gap-2">
                {selectedEpisodes.map(({ season, episodes }) => (
                  <button
                    key={season}
                    onClick={() => setSelectedSeason(season)}
                    className={
                      'rounded-xl border px-4 py-2 text-sm font-semibold transition ' +
                      (selectedSeason === season
                        ? 'border-brand-300 bg-brand-50 text-brand-700'
                        : 'border-sand-200 bg-white text-ink-100 hover:border-brand-200 hover:text-brand-600')
                    }
                  >
                    第 {season} 季 · {episodes.length} 集
                  </button>
                ))}
              </div>

              <div>
                <h3 className="mb-3 font-display text-lg font-semibold text-ink-600">
                  第 {selectedSeason ?? selectedEpisodes[0]?.season ?? 1} 季
                </h3>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
                  {visibleEpisodes.map((ep) => (
                    <div
                      key={ep.id}
                      className="group flex items-center gap-3 rounded-xl border border-sand-200 bg-white p-3 shadow-card transition-all hover:border-brand-300 hover:shadow-card-hover"
                    >
                      <Link to={`/play/${ep.id}`} className="flex min-w-0 flex-1 items-center gap-3">
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-brand-50 text-brand-600 font-semibold text-sm">
                          {ep.episode_num || '—'}
                        </div>
                        <div className="min-w-0 flex-1">
                          <p className="truncate text-sm font-medium text-ink-600">
                            {ep.original_name || (ep.episode_num > 0 ? `第 ${ep.episode_num} 集` : ep.title)}
                          </p>
                          <p className="text-xs text-sand-500">
                            {ep.duration_sec > 0
                              ? `${Math.floor(ep.duration_sec / 60)} 分钟`
                              : formatSize(ep.size_bytes)}
                          </p>
                        </div>
                        <Play size={14} className="shrink-0 text-gray-500 opacity-0 transition-opacity group-hover:opacity-100 group-hover:text-brand-500" />
                      </Link>
                      <ExternalPlayerButton mediaId={ep.id} label="外部" compact />
                    </div>
                  ))}
                </div>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* 剧集为空 */}
      {isSeries && seriesCards.length === 0 && !loading && (
        <div className="flex flex-col items-center justify-center py-24 text-center">
          <Film className="h-12 w-12 text-gray-500 mb-4" />
          <p className="text-ink-50">该库尚未发现任何剧集，触发一次扫描后再来看看</p>
        </div>
      )}
    </div>
  )
}

function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = bytes, i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${units[i]}`
}
