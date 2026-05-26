import type { Media } from '../types'

/**
 * 把若干 Episode/Movie 行折叠成"剧集卡片"。
 *
 * 后端的 /api/media 等接口默认返回 episode 级行——同一部剧的每一集都
 * 是一行。在「最近添加」「海报墙」「收藏墙」这种以剧集为单位展示的页面
 * 我们要把它们合成一张代表卡片，避免同一海报刷屏。
 *
 * 折叠键优先级（命中第一个就分组）：
 *
 *   1. tmdb_id       （刮削匹配后最稳定）
 *   2. bangumi_id    （番剧）
 *   3. series_id     （后端某些场景下会预先聚合）
 *   4. library_id + title （fallback：同库同名视为同一剧）
 *
 * 同一组内取最早 created_at 的那条作为代表卡片，并带 count 表示集数。
 */
export type SeriesCard = { rep: Media; count: number }

export function groupSeries(items: Media[]): SeriesCard[] {
  const groups = new Map<string, SeriesCard>()
  for (const m of items) {
    let key = ''
    if (m.tmdb_id && m.tmdb_id > 0) key = `tmdb:${m.tmdb_id}`
    else if (m.bangumi_id && m.bangumi_id > 0) key = `bgm:${m.bangumi_id}`
    else if (m.series_id) key = `series:${m.series_id}`
    else key = `lib:${m.library_id}|${(m.title ?? '').toLowerCase().trim()}`

    const g = groups.get(key)
    if (!g) {
      groups.set(key, { rep: m, count: 1 })
    } else {
      g.count += 1
      const repHasPoster = !!g.rep.poster_url
      const curHasPoster = !!m.poster_url
      if (!repHasPoster && curHasPoster) {
        g.rep = m
      } else if (repHasPoster === curHasPoster) {
        const cur = (m.season_num ?? 0) * 10000 + (m.episode_num ?? 0)
        const rep = (g.rep.season_num ?? 0) * 10000 + (g.rep.episode_num ?? 0)
        if (cur > 0 && (rep === 0 || cur < rep)) g.rep = m
      }
    }
  }
  return Array.from(groups.values())
}
