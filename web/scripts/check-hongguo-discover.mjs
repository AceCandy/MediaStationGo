// 先启动本地 Web 预览，再运行：node scripts/check-hongguo-discover.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'
import { URL, URLSearchParams } from 'node:url'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `hongguo-discover-check-${process.pid}`
function browser(...args) {
  return execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
}
function evaluate(code) {
  const response = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(response.success, true, response.error)
  return response.data.result
}
function route(path, data) { browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data)) }
function visit(path) { browser('open', base + path) }
function waitFor(code) { browser('wait', '--fn', code) }
const titles = ['长风渡山河', '盛夏的第七封信', '月色不负归人', '山海之间', '许你万家灯火', '这一刻的重逢']
const items = Array.from({ length: 18 }, (_, i) => ({
  id: `work-${i}`, source_id: `${90001 + i}`, title: titles[i % titles.length], kind: 'series',
  source_category: 'real-drama',
  artwork_id: i === 2 ? '' : `poster-${i}`, tags: ['都市', '成长', '家庭'],
  update_text: `全${60 + i}集`, episode_count: 60 + i, rating: 8.5, hydrated: i !== 0,
  downloaded: i === 0 || i === 1,
}))

try {
  visit('/login')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'user', is_super: false })
  route('play-profiles', [])
  route('catalogs/hongguo/works?keyword=%E6%B5%8B%E8%AF%95&*', { items, total: 18 })
  route('catalogs/hongguo/works?*', { items, total: 51 })
  route('catalogs/hongguo/search?*', { items, total: 18 })
  route('catalogs/hongguo/status', { enabled: true })
  route('catalogs/hongguo/works/90001', { ...items[0], overview: '弹窗测试简介', first_visible_at: '2026-09-01T00:00:00Z', tags: ['都市'], artwork: [], credits: [{ person_id: 'cast-1', subtitle: '测试角色', person: { name: '测试演员', source_id: '1' } }], episodes: [] })
  route('catalogs/hongguo/works/90001/favorite', { favorite: false })
  route('discover/sections', { sections: [{ key: 'tmdb_trending_day', label: 'TMDb 今日趋势', provider: 'tmdb' }] })
  route('discover/feed?*', { tmdb_trending_day: [], _meta: { tmdb_trending_day: { page: 1, has_next: false } } })
  // 模拟图片损坏，验证占位回退；真实本地图片另列部署验收。
  route('catalogs/hongguo/artwork/*', {})
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'viewer',username:'测试观众',role:'user',tier:'free'}},version:0}))`)
  visit('/hongguo?keyword=测试&page=2')
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]') !== null`)
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('/catalogs/hongguo/search?')) && performance.getEntriesByType('resource').some(r=>r.name.includes('/catalogs/hongguo/works?'))`)
  const state = evaluate(`({path:location.pathname,query:location.search,links:[...document.querySelectorAll('a')].map(a=>a.getAttribute('href')),requests:performance.getEntriesByType('resource').map(r=>r.name),cards:document.querySelectorAll('button[aria-label^="查看"]').length})`)
  assert.equal(state.path, '/discover')
  const query = new URLSearchParams(state.query)
  assert.equal(query.get('system'), 'hongguo')
  assert.equal(query.get('keyword'), '测试')
  assert.equal(query.get('page'), '2')
  assert.equal(state.cards, 18)
  assert.equal(evaluate(`document.querySelectorAll('[data-hongguo-downloaded]').length`), 2)
  assert.ok(!state.requests.some((url) => url.includes('/downloads')))
  assert.ok(evaluate(`document.querySelector('button[aria-label="查看长风渡山河"]').innerText.includes('真人剧')`))
  assert.ok(!state.links.includes('/hongguo'))
  assert.equal(state.requests.filter((url) => url.includes('/catalogs/hongguo/search?')).length, 1)
  assert.equal(state.requests.filter((url) => url.includes('/catalogs/hongguo/works?')).length, 1)
  assert.ok(state.requests.some((url) => url.includes('/catalogs/hongguo/works?') && new URL(url).searchParams.get('page') === '1'))
  assert.ok(!state.requests.some((url) => url.includes('/discover/sections') || url.includes('/discover/feed')))
  assert.ok(!state.requests.some((url) => /\/works\/\d+/.test(url)))
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]').innerText.includes('暂无海报')`)
  assert.ok(evaluate(`document.querySelector('button[aria-label="查看长风渡山河"]').innerText.includes('待补齐')`))
  const detailRequestsBefore = evaluate(`performance.getEntriesByType('resource').filter(r => r.name.includes('/catalogs/hongguo/works/')).length`)
  browser('find', 'role', 'button', 'click', '--name', '查看长风渡山河', '--exact')
  waitFor(`document.querySelector('[role="dialog"]')?.innerText.includes('弹窗测试简介')`)
  assert.ok(evaluate(`performance.getEntriesByType('resource').filter(r => r.name.includes('/catalogs/hongguo/works/')).length`) > detailRequestsBefore)
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看"]').length`), 18)
  assert.equal(evaluate(`performance.getEntriesByType('resource').filter(r => r.name.includes('/catalogs/hongguo/search?')).length`), 1)
  assert.ok(!evaluate(`document.querySelector('[role="dialog"]').innerText.includes('本地媒体与 STRM')`))
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r => r.name.includes('/works/') && r.name.includes('/media'))`))
  assert.ok(evaluate(`document.querySelector('[role="dialog"] [aria-label="评分 8.5"]')?.classList.contains('badge-gold')`))
  assert.ok(evaluate(`document.querySelector('[role="dialog"] [aria-label^="红果上线"]') !== null`))
  assert.ok(evaluate(`document.querySelector('[role="dialog"] .overflow-x-auto')?.innerText.includes('测试演员')`))
  for (const width of [390, 640, 768, 1024, 1440]) {
    browser('set', 'viewport', String(width), '900')
    waitFor(`document.querySelector('[role="dialog"]').getAnimations({subtree:true}).every(animation => animation.playState !== 'running')`)
    assert.ok(evaluate(`document.querySelector('[role="dialog"]').scrollWidth <= document.querySelector('[role="dialog"]').clientWidth`))
    if (width >= 1024) assert.equal(evaluate(`Math.round(document.querySelector('[role="dialog"] [class*="aspect-"]').getBoundingClientRect().width)`), 260)
    if (process.env.DISCOVER_SCREENSHOT_DIR && (width === 390 || width === 1440)) {
      for (const theme of ['dark', 'light']) {
        evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
        browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/hongguo-modal-${theme}-${width}.png`)
      }
    }
  }
  browser('press', 'Escape')
  waitFor(`!document.querySelector('[role="dialog"]') && !new URLSearchParams(location.search).has('id')`)
  assert.equal(evaluate(`performance.getEntriesByType('resource').filter(r => r.name.includes('/catalogs/hongguo/search?')).length`), 1)
  assert.equal(evaluate(`document.activeElement?.getAttribute('aria-label')`), '查看长风渡山河')
  visit('/discover?system=hongguo&source=real-drama&category=都市')
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('source_category=real-drama')&&r.name.includes('category=%E9%83%BD%E5%B8%82')&&r.name.includes('page=1'))`)
  assert.ok(!evaluate(`document.querySelector('button[aria-label="查看长风渡山河"]').innerText.includes('真人剧')`))
  assert.ok(evaluate(`document.querySelector('button[aria-label="查看长风渡山河"]').innerText.includes('都市 · 成长')`))
  browser('scrollintoview', '[data-testid="hongguo-load-more"]')
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('source_category=real-drama')&&r.name.includes('category=%E9%83%BD%E5%B8%82')&&r.name.includes('page=2'))`)
  assert.ok(!evaluate(`document.body.innerText.includes('下一页')`))
  assert.equal(evaluate(`[...document.querySelectorAll('button')].some(b=>b.innerText==='漫画')`), false)
  assert.equal(evaluate(`[...document.querySelectorAll('button')].some(b=>b.innerText==='其它')`), true)
  browser('find', 'role', 'button', 'click', '--name', '漫剧', '--exact')
  waitFor(`new URLSearchParams(location.search).get('source') === 'comic-drama' && !new URLSearchParams(location.search).has('category') && [...document.querySelectorAll('button')].some(b=>b.innerText==='脑洞') && ![...document.querySelectorAll('button')].some(b=>b.innerText==='都市')`)
  assert.deepEqual(evaluate(`[...document.querySelectorAll('button')].filter(b=>['都市','脑洞'].includes(b.innerText)).map(b=>b.innerText)`), ['脑洞'])
  browser('find', 'role', 'button', 'click', '--name', '脑洞', '--exact')
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('source_category=comic-drama')&&r.name.includes('category=%E8%84%91%E6%B4%9E'))`)
  browser('find', 'role', 'button', 'click', '--name', 'AI剧', '--exact')
  waitFor(`new URLSearchParams(location.search).get('source') === 'ai-drama' && !new URLSearchParams(location.search).has('category')`)
  browser('find', 'role', 'button', 'click', '--name', '榜单', '--exact')
  waitFor(`new URLSearchParams(location.search).get('section') === 'rank' && new URLSearchParams(location.search).get('rank') === 'hot-drama' && !new URLSearchParams(location.search).has('source') && performance.getEntriesByType('resource').some(r=>r.name.includes('rank=hot-drama'))`)
  assert.ok(!evaluate(`performance.getEntriesByType('resource').filter(r=>r.name.includes('rank=hot-drama')).some(r=>r.name.includes('sort=')||r.name.includes('source_category='))`))
  browser('find', 'role', 'button', 'click', '--name', '选择红果榜单', '--exact')
  assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="option"]')].map(o=>o.innerText)`), ['红果热播榜', '真人剧热播榜', 'AI剧热播榜', '漫剧热播榜'])
  browser('find', 'role', 'option', 'click', '--name', 'AI剧热播榜', '--exact')
  waitFor(`new URLSearchParams(location.search).get('rank') === 'hot-ai-drama' && performance.getEntriesByType('resource').some(r=>r.name.includes('rank=hot-ai-drama'))`)

  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1024, 1280, 1440]) {
      browser('set', 'viewport', String(width), '900')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} overflow`)
      assert.ok(evaluate(`[...document.querySelectorAll('[data-hongguo-downloaded]')].every(b => { const a=b.previousElementSibling; if (!a) return true; const x=a.getBoundingClientRect(), y=b.getBoundingClientRect(); return x.right<=y.left || x.bottom<=y.top || y.bottom<=x.top; })`), `${theme}/${width} badge overlap`)
      if (process.env.DISCOVER_SCREENSHOT_DIR) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/${theme}-${width}.png`)
    }
  }
  browser('find', 'role', 'button', 'click', '--name', 'TMDB/豆瓣/Bangumi', '--exact')
  waitFor(`new URLSearchParams(location.search).get('system') === 'catalog' && document.querySelector('button[aria-label="选择榜单"]') !== null`)
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看"]').length`), 0)
  browser('find', 'role', 'button', 'click', '--name', '红果短剧', '--exact')
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]') !== null`)
  visit('/discover?system=hongguo&system=invalid')
  waitFor(`location.search === '?system=catalog'`)
  assert.ok(!evaluate(`document.body.innerText.includes('页面加载失败')`))
  assert.equal(browser('errors').trim(), '')

  browser('network', 'unroute')
  route('auth/permissions', { permissions: { can_view_discover: false }, role: 'user', is_super: false })
  route('play-profiles', [])
  route('**', {})
  visit('/hongguo?id=90001')
  waitFor(`location.pathname === '/'`)
  assert.equal(evaluate(`document.querySelector('nav[aria-label="发现资料体系"]') === null`), true)
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r=>r.name.includes('/catalogs/hongguo/works'))`))

  browser('network', 'unroute')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('catalogs/hongguo/search?*', { items: [items[0]], total: 1 })
  route('catalogs/hongguo/works?*', { items: [{ ...items[0], source_category: '', hydrated: false }], total: 1 })
  route('catalogs/hongguo/status', { enabled: true })
  route('catalogs/hongguo/works/90001/refresh', {})
  route('catalogs/hongguo/works/90001/category', {})
  route('catalogs/hongguo/works/90001/media?*', { items: [], total: 0 })
  route('catalogs/hongguo/works/90001/favorite', { favorite: false })
  route('catalogs/hongguo/works/90001', { ...items[0], overview: '补录后的简介', artwork: [], credits: [], tags: [] })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'admin',username:'测试管理',role:'admin',tier:'free'}},version:0}))`)
  visit('/discover?system=hongguo&keyword=长风')
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]') !== null`)
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r=>new URL(r.name).pathname.endsWith('/works/90001/refresh'))`))
  browser('find', 'role', 'button', 'click', '--name', '查看长风渡山河', '--exact')
  waitFor(`document.body.innerText.includes('补录后的简介') || document.body.innerText.includes('页面加载失败')`)
  assert.ok(evaluate(`new URLSearchParams(location.search).get('id') === '90001' && document.body.innerText.includes('补录后的简介')`))
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r=>new URL(r.name).pathname.endsWith('/works/90001/refresh'))`))
  assert.ok(evaluate(`!document.querySelector('[role="dialog"]').innerText.match(/收藏|刷新资料|下载空间/)`))
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('下载已更新分集')`))
  assert.ok(evaluate(`document.querySelector('[aria-label="设置作品分类"]') === null`))
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r=>new URL(r.name).pathname.endsWith('/works/90001/favorite'))`))
  browser('network', 'unroute', `${base}/api/catalogs/hongguo/works/90001`)
  browser('network', 'unroute', `${base}/api/**`)
  route('catalogs/hongguo/works/90001', { ...items[0], source_category: 'other', overview: '补录后的简介', artwork: [], credits: [], tags: [] })
  route('**', {})
  visit('/discover?system=hongguo&source=other')
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]') !== null && new URLSearchParams(location.search).get('source') === 'other'`)
  assert.ok(evaluate(`performance.getEntriesByType('resource').some(r=>r.name.includes('source_category=other'))`))
  browser('find', 'role', 'button', 'click', '--name', '查看长风渡山河', '--exact')
  waitFor(`document.querySelector('[role="dialog"]')?.innerText.includes('补录后的简介')`)
  browser('find', 'role', 'button', 'click', '--name', '设置作品分类', '--exact')
  browser('find', 'role', 'option', 'click', '--name', 'AI剧', '--exact')
  waitFor(`document.querySelector('[data-rht-toaster] [role="status"]')?.innerText.includes('分类已保存')`)
  assert.ok(evaluate(`document.querySelector('[aria-label="设置作品分类"]') === null`))
  assert.ok(evaluate(`performance.getEntriesByType('resource').some(r=>new URL(r.name).pathname.endsWith('/works/90001/category'))`))
  assert.equal(evaluate(`document.querySelector('[role="dialog"]') !== null && document.querySelector('button[aria-label="查看长风渡山河"]') === null`), true)
  browser('network', 'unroute')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('libraries/hongguo-test', { id: 'hongguo-test', name: '测试红果库', type: 'hongguo' })
  route('catalogs/hongguo/libraries/hongguo-test?*', { items: [{ ...items[0], id: 'library-work' }], total: 1 })
  route('catalogs/hongguo/works/90001/media?*', { items: [{ id: 'test-episode', title: '第一集', season_num: 1, episode_num: 1, relative_path: 'test.strm' }], total: 1 })
  route('catalogs/hongguo/works/90001', { ...items[0], overview: '库内简介', artwork: [], credits: [], tags: [] })
  route('**', {})
  visit('/library/hongguo-test')
  waitFor(`document.body.innerText.includes('测试红果库') && document.querySelector('button.card') !== null`)
  browser('click', 'button.card')
  waitFor(`document.body.innerText.includes('播放 · S1E001')`)
  assert.equal(evaluate(`location.pathname`), '/library/hongguo-test')
  assert.equal(evaluate(`new URLSearchParams(location.search).get('hongguo_id')`), '90001')
  assert.equal(evaluate(`document.querySelector('[role="dialog"]')`), null)
  assert.ok(evaluate(`document.querySelector('a[href="/play/test-episode"]') !== null`))
  browser('find', 'role', 'button', 'click', '--name', '返回媒体库', '--exact')
  waitFor(`!new URLSearchParams(location.search).has('hongguo_id') && document.querySelector('button.card') !== null`)
  console.log('红果发现：旧链接、参数、按需加载、体系切换、权限与双主题响应式检查通过')
} catch (error) {
  console.error(browser('errors'))
  console.error(browser('console'))
  console.error(evaluate(`({path:location.pathname, text:document.body.innerText.slice(0, 1500), requests:performance.getEntriesByType('resource').map(r=>r.name)})`))
  throw error
} finally {
  browser('close')
}
