import { Film, Star, UserRound } from 'lucide-react'
import { useState } from 'react'
import { imageURL } from '../api/client'

export function CatalogPosterCard({ title, image, version, year, rating, person = false, owned = false, onClick }: {
  title: string; image?: string; version?: string; year?: string; rating?: number; person?: boolean; owned?: boolean; onClick: () => void
}) {
  const src = imageURL(image, version, { maxWidth: 320 })
  const [failed, setFailed] = useState('')
  return <button type="button" aria-label={`查看${title}`} onClick={onClick} className="group w-full min-w-0 text-left">
    <div className={`relative flex ${person ? 'aspect-square' : 'aspect-[2/3]'} items-center justify-center overflow-hidden rounded-xl bg-[var(--app-glass)] shadow-poster transition group-hover:shadow-poster-hover group-focus-visible:ring-2 group-focus-visible:ring-brand-500 ${person ? 'p-3' : ''}`}>
      {src && failed !== src ? <img src={src} alt={title} loading="lazy" decoding="async" className={`h-full w-full object-cover transition duration-300 motion-reduce:transition-none group-hover:scale-[1.03] ${person ? 'rounded-full object-top' : ''}`} onError={() => setFailed(src)} /> : <div aria-label={person ? '暂无头像' : '暂无海报'} className="flex flex-col items-center gap-2 text-sm text-ink-50">{person ? <UserRound size={32} /> : <Film size={32} />}暂无{person ? '头像' : '海报'}</div>}
      {!!rating && <span className="absolute right-2 top-2 flex items-center gap-1 rounded-lg bg-black/55 px-2 py-1 text-xs text-gold-500 backdrop-blur"><Star size={12} fill="currentColor" />{rating.toFixed(1)}</span>}
      {owned && <span className="absolute bottom-2 left-2 rounded bg-black/55 px-2 py-1 text-xs text-white backdrop-blur">已有本地媒体</span>}
    </div>
    <p className="mt-2 truncate text-sm font-medium text-ink-600">{title}</p>
    {year && <p className="mt-1 truncate text-xs text-ink-50">{year}</p>}
  </button>
}
