import { useEffect, useMemo, useState } from 'react'
import { AlertTriangle, Sparkles } from 'lucide-react'

import { discoverAPI, type DiscoverItem, type DiscoverSection } from '../api/discover'
import { imageURL } from '../api/client'

const defaultSections = [
  'tmdb_trending_day',
  'douban_hot_movie',
  'douban_hot_tv',
  'bangumi_calendar',
]

const storageKey = 'mediastation.discover.sections'

export function DiscoverPage() {
  const [sections, setSections] = useState<DiscoverSection[]>([])
  const [selected, setSelected] = useState<string[]>(defaultSections)
  const [rows, setRows] = useState<Record<string, DiscoverItem[]>>({})
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    discoverAPI
      .sections()
      .then((items) => {
        setSections(items)
        const saved = readSavedSections(items)
        setSelected(saved.length > 0 ? saved : defaultSections)
      })
      .catch(() => {
        setSections(defaultSectionDefs)
        setSelected(defaultSections)
      })
  }, [])

  useEffect(() => {
    if (selected.length === 0) {
      setRows({})
      setLoading(false)
      return
    }
    setLoading(true)
    setError('')
    window.localStorage.setItem(storageKey, JSON.stringify(selected))
    discoverAPI
      .feed(selected)
      .then((feed) => {
        const next: Record<string, DiscoverItem[]> = {}
        for (const key of selected) {
          next[key] = feed[key] ?? []
        }
        setRows(next)
      })
      .catch((err) => {
        setRows({})
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => setLoading(false))
  }, [selected])

  const sectionMap = useMemo(
    () => new Map(sections.map((section) => [section.key, section])),
    [sections],
  )
  const hasContent = selected.some((key) => (rows[key] ?? []).length > 0)

  const toggleSection = (key: string) => {
    setSelected((current) => {
      if (current.includes(key)) {
        return current.filter((item) => item !== key)
      }
      return [...current, key]
    })
  }

  return (
    <div className="mx-auto max-w-7xl space-y-8 px-4 py-6">
      <header className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex items-center gap-4">
          <div className="rounded-2xl border border-primary-500/20 bg-gradient-to-br from-primary-500/20 to-primary-600/10 p-3">
            <Sparkles className="h-8 w-8 text-brand-500" />
          </div>
          <div>
            <h1 className="font-display text-4xl font-bold tracking-tight text-ink-600">
              发现
            </h1>
            <p className="mt-1 text-base text-ink-50">
              多源推荐：TMDb / 豆瓣 / Bangumi，可按需组合显示
            </p>
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          {sections.map((section) => {
            const active = selected.includes(section.key)
            return (
              <button
                key={section.key}
                type="button"
                onClick={() => toggleSection(section.key)}
                className={
                  'rounded-full border px-3 py-1.5 text-xs font-semibold transition ' +
                  (active
                    ? 'border-primary-400 bg-primary-400/15 text-brand-500'
                    : 'border-gray-200 bg-white text-gray-500 hover:border-primary-300 hover:text-ink-600')
                }
              >
                {section.label}
              </button>
            )
          })}
        </div>
      </header>

      {loading && <DiscoverSkeleton />}

      {!loading && error && (
        <div className="flex items-center gap-3 rounded-2xl border border-red-500/20 bg-red-500/10 p-4">
          <AlertTriangle className="h-5 w-5 flex-shrink-0 text-red-400" />
          <p className="text-red-300">{error}</p>
        </div>
      )}

      {!loading && selected.length === 0 && (
        <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center text-sand-500">
          至少选择一个推荐源，小宇宙才会开始转动。
        </div>
      )}

      {!loading && !error && selected.length > 0 && (
        <div className="space-y-10">
          {selected.map((key) => {
            const items = rows[key] ?? []
            if (items.length === 0) return null
            return (
              <ContentRow
                key={key}
                title={sectionMap.get(key)?.label ?? key}
                items={items}
              />
            )
          })}

          {!hasContent && (
            <div className="rounded-2xl border border-gray-200 bg-white p-10 text-center">
              <p className="text-sand-500">
                当前选择的推荐源暂未返回内容，可切换豆瓣 / Bangumi 或检查网络代理。
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function ContentRow({ title, items }: { title: string; items: DiscoverItem[] }) {
  return (
    <section className="space-y-4">
      <h2 className="pl-1 font-display text-2xl font-semibold text-ink-600">{title}</h2>
      <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
        {items.map((item, index) => (
          <DiscoverCard key={discoverKey(item, index)} item={item} />
        ))}
      </div>
    </section>
  )
}

function DiscoverCard({ item }: { item: DiscoverItem }) {
  const source = item.source || (item.bangumi_id ? 'bangumi' : item.douban_id ? 'douban' : 'tmdb')
  return (
    <div className="group relative overflow-hidden rounded-xl border border-gray-200 bg-gray-50 transition-all duration-300 hover:border-primary-500/30">
      <div className="relative aspect-[2/3] w-full overflow-hidden bg-surface-900">
        {item.poster_url ? (
          <img
            src={imageURL(item.poster_url)}
            alt={item.title}
            loading="lazy"
            referrerPolicy="no-referrer"
            className="h-full w-full object-cover transition-transform duration-500 group-hover:scale-105"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-xs text-gray-500">
            无海报
          </div>
        )}
        <div className="absolute left-1.5 top-1.5 rounded-xl border border-white/20 bg-black/65 px-1.5 py-0.5 text-[10px] font-semibold uppercase text-white backdrop-blur-sm">
          {source}
        </div>
        {(item.rating ?? 0) > 0 && (
          <div className="absolute right-1.5 top-1.5 rounded-xl border border-yellow-400/30 bg-black/70 px-1.5 py-0.5 text-[11px] font-semibold text-yellow-400 backdrop-blur-sm">
            ★ {(item.rating ?? 0).toFixed(1)}
          </div>
        )}
      </div>
      <div className="space-y-0.5 px-2.5 py-2">
        <p className="truncate text-xs font-medium text-ink-600 transition-colors group-hover:text-brand-500">
          {item.title}
        </p>
        <p className="text-[11px] text-sand-500">
          {[item.media_type, item.year && item.year > 0 ? item.year : ''].filter(Boolean).join(' · ') || '推荐'}
        </p>
      </div>
    </div>
  )
}

function DiscoverSkeleton() {
  return (
    <div className="space-y-8">
      {[1, 2, 3].map((section) => (
        <section key={section} className="space-y-4">
          <div className="h-8 w-48 animate-pulse rounded-xl bg-gray-100" />
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-7 xl:grid-cols-8">
            {[1, 2, 3, 4, 5, 6, 7, 8].map((item) => (
              <div key={item} className="aspect-[2/3] animate-pulse rounded-xl bg-gray-100" />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function discoverKey(item: DiscoverItem, index: number): string {
  return `${item.source || 'source'}:${item.tmdb_id || item.douban_id || item.bangumi_id || item.title}:${index}`
}

function readSavedSections(sections: DiscoverSection[]): string[] {
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    const allowed = new Set(sections.map((section) => section.key))
    return parsed.filter((key) => typeof key === 'string' && allowed.has(key))
  } catch {
    return []
  }
}

const defaultSectionDefs: DiscoverSection[] = [
  { key: 'tmdb_trending_day', label: 'TMDb 今日趋势', provider: 'tmdb' },
  { key: 'tmdb_popular_movie', label: 'TMDb 热门电影', provider: 'tmdb' },
  { key: 'douban_hot_movie', label: '豆瓣热门电影', provider: 'douban' },
  { key: 'douban_hot_tv', label: '豆瓣热门剧集', provider: 'douban' },
  { key: 'bangumi_calendar', label: 'Bangumi 每日放送', provider: 'bangumi' },
]
