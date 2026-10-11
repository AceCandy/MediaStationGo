import { useSearchParams } from 'react-router-dom'
import { DoubanDataCenterPage } from './DoubanDataCenterPage'
import { TMDbDataCenterPage } from './TMDbDataCenterPage'
import { useAuthStore } from '../stores/auth'

export function DataCenterPage() {
  const [params, setParams] = useSearchParams()
  const session = useAuthStore(s => s.sessionVersion)
  const tmdb = params.get('provider') === 'tmdb'
  return <div className="space-y-4">
    <div className="flex gap-2" aria-label="资料来源">
      {['豆瓣', 'TMDb'].map((name, i) => <button key={name} className={(i === 1) === tmdb ? 'btn-primary' : 'btn-outline'} aria-pressed={(i === 1) === tmdb} onClick={() => setParams(i ? { provider: 'tmdb' } : {})}>{name}</button>)}
    </div>
    {tmdb ? <TMDbDataCenterPage key={`tmdb-${session}`} /> : <DoubanDataCenterPage key={`douban-${session}`} />}
  </div>
}
