import { lazy, Suspense, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'
import { useAuthStore } from '../stores/auth'

const CatalogDiscover = lazy(() => import('./DiscoverPage').then((m) => ({ default: m.DiscoverPage })))
const HongGuoDiscover = lazy(() => import('./HongGuoPage').then((m) => ({ default: m.HongGuoPage })))

const HuangGuoAIDiscover = lazy(() => import('./HuangGuoAIPage').then(m => ({ default: m.HuangGuoAIPage })))
const TMDbData = lazy(() => import('./TMDbDataCenterPage').then(m => ({ default: m.TMDbDataCenterPage })))
const DoubanData = lazy(() => import('./DoubanDataCenterPage').then(m => ({ default: m.DoubanDataCenterPage })))
const sources = [['tmdb', 'TMDB'], ['douban', '豆瓣'], ['hongguo', '红果短剧'], ['huangguoai', '黄果 AI'], ['bangumi', 'Bangumi']] as const

export function DiscoverHubPage() {
  const [params, setParams] = useSearchParams()
  const accessKey = useMediaAccessKey()
  const session = useAuthStore(s => s.sessionVersion)
  const values = params.getAll('system')
  const system = values.length === 1 && sources.some(([value]) => value === values[0]) ? values[0] : 'tmdb'
  const hasData = system === 'tmdb' || system === 'douban'
  const section = params.getAll('section').length === 1 && params.get('section') === 'rank' ? 'rank' : 'data'
  useEffect(() => {
    const next = new URLSearchParams(params)
    next.set('system', system)
    next.delete('provider')
    if (hasData) {
      if (section === 'rank') next.set('section', 'rank'); else next.delete('section')
    }
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }, [params, setParams, system, hasData, section])
  const contentKey = `${system}:${hasData ? section : ''}:${accessKey}:${session}`
  return <div className="space-y-6">
    <nav aria-label="发现资料体系" className="tab-list">
      {sources.map(([value, label]) => <button
        key={value} type="button" aria-pressed={system === value}
        className="tab-item"
        onClick={() => { if (value !== system) setParams({ system: value }) }}
      >{label}</button>)}
    </nav>
    {hasData && <nav aria-label="来源内容" className="tab-list w-fit">{([['data', '分类'], ['rank', '榜单']] as const).map(([value, label]) => <button key={value} type="button" className="tab-item" aria-pressed={section === value} onClick={() => { if (value !== section) setParams(value === 'rank' ? { system, section: value } : { system }) }}>{label}</button>)}</nav>}
    {system === 'bangumi' && <nav aria-label="来源内容" className="tab-list w-fit"><button type="button" className="tab-item" aria-pressed="true">榜单</button></nav>}
    <Suspense fallback={<p role="status" className="py-12 text-center text-ink-50">加载发现内容…</p>}>
      {system === 'huangguoai' ? <HuangGuoAIDiscover key={contentKey} /> : system === 'hongguo' ? <HongGuoDiscover key={contentKey} /> : system === 'tmdb' && section === 'data' ? <TMDbData key={contentKey} /> : system === 'douban' && section === 'data' ? <DoubanData key={contentKey} /> : <CatalogDiscover key={contentKey} provider={system as 'tmdb' | 'douban' | 'bangumi'} />}
    </Suspense>
  </div>
}
