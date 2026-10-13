import { useState } from 'react'
import type { MediaCredit } from '../types'
import { MediaCredits } from './MediaDetailCast'

// 已随详情返回的来源人员只分批展示，不重复请求详情或图片列表。
export function CatalogCredits({ credits }: { credits: MediaCredit[] }) {
  const cast = credits.filter(v => ['', 'actor', 'gueststar', 'cast'].includes(v.type.toLowerCase()))
  const crew = credits.filter(v => !['', 'actor', 'gueststar', 'cast'].includes(v.type.toLowerCase()))
  return <div className="min-w-0 space-y-6"><LocalCreditRow title="演员" credits={cast} /><LocalCreditRow title="幕后" credits={crew} /></div>
}

function LocalCreditRow({ title, credits }: { title: string; credits: MediaCredit[] }) {
  const [count, setCount] = useState(20)
  return <MediaCredits progressive title={title} credits={credits.slice(0, count)} total={credits.length} onLoadMore={count < credits.length ? () => setCount(count + 20) : undefined} />
}
