import { lazy, Suspense, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMediaAccessKey } from '../hooks/useMediaAccessKey'

const CatalogDiscover = lazy(() => import('./DiscoverPage').then((m) => ({ default: m.DiscoverPage })))
const HongGuoDiscover = lazy(() => import('./HongGuoPage').then((m) => ({ default: m.HongGuoPage })))

export function DiscoverHubPage() {
  const [params, setParams] = useSearchParams()
  const accessKey = useMediaAccessKey()
  const values = params.getAll('system')
  const system = values.length === 1 && values[0] === 'hongguo' ? 'hongguo' : 'catalog'
  useEffect(() => {
    if (values.length > 1 || (values.length === 1 && values[0] !== system)) {
      const next = new URLSearchParams(params)
      next.set('system', system)
      setParams(next, { replace: true })
    }
  }, [params, setParams, system, values])
  return <div className="space-y-6">
    <nav aria-label="发现资料体系" className="tab-list">
      {([['catalog', 'TMDB/豆瓣/Bangumi'], ['hongguo', '红果短剧']] as const).map(([value, label]) => <button
        key={value} type="button" aria-pressed={system === value}
        className="tab-item"
        onClick={() => { if (value !== system) { const next = new URLSearchParams(params); next.set('system', value); setParams(next) } }}
      >{label}</button>)}
    </nav>
    <Suspense fallback={<p role="status" className="py-12 text-center text-ink-50">加载发现内容…</p>}>
      {system === 'hongguo' ? <HongGuoDiscover key={`hongguo:${accessKey}`} /> : <CatalogDiscover key={`catalog:${accessKey}`} />}
    </Suspense>
  </div>
}
