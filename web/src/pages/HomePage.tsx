import { useEffect, useMemo, useState } from 'react'
import toast from 'react-hot-toast'

import { aiAPI } from '../api/ai'
import { mediaAPI } from '../api/library'
import { historyAPI } from '../api/history'
import { useLayoutPermissions } from '../components/useLayoutPermissions'
import { useAuthStore } from '../stores/auth'
import type { HistoryItem, Media } from '../types'
import type { SeriesCard } from '../utils/groupSeries'
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
  const [recentCards, setRecentCards] = useState<SeriesCard[]>([])
  const [history, setHistory] = useState<HistoryItem[]>([])
  const [pendingRecent, setPendingRecent] = useState(true)
  const [pendingHistory, setPendingHistory] = useState(true)
  const [recommendationsAvailable, setRecommendationsAvailable] = useState(false)
  const [recommendations, setRecommendations] = useState<string[] | null>(null)
  const [recommending, setRecommending] = useState(false)

  useEffect(() => {
    let cancelled = false
    mediaAPI.recent(24).then((rows) => {
      if (!cancelled) setRecentCards(asArray<SeriesCard>(rows))
    }).catch(() => undefined).finally(() => {
      if (!cancelled) setPendingRecent(false)
    })
    historyAPI.continueWatching(30)
      .then((rows) => {
        if (!cancelled) setHistory(asArray<{ history: HistoryItem; media: Media }>(rows).map(({ history: item, media }) => ({ ...item, media })).filter((h) => h && !h.completed && !!h.media))
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setPendingHistory(false)
      })
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
  const empty = !pendingRecent && !pendingHistory && recentCards.length === 0 && history.length === 0

  if (pendingRecent && pendingHistory && recentCards.length === 0 && history.length === 0) {
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
