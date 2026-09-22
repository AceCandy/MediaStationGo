import { useEffect, useMemo } from 'react'

import type { Media } from '../types'
import { getSeriesKey, type SeriesCard } from '../utils/groupSeries'
import { distinctEpisodes } from './seriesDetailModel'

type SeasonEpisodes = {
  season: number
  episodes: Media[]
}

type UseLibrarySeriesSelectionOptions = {
  items: Media[]
  seriesEpisodeItems: Media[]
  isSeriesLibrary: boolean
  isSeries: boolean
  loading: boolean
  seriesCards: SeriesCard[]
  searchParams: URLSearchParams
  setSearchParams: (params: URLSearchParams) => void
  selectedSeries: SeriesCard | null
  setSelectedSeries: (series: SeriesCard | null) => void
  onClearSeriesState?: () => void
}

export function useLibrarySeriesSelection({
  items,
  seriesEpisodeItems,
  isSeriesLibrary,
  isSeries,
  loading,
  seriesCards,
  searchParams,
  setSearchParams,
  selectedSeries,
  setSelectedSeries,
  onClearSeriesState,
}: UseLibrarySeriesSelectionOptions) {
  const allSeasons = useMemo(() => {
    const sourceItems = isSeriesLibrary ? seriesEpisodeItems : items
    if (!selectedSeries) return []
    const eps = isSeriesLibrary
      ? sourceItems
      : sourceItems.filter((m) => getSeriesKey(m) === selectedSeries.key)
    const seasons = new Map<number, Media[]>()
    for (const ep of eps) {
      const s = ep.episode_num > 0 ? (ep.season_num ?? 0) : (ep.season_num || 1)
      if (!seasons.has(s)) seasons.set(s, [])
      seasons.get(s)!.push(ep)
    }
    for (const [, list] of seasons) {
      list.sort((a, b) => (a.episode_num || 0) - (b.episode_num || 0))
    }
    for (const season of selectedSeries.seasons ?? []) {
      if (!seasons.has(season)) seasons.set(season, [])
    }
    return Array.from(seasons.entries())
      .sort(([a], [b]) => a - b)
      .map(([season, episodes]) => ({ season, episodes }))
  }, [isSeriesLibrary, selectedSeries, items, seriesEpisodeItems])

  const selectedEpisodes = useMemo(() => allSeasons.map((group) => ({ ...group, episodes: distinctEpisodes(group.episodes) })), [allSeasons])

  const selectedSeriesEpisodes = useMemo(
    () => allSeasons.flatMap((season: SeasonEpisodes) => season.episodes),
    [allSeasons],
  )

  useEffect(() => {
    if (loading) return
    if (!isSeries) {
      setSelectedSeries(null)
      return
    }

    const seriesID = searchParams.get('series_id')
    const key = searchParams.get('series')
    if (!seriesID && !key) {
      setSelectedSeries(null)
      return
    }

    const next = seriesCards.find((card) => seriesID ? card.rep.series_id === seriesID : card.key === key)
    setSelectedSeries(next ?? null)
  }, [isSeries, loading, searchParams, seriesCards, setSelectedSeries])

  const handleSeasonChange = (season: number) => {
    const next = new URLSearchParams(searchParams)
    next.set('season', String(season))
    next.delete('episode')
    next.delete('version')
    setSearchParams(next)
  }

  const handleSeriesClick = (card: SeriesCard) => {
    setSelectedSeries(card)
    const next = new URLSearchParams(searchParams)
    next.delete('series_id')
    next.delete('season')
    next.delete('episode')
    next.delete('version')
    next.set('series', card.key)
    setSearchParams(next)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  const clearSelectedSeries = () => {
    setSelectedSeries(null)
    onClearSeriesState?.()
    const next = new URLSearchParams(searchParams)
    next.delete('series')
    next.delete('series_id')
    next.delete('season')
    next.delete('episode')
    next.delete('version')
    setSearchParams(next)
  }

  return {
    selectedEpisodes,
    selectedSeriesEpisodes,
    handleSeriesClick,
    handleSeasonChange,
    clearSelectedSeries,
  }
}
