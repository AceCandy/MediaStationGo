// 先启动 Web 预览，再运行 node scripts/check-discover-detail.mjs。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `discover-detail-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = (code) => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const route = (path, body) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(body))
const replaceRoute = (path, body) => {
  browser('network', 'unroute', `${base}/api/**`)
  browser('network', 'unroute', `${base}/api/${path}`)
  if (body === undefined) browser('network', 'route', `${base}/api/${path}`, '--abort')
  else route(path, body)
  route('**', {})
}
const wait = (code) => browser('wait', '--fn', code)
const click = (name) => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const identity = { tmdb_id: 123, media_type: 'tv' }
// 使用预览服务的静态图片，浏览器规范化路径后不经过 API mock。
const poster = '/api/../brand/mediastationgo-logo.svg'
const items = [
  { ...identity, source: 'tmdb', title: '测试整剧', year: 2026, rating: 8.2, overview: '列表简介', poster_url: poster },
  { ...identity, media_type: 'movie', source: 'tmdb', title: '测试电影', year: 2026, poster_url: poster },
]
const detail = { ...items[0], local_metadata: true, backdrop_url: poster, overview: '完整简介。'.repeat(20), release_date: '2026-09-01', runtime_minutes: [45, 50], genres: ['剧情'], countries: ['CN'], languages: ['zh'], douban_id: '456', tmdb_status: 'complete', credits: [{ person_id: 'local-1', name: '测试演员', role: '已翻译角色', type: 'Actor' }, { person_id: 'local-2', name: '测试导演', type: 'Director' }] }
const refreshedDetail = { ...detail, title: '刷新后整剧', rating: 8.7, credits: [{ person_id: 'local-3', name: '刷新演员', role: '新角色', type: 'Actor' }] }
try {
  browser('open', `${base}/login`)
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('discover/sections', { sections: [{ key: 'tmdb_trending_day', label: 'TMDb 今日趋势', provider: 'tmdb' }] })
  route('discover/feed?*', { tmdb_trending_day: items, _meta: { tmdb_trending_day: { page: 1, has_next: false } } })
  route('discover/library-status', { items: [identity] })
  route('discover/tmdb/tv/123/refresh', refreshedDetail)
  route('discover/tmdb/tv/123', detail)
  route('discover/tmdb/movie/123', { ...items[1], local_metadata: false, runtime_minutes: [], credits: [], genres: [], languages: [], countries: [] })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'admin',username:'测试用户',role:'admin',tier:'free'}},version:0}))`)
  browser('open', `${base}/discover`)
  wait(`document.querySelector('button[aria-label="查看测试整剧"]')?.innerText.includes('已入库')`)
  assert.ok(!evaluate(`document.querySelector('button[aria-label="查看测试电影"]').innerText.includes('已入库')`))
  assert.equal(evaluate(`performance.getEntriesByType('resource').filter(r=>r.name.includes('/discover/library-status')).length`), 1)
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r=>r.name.includes('/discover/tmdb/'))`))
  click('查看测试整剧')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('刷新后整剧')`)
  const text = evaluate(`document.querySelector('[role="dialog"]').innerText`)
  for (const value of ['2026-09-01', '单集', '45m / 50m', '类型流派', '剧情', '国家/地区', '中国', '语言', '中文', '刷新演员', '新角色', '8.7', '剧情简介']) assert.ok(text.includes(value), value)
  assert.ok(evaluate(`performance.getEntriesByType('resource').some(r=>r.name.includes('/discover/tmdb/tv/123/refresh'))`))
  assert.ok(evaluate(`document.querySelector('[role="dialog"] [aria-label="首播日期 2026-09-01"]') !== null`))
  assert.equal(evaluate(`document.querySelectorAll('[role="dialog"] [data-discover-backdrop]').length`), 1)
  assert.ok(evaluate(`document.querySelector('[data-discover-backdrop] img').complete`))
  assert.ok(!text.includes('Media ID') && !text.includes('本地路径') && !text.includes('GB'))
  assert.ok(!text.includes('Season') && !text.includes('第 1 季'))
  assert.deepEqual(evaluate(`Array.from(document.querySelectorAll('[role="dialog"] a')).map(a=>[a.href,a.target,a.rel])`), [
    ['https://www.themoviedb.org/tv/123', '_blank', 'noopener noreferrer'],
    ['https://movie.douban.com/subject/456/', '_blank', 'noopener noreferrer'],
  ])
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const [width, height] of [[390, 844], [768, 1024], [1024, 900], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('[role="dialog"]').scrollWidth<=document.querySelector('[role="dialog"]').clientWidth`), `${theme} ${width}`)
      if (process.env.DISCOVER_SCREENSHOT_DIR) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/${theme}-${width}.png`)
    }
  }
  browser('press', 'Escape')
  wait(`document.querySelector('[role="dialog"]') === null`)
  click('查看测试电影')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('暂无演职员资料')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"] [aria-label="上映日期 2026 年"]') !== null`))
  assert.equal(evaluate(`document.querySelectorAll('[role="dialog"] [data-discover-backdrop]').length`), 0)
  assert.equal(evaluate(`document.querySelectorAll('[role="dialog"] a').length`), 1)
  click('关闭详情')
  replaceRoute('discover/tmdb/tv/123')
  click('查看测试整剧')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('完整资料暂时不可用')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('列表简介')`))
  replaceRoute('discover/tmdb/tv/123', { ...detail, local_metadata: false })
  click('重试详情')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('测试演员')`)
  click('关闭详情')
  replaceRoute('discover/library-status', { items: [] })
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'viewer',username:'另一用户',role:'user',tier:'free'}},version:0}))`)
  browser('open', `${base}/discover`)
  wait(`document.querySelector('button[aria-label="查看测试整剧"]') !== null && performance.getEntriesByType('resource').some(r=>r.name.includes('/discover/library-status'))`)
  assert.ok(!evaluate(`document.querySelector('button[aria-label="查看测试整剧"]').innerText.includes('已入库')`))
  assert.equal(browser('errors').trim(), '')
  console.log('发现弹窗：资料、链接、任意分集标记、按需请求、错误重试、账户隔离、双主题响应式检查通过')
} catch (err) {
  console.error(evaluate(`({path:location.pathname,text:document.body.innerText.slice(-2000)})`))
  console.error(browser('errors'))
  throw err
} finally {
  browser('close')
}
