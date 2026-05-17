import { useEffect, useState } from 'react'
import { Sparkles, AlertTriangle, ExternalLink } from 'lucide-react'
import { Link } from 'react-router-dom'

import { discoverAPI, type DiscoverItem } from '../api/discover'
import { imageURL } from '../api/client'

export function DiscoverPage() {
  const [trending, setTrending] = useState<DiscoverItem[]>([])
  const [popular, setPopular] = useState<DiscoverItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    
    Promise.all([
      discoverAPI.trending().catch((err) => {
        console.error('Failed to fetch trending:', err)
        return [] as DiscoverItem[]
      }),
      discoverAPI.popular().catch((err) => {
        console.error('Failed to fetch popular:', err)
        return [] as DiscoverItem[]
      }),
    ])
      .then(([t, p]) => {
        setTrending(t)
        setPopular(p)
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : 'Failed to load discover data')
      })
      .finally(() => setLoading(false))
  }, [])

  // Check if TMDB API key is likely not configured
  const isTMDBMissing = !loading && trending.length === 0 && popular.length === 0 && !error

  return (
    <div className="space-y-8 px-4 py-6 max-w-7xl mx-auto">
      {/* Header */}
      <header className="flex items-center gap-4 mb-8">
        <div className="p-3 rounded-2xl bg-gradient-to-br from-primary-500/20 to-primary-600/10 border border-primary-500/20">
          <Sparkles className="h-8 w-8 text-primary-400" />
        </div>
        <div>
          <h1 className="font-display text-4xl font-bold text-white tracking-tight">发现</h1>
          <p className="mt-1 text-base text-slate-400">
            来自 TMDb 的当日热门与流行榜单
          </p>
        </div>
      </header>

      {/* Error Alert */}
      {error && (
        <div className="rounded-2xl bg-red-500/10 border border-red-500/20 p-4 flex items-center gap-3">
          <AlertTriangle className="h-5 w-5 text-red-400 flex-shrink-0" />
          <p className="text-red-300">{error}</p>
        </div>
      )}

      {/* Loading State */}
      {loading && <DiscoverSkeleton />}

      {/* TMDB API Key Missing */}
      {isTMDBMissing && (
        <div className="rounded-2xl bg-amber-500/10 border border-amber-500/20 p-6 text-center space-y-4">
          <div className="mx-auto w-16 h-16 rounded-full bg-amber-500/10 flex items-center justify-center">
            <AlertTriangle className="h-8 w-8 text-amber-400" />
          </div>
          <h3 className="text-lg font-semibold text-white">TMDb API Key 未配置</h3>
          <p className="text-sm text-slate-400 max-w-md mx-auto">
            您需要配置 TMDb API Key 才能查看发现内容。请前往管理后台配置。
          </p>
          <Link
            to="/admin"
            className="inline-flex items-center gap-2 px-5 py-2.5 rounded-xl bg-primary-500/20 text-primary-400 hover:bg-primary-500/30 transition-colors font-medium"
          >
            前往管理后台
            <ExternalLink className="h-4 w-4" />
          </Link>
        </div>
      )}

      {/* Content Rows */}
      {!loading && !isTMDBMissing && (
        <div className="space-y-10">
          {trending.length > 0 && <ContentRow title="今日趋势" items={trending} />}
          {popular.length > 0 && <ContentRow title="热门电影" items={popular} />}
          
          {/* Empty State (TMDB configured but no data) */}
          {trending.length === 0 && popular.length === 0 && (
            <div className="text-center py-12">
              <p className="text-slate-500">暂无发现内容</p>
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
      <h2 className="font-display text-2xl font-semibold text-white pl-1">{title}</h2>
      <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
        {items.map((item) => (
          <DiscoverCard key={item.tmdb_id} item={item} />
        ))}
      </div>
    </section>
  )
}

function DiscoverCard({ item }: { item: DiscoverItem }) {
  return (
    <div className="group relative overflow-hidden rounded-2xl border border-white/5 bg-surface-800/60 hover:border-primary-500/30 transition-all duration-300 hover:shadow-2xl hover:shadow-primary-500/10 hover:-translate-y-1">
      {/* Poster */}
      <div className="aspect-[2/3] w-full bg-surface-900 relative overflow-hidden">
        {item.poster_url ? (
          <img
            src={imageURL(item.poster_url)}
            alt={item.title}
            loading="lazy"
            referrerPolicy="no-referrer"
            className="h-full w-full object-cover group-hover:scale-105 transition-transform duration-500"
          />
        ) : (
          <div className="flex h-full w-full items-center justify-center text-slate-600">
            无海报
          </div>
        )}
        
        {/* Rating Badge */}
        {item.rating > 0 && (
          <div className="absolute top-3 right-3 rounded-lg bg-black/70 backdrop-blur-sm px-2 py-1 text-sm font-semibold text-yellow-400 border border-yellow-400/30">
            ★ {item.rating.toFixed(1)}
          </div>
        )}
      </div>

      {/* Info */}
      <div className="p-4 space-y-1">
        <p className="font-medium text-white truncate group-hover:text-primary-400 transition-colors">
          {item.title}
        </p>
        {item.year > 0 && (
          <p className="text-sm text-slate-500">{item.year}</p>
        )}
      </div>
    </div>
  )
}

function DiscoverSkeleton() {
  return (
    <div className="space-y-8">
      {[1, 2].map((section) => (
        <section key={section} className="space-y-4">
          <div className="h-8 w-48 rounded-lg bg-surface-800 animate-pulse" />
          <div className="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
            {[1, 2, 3, 4, 5, 6].map((i) => (
              <div key={i} className="aspect-[2/3] rounded-2xl bg-surface-800 animate-pulse" />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}
