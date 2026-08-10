import { useEffect, useMemo, useState } from 'react'
import toast from 'react-hot-toast'

import { aiAPI } from '../api/ai'
import { libraryAPI, mediaAPI } from '../api/library'
import { playbackAPI, type HistoryItem } from '../api/playback'
import { useLayoutPermissions } from '../components/useLayoutPermissions'
import { useAuthStore } from '../stores/auth'
import type { Library, Media } from '../types'
import { groupSeries, type SeriesCard } from '../utils/groupSeries'
import {
  ContinueWatchingSection,
  HomeEmptyState,
  HomeFeaturedSection,
  HomeLoadingState,
  RecentMediaSection,
} from './HomePageSections'
import { AIAssistantRecommendationsSection } from './AIAssistantRecommendationsSection'

const hasArtwork = (media?: Media | null) => !!(media?.poster_url || media?.backdrop_url)
const asArray = <T,>(value: unknown): T[] => (Array.isArray(value) ? value as T[] : [])

export function HomePage() {
  const user = useAuthStore((state) => state.user)
  const { can, isReady: permissionsReady } = useLayoutPermissions(user)
  const [libraries, setLibraries] = useState<Library[]>([])
  const [recentCards, setRecentCards] = useState<SeriesCard[]>([])
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [recommendationsAvailable, setRecommendationsAvailable] = useState(false)
  const [recommendations, setRecommendations] = useState<string[] | null>(null)
  const [recommending, setRecommending] = useState(false)

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      try {
        const [libs, recentItems, hist] = await Promise.all([
          libraryAPI.list().then((rows) => asArray<Library>(rows)).catch(() => [] as Library[]),
          mediaAPI.recent(24).then((rows) => asArray<SeriesCard>(rows)).catch(async () => {
            const fallback = await mediaAPI.search('', 120).then((d) => asArray<Media>(d?.items)).catch(() => [] as Media[])
            return groupSeries(fallback).slice(0, 24)
          }),
          playbackAPI.recentHistory().then((rows) => asArray<HistoryItem>(rows)).catch(() => [] as HistoryItem[]),
        ])
        if (cancelled) return
        setLibraries(libs)
        setRecentCards(recentItems)
        setHistory(hist.filter((h) => h && !h.completed && !!h.media))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    if (!permissionsReady || !can('can_use_ai_assistant')) {
      setRecommendationsAvailable(false)
      return
    }
    let cancelled = false
    aiAPI.status()
      .then((status) => {
        if (!cancelled) setRecommendationsAvailable(status.enabled)
      })
      .catch(() => {
        if (!cancelled) setRecommendationsAvailable(false)
      })
    return () => {
      cancelled = true
    }
  }, [can, permissionsReady])

  const generateRecommendations = async () => {
    setRecommending(true)
    try {
      setRecommendations(await aiAPI.recommend())
    } catch {
      toast.error('获取推荐失败')
    } finally {
      setRecommending(false)
    }
  }

  const featuredItem = useMemo(() => {
    const candidates = [
      ...(history.map((h) => h.media).filter(Boolean) as Media[]),
      ...recentCards.map((card) => card.rep),
    ]
    return candidates.find(hasArtwork) ?? candidates[0] ?? null
  }, [history, recentCards])
  const featuredVisual = featuredItem?.backdrop_url || featuredItem?.poster_url || ''
  const featuredPoster = featuredItem?.poster_url || featuredItem?.backdrop_url || ''
  const featuredMark = (featuredItem?.title || 'MS').trim().slice(0, 4).toUpperCase()
  const empty = !loading && libraries.length === 0 && recentCards.length === 0 && history.length === 0

  if (loading) {
    return <HomeLoadingState />
  }

  if (empty) {
    return <HomeEmptyState />
  }

  return (
    <div className="space-y-12">
      {featuredItem && (
        <HomeFeaturedSection
          featuredItem={featuredItem}
          featuredVisual={featuredVisual}
          featuredPoster={featuredPoster}
          featuredMark={featuredMark}
          showDiscover={can('can_view_discover')}
        />
      )}

      {history.length > 0 && <ContinueWatchingSection history={history} />}
      {recommendationsAvailable && (
        <AIAssistantRecommendationsSection
          recs={recommendations}
          recommending={recommending}
          onRecommend={generateRecommendations}
        />
      )}
      {recentCards.length > 0 && <RecentMediaSection recentCards={recentCards} />}
    </div>
  )
}
