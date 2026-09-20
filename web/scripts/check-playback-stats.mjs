// 先启动本地 Web 预览，再运行：node scripts/check-playback-stats.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'

const base = process.env.PLAYBACK_STATS_TEST_URL || 'http://127.0.0.1:4179'
const session = `playback-stats-check-${process.pid}`
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
const labels = { catalog: '普通媒体库', hongguo: '红果短剧', nfo: '非常规媒体库', all: '全部体系' }
const libraries = [
  { id: 'catalog-lib', type: 'movie', name: '普通电影测试库' },
  { id: 'hongguo-lib', type: 'hongguo', name: '红果测试库' },
  { id: 'nfo-lib', type: 'nfo_movie', name: '本地电影测试库' },
  { id: 'nfo-tv-lib', type: 'nfo_tv', name: '本地剧集测试库' },
]
const items = ['catalog', 'hongguo', 'nfo'].map((system) => ({
  id: 'same-event-id', system, source_id: system === 'hongguo' ? '90001' : '',
  title: '同名测试作品', user_name: '测试账户', library_name: libraries.find((l) => l.id === `${system}-lib`).name,
  played_at: '2026-09-20T08:00:00Z', media_id: `${system}-media`, media_available: system !== 'nfo',
}))
function stats(system, empty = false) {
  const rows = empty ? [] : items.filter((item) => system === 'all' || item.system === system)
  return {
    total: rows.length, buckets: rows.length ? [{ period: '2026-09-20', count: rows.length }] : [],
    details: { items: rows, total: empty ? 0 : 21, page: 1, page_size: 20 },
    ranking: { grain: 'day', period: '2026-09-20', items: rows.map((item) => ({ ...item, group_id: 'same-group-id', count: 1 })) },
  }
}
function configure(mode = 'normal') {
  browser('network', 'unroute')
  route('auth/permissions', { permissions: {}, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('admin/users', [{ id: 'user', username: '测试账户' }])
  route('libraries*', libraries)
  if (mode === 'error') browser('network', 'route', `${base}/api/admin/playback-stats*`, '--abort')
  else for (const system of Object.keys(labels)) route(`admin/playback-stats?system=${system}&*`, stats(system, mode === 'empty'))
  route('**', {})
}

try {
  visit('/login')
  configure()
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'admin',username:'测试管理员',role:'admin',tier:'free'}},version:0}))`)
  visit('/playback-stats')
  waitFor(`document.querySelector('#playback-details-title') && document.body.innerText.includes('同名测试作品')`)
  assert.ok(evaluate(`performance.getEntriesByType('resource').some(r=>r.name.includes('/admin/playback-stats?system=all&'))`))
  assert.equal(evaluate(`document.querySelectorAll('fieldset input[type="checkbox"]').length`), 4)
  assert.ok(evaluate(`document.querySelector('table').innerText.includes('非常规媒体库')`))
  assert.ok(evaluate(`document.querySelector('a[href="/discover?system=hongguo&id=90001"]') !== null`))
  assert.ok(evaluate(`document.querySelector('a[href="/media/catalog-media"]') !== null`))
  assert.equal(evaluate(`document.querySelectorAll('a[href="/media/nfo-media"]').length`), 0)
  browser('find', 'role', 'button', 'click', '--name', '下一页', '--exact')
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('/admin/playback-stats?')&&r.name.includes('page=2'))`)
  for (const system of ['nfo', 'hongguo', 'catalog', 'all']) {
    const libraryCount = system === 'all' ? 4 : system === 'nfo' ? 2 : 1
    browser('find', 'first', 'button[aria-haspopup="listbox"]', 'click')
    browser('find', 'role', 'option', 'click', '--name', labels[system], '--exact')
    waitFor(`new URLSearchParams(location.search).get('system') === '${system}' && document.body.innerText.includes('同名测试作品') && document.querySelectorAll('fieldset input[type="checkbox"]').length === ${libraryCount}`)
    assert.equal(evaluate(`document.querySelectorAll('fieldset input[type="checkbox"]').length`), libraryCount)
    waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('system=${system}&')&&r.name.includes('page=1'))`)
    assert.equal(evaluate(`document.querySelectorAll('fieldset input:checked').length`), 0)
    browser('check', 'fieldset label:first-child input[type="checkbox"]')
    waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('system=${system}&')&&r.name.includes('library_ids='))`)
  }
  for (const theme of ['light', 'dark']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'`)
    for (const [width, height] of [[390, 844], [768, 1024], [1023, 900], [1024, 900], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} overflow`)
    }
  }
  browser('find', 'role', 'button', 'click', '--name', '每周', '--exact')
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.includes('rank_grain=week'))`)
  visit('/playback-stats?system=invalid')
  waitFor(`location.search === '?system=all'`)
  visit('/playback-stats?system=nfo&system=hongguo')
  waitFor(`location.search === '?system=nfo' && document.body.innerText.includes('同名测试作品')`)
  assert.equal(browser('errors').trim(), '')
  configure('empty')
  visit('/playback-stats?system=nfo')
  waitFor(`document.body.innerText.includes('当前条件下暂无播放明细。')`)
  configure('error')
  visit('/playback-stats?system=all')
  waitFor(`document.body.innerText.includes('播放统计加载失败。')`)
  console.log('播放统计合并/分体系、筛选、分页、来源标签、空/错误态和响应式检查通过')
} finally {
  browser('close')
}
