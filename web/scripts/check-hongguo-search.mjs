// 复用预览服务，模拟全部 API；不访问真实搜索/队列。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `hongguo-search-${process.pid}`
function browser(...args) { return execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 }) }
function evaluate(code) {
  const response = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(response.success, true, response.error)
  return response.data.result
}
function waitFor(code) { browser('wait', '--fn', code) }
function click(name) { browser('find', 'role', 'button', 'click', '--name', name, '--exact') }
function route(path, data) { browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data)) }
function search(keyword) { browser('find', 'role', 'searchbox', 'fill', '--name', '搜索红果资料', keyword); click('搜索') }
const work = (index) => ({ id: `work-${index}`, source_id: String(92000 + index), title: `匹配作品${index}`, hydrated: true, kind: 'series', source_category: 'real-drama', artwork_id: '', tags: ['都市'], rating: 8, episode_count: 20 })
const local = Array.from({ length: 52 }, (_, i) => work(i))
const remote = [ { ...work(0), title: '官网摘要0', hydrated: false, downloaded: true }, { ...work(1), title: '官网完整作品1' }, ...Array.from({ length: 8 }, (_, i) => work(100 + i)) ]
local[1] = { ...local[1], title: '本地待补齐1', hydrated: false, downloaded: true }
try {
  browser('open', `${base}/login`)
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('catalogs/hongguo/status', { enabled: true })
  route('catalogs/hongguo/search?keyword=empty', { items: [], total: 0 })
  route('catalogs/hongguo/search?keyword=local-only', { items: [], total: 0 })
  route('catalogs/hongguo/search?*', { items: remote, total: 10 })
  route('catalogs/hongguo/works?keyword=empty&*', { items: [], total: 0 })
  route('catalogs/hongguo/works?*page=1&page_size=50', { items: local.slice(0, 50), total: 52 })
  route('catalogs/hongguo/works?*page=2&page_size=50', { items: local.slice(50), total: 52 })
  route('catalogs/hongguo/works/92000', { ...work(0), overview: '本地详情', artwork: [], credits: [], tags: [] })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'test-token',user:{id:'admin',username:'测试',role:'admin',tier:'free'}},version:0}))`)
  browser('open', `${base}/discover?system=hongguo&keyword=匹配`)
  waitFor(`document.body.innerText.includes('已显示 58 部') && !document.body.innerText.includes('正在搜索官网')`)
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看"]').length`), 58)
  assert.equal(evaluate(`document.querySelectorAll('[data-hongguo-downloaded]').length`), 2)
  assert.deepEqual(evaluate(`[...document.querySelectorAll('button[aria-label^="查看"]')].slice(0,3).map(b=>b.getAttribute('aria-label'))`), ['查看匹配作品0', '查看官网完整作品1', '查看匹配作品100'])
  assert.ok(evaluate(`document.body.innerText.includes('不代表官网全部搜索结果')`))
  evaluate(`window.searchRequests=[]; window.failPage=true; window.failLocal=false; window.failRemote=false; window.holdPage=false;
    const open=XMLHttpRequest.prototype.open,send=XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open=function(method,url,...rest){this.testURL=String(url);return open.call(this,method,url,...rest)};
    XMLHttpRequest.prototype.send=function(body){
      window.searchRequests.push(this.testURL);
      if((window.failLocal && this.testURL.includes('/works?')) || (window.failPage && this.testURL.includes('/works?') && this.testURL.includes('page=2&')) || (window.failRemote && this.testURL.includes('/search?'))) throw new Error('模拟搜索失败');
      if(window.holdPage && this.testURL.includes('/works?') && this.testURL.includes('page=2&')) { window.releaseSearch=()=>send.call(this,body); return; }
      return send.call(this,body);
    }`)
  click('查看匹配作品0')
  waitFor(`document.querySelector('[role="dialog"]')?.innerText.includes('本地详情')`)
  browser('press', 'Escape')
  waitFor(`!document.querySelector('[role="dialog"]')`)
  assert.ok(!evaluate(`window.searchRequests.some(url=>url.includes('/search?') || url.includes('/works?'))`))
  click('多选'); click('选择匹配作品0')
  assert.ok(evaluate(`document.querySelector('[aria-label="选择匹配作品0"] [data-hongguo-downloaded]').classList.contains('top-10')`))
  browser('scrollintoview', '[data-testid="hongguo-load-more"]')
  waitFor(`document.body.innerText.includes('本地资料加载失败')`)
  assert.ok(evaluate(`document.body.innerText.includes('已选 1 部') && document.body.innerText.includes('已显示 58 部')`))
  assert.equal(evaluate(`window.searchRequests.filter(url=>url.includes('/works?')).length`), 1)
  evaluate(`window.failPage=false`)
  click('重试加载')
  waitFor(`document.body.innerText.includes('已显示 60 部') && document.body.innerText.includes('本地匹配已加载完')`)
  assert.ok(evaluate(`document.body.innerText.includes('已选 1 部')`))
  assert.deepEqual(evaluate(`window.searchRequests.filter(url=>url.includes('/works?')).map(url=>new URL(url,location.origin).searchParams.get('page'))`), ['2', '2'])
  assert.ok(!evaluate(`window.searchRequests.some(url=>url.includes('/search?'))`))
  click('退出多选')
  browser('scroll', 'up', '20000')
  click('搜索')
  waitFor(`document.body.innerText.includes('已显示 58 部') && !document.body.innerText.includes('正在搜索官网')`)
  assert.equal(evaluate(`window.searchRequests.filter(url=>url.includes('/search?')).length`), 1)
  assert.equal(evaluate(`new URL(window.searchRequests.filter(url=>url.includes('/works?')).at(-1),location.origin).searchParams.get('page')`), '1')
  evaluate(`window.failRemote=true`)
  search('offline')
  waitFor(`document.body.innerText.includes('官网搜索失败') && document.body.innerText.includes('已显示 50 部')`)
  const localRequests = evaluate(`window.searchRequests.filter(url=>url.includes('/works?')).length`)
  evaluate(`window.failRemote=false`)
  click('重试官网搜索')
  waitFor(`document.body.innerText.includes('已显示 58 部') && !document.body.innerText.includes('官网搜索失败')`)
  assert.equal(evaluate(`window.searchRequests.filter(url=>url.includes('/works?')).length`), localRequests)
  evaluate(`window.holdPage=true`)
  browser('scrollintoview', '[data-testid="hongguo-load-more"]')
  waitFor(`typeof window.releaseSearch==='function'`)
  browser('scroll', 'up', '20000')
  search('empty')
  waitFor(`document.body.innerText.includes('没有找到匹配的作品')`)
  evaluate(`window.releaseSearch()`)
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('/works?keyword=offline&page=2&'))`)
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看"]').length`), 0)
  search('local-only')
  waitFor(`document.body.innerText.includes('已显示 50 部') && !document.body.innerText.includes('正在搜索官网')`)
  evaluate(`window.failLocal=true; window.holdPage=false`)
  search('remote-only')
  waitFor(`document.body.innerText.includes('已显示 10 部') && document.body.innerText.includes('本地资料加载失败')`)
  const remoteRequests = evaluate(`window.searchRequests.filter(url=>url.includes('/search?')).length`)
  evaluate(`window.failLocal=false`)
  click('重试加载')
  waitFor(`document.body.innerText.includes('已显示 58 部')`)
  assert.equal(evaluate(`window.searchRequests.filter(url=>url.includes('/search?')).length`), remoteRequests)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1024, 1440]) {
      browser('set', 'viewport', String(width), '900')
      waitFor(`document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).every(a=>a.playState!=='running')`)
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth`), `${theme}/${width}`)
      if (process.env.DISCOVER_SCREENSHOT_DIR && width === 390) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/search-${theme}.png`)
    }
  }
  assert.equal(browser('errors').trim(), '')
  console.log('红果合并搜索：去重、资料优先、分页/重试、选择保留、刷新、独立失败与过期响应检查通过')
} finally { browser('close') }
