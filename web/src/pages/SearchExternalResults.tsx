import { useState } from 'react'
import { Info } from 'lucide-react'

import type { ExternalMediaResult } from '../api/ai'
import { imageURL } from '../api/client'
import { ModalShell } from '../components/ModalShell'

export function ExternalResults({ items }: { items: ExternalMediaResult[] }) {
  const [detail, setDetail] = useState<ExternalMediaResult | null>(null)

  return (
    <section className="space-y-3">
      <div>
        <h2 className="font-display text-xl font-semibold text-ink-600">外部数据源</h2>
        <p className="text-xs text-ink-50">来自 TMDb / 豆瓣 / Bangumi 的普通搜索结果。</p>
      </div>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {items.map((item) => {
          const key = `${item.source}:${item.tmdb_id ?? item.bangumi_id ?? item.douban_id ?? item.title}`
          return (
            <article
              key={key}
              role="button"
              tabIndex={0}
              onClick={() => setDetail(item)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') setDetail(item)
              }}
              className="glass-panel flex cursor-pointer gap-3 !p-3 transition hover:-translate-y-0.5 hover:shadow-lg"
            >
              <div className="h-28 w-20 shrink-0 overflow-hidden rounded-xl bg-gray-100">
                {item.poster_url ? (
                  <img src={imageURL(item.poster_url)} alt={item.title} className="h-full w-full object-cover" />
                ) : null}
              </div>
              <div className="min-w-0 flex-1">
                <div className="mb-1 flex flex-wrap items-center gap-2">
                  <span className="rounded-full bg-primary-400/10 px-2 py-0.5 text-[10px] uppercase text-brand-500">
                    {item.source}
                  </span>
                  {item.media_type && <span className="text-xs text-sand-500">{item.media_type}</span>}
                  {item.year ? <span className="text-xs text-sand-500">{item.year}</span> : null}
                  {item.rating ? <span className="text-xs text-amber-500">★ {item.rating.toFixed(1)}</span> : null}
                </div>
                <h3 className="truncate font-semibold text-ink-600">{item.title}</h3>
                <p className="mt-1 line-clamp-3 text-xs text-ink-50">{item.overview || '暂无简介。'}</p>
                <p className="mt-3 text-xs font-semibold text-brand-500">查看详情</p>
              </div>
            </article>
          )
        })}
      </div>
      {detail && <ExternalDetailModal item={detail} onClose={() => setDetail(null)} />}
    </section>
  )
}

function ExternalDetailModal({ item, onClose }: { item: ExternalMediaResult; onClose: () => void }) {
  return (
    <ModalShell onClose={onClose} maxWidth="max-w-3xl" className="max-h-[88vh] overflow-y-auto" ariaLabel={item.title}>
      <div className="grid gap-0 md:grid-cols-[220px,1fr]">
        <div className="min-h-72 bg-gray-100">
          {item.poster_url ? (
            <img src={imageURL(item.poster_url)} alt={item.title} className="h-full w-full object-cover" />
          ) : (
            <div className="flex h-full min-h-72 items-center justify-center text-brand-500">
              <Info size={42} />
            </div>
          )}
        </div>
        <div className="space-y-4 p-5">
          <div>
            <div className="mb-2 flex flex-wrap gap-2 text-xs">
              <span className="rounded-full bg-primary-400/10 px-2 py-0.5 font-semibold uppercase text-brand-500">{item.source}</span>
              {item.media_type ? <span className="rounded-full bg-gray-100 px-2 py-0.5 text-ink-100">{item.media_type}</span> : null}
              {item.year ? <span className="rounded-full bg-gray-100 px-2 py-0.5 text-ink-100">{item.year}</span> : null}
              {item.rating ? <span className="rounded-full bg-amber-50 px-2 py-0.5 text-amber-600">★ {item.rating.toFixed(1)}</span> : null}
            </div>
            <h3 className="font-display text-2xl font-bold text-ink-600">{item.title}</h3>
            <p className="mt-2 text-sm leading-6 text-ink-50">{item.overview || '暂无简介。'}</p>
          </div>
          <div className="flex justify-end pt-2">
            <button onClick={onClose} className="btn-outline px-4 py-2 shadow-none">关闭</button>
          </div>
        </div>
      </div>
    </ModalShell>
  )
}
