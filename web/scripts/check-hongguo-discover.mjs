// 先启动本地 Web 预览，再运行：node scripts/check-hongguo-discover.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'
import { URLSearchParams } from 'node:url'

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
  artwork_id: i === 2 ? '' : `poster-${i}`, tags: ['都市', '成长', '家庭'],
  update_text: `全${60 + i}集`, episode_count: 60 + i, rating: 8.5,
}))

try {
  visit('/login')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'user', is_super: false })
  route('play-profiles', [])
  route('catalogs/hongguo/works?*', { items, total: 51 })
  route('catalogs/hongguo/status', { enabled: true })
  route('discover/sections', { sections: [] })
  // 模拟图片损坏，验证占位回退；真实本地图片另列部署验收。
  route('catalogs/hongguo/artwork/*', {})
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'viewer',username:'测试观众',role:'user',tier:'free'}},version:0}))`)
  visit('/hongguo?keyword=测试&page=2')
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]') !== null`)
  const state = evaluate(`({path:location.pathname,query:location.search,links:[...document.querySelectorAll('a')].map(a=>a.getAttribute('href')),requests:performance.getEntriesByType('resource').map(r=>r.name),cards:document.querySelectorAll('button[aria-label^="查看"]').length})`)
  assert.equal(state.path, '/discover')
  const query = new URLSearchParams(state.query)
  assert.equal(query.get('system'), 'hongguo')
  assert.equal(query.get('keyword'), '测试')
  assert.equal(query.get('page'), '2')
  assert.equal(state.cards, 18)
  assert.ok(!state.links.includes('/hongguo'))
  assert.equal(state.requests.filter((url) => url.includes('/catalogs/hongguo/works?')).length, 1)
  assert.ok(!state.requests.some((url) => url.includes('/discover/sections') || url.includes('/discover/feed')))
  assert.ok(!state.requests.some((url) => /\/works\/\d+/.test(url)))
  waitFor(`document.querySelector('button[aria-label="查看长风渡山河"]').innerText.includes('暂无海报')`)

  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1024, 1280, 1440]) {
      browser('set', 'viewport', String(width), '900')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} overflow`)
      if (process.env.DISCOVER_SCREENSHOT_DIR) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/${theme}-${width}.png`)
    }
  }
  browser('find', 'role', 'button', 'click', '--name', '现有资料体系', '--exact')
  waitFor(`new URLSearchParams(location.search).get('system') === 'catalog' && document.body.innerText.includes('多源推荐')`)
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
  console.log('红果发现：旧链接、参数、按需加载、体系切换、权限与双主题响应式检查通过')
} catch (error) {
  console.error(evaluate(`({path:location.pathname, text:document.body.innerText.slice(0, 1500), requests:performance.getEntriesByType('resource').map(r=>r.name)})`))
  throw error
} finally {
  browser('close')
}
