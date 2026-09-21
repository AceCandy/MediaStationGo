// 先启动本地 Web 预览，再运行：node scripts/check-nextup.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'

const base = process.env.NEXTUP_TEST_URL || 'http://127.0.0.1:4179'
const session = `nextup-check-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('eval', '-b', Buffer.from(code).toString('base64'), '--json')).data.result
const route = (path, body) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(body))
const visit = path => browser('open', base + path)
const wait = code => browser('wait', '--fn', code)
const media = { id: 'next-file', library_id: 'library', series_id: 'hg-group-100', catalog_source: 'hongguo', catalog_item_id: 'hg-episode-next', metadata_kind: 'episode', title: '第 1 集', series_title: '测试合集', season_num: 2, episode_num: 1, tracks: [], rating: 0 }

try {
  visit('/login')
  route('auth/permissions', { permissions: {}, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('media/recent*', [])
  route('watch-history/continue*', [{ history: { id: 'next:hg-episode-next', is_next: true, position_ms: 0, duration_ms: 0, completed: false }, media }])
  route('ai/status', { enabled: false })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'admin',username:'测试管理员',role:'admin',tier:'free'}},version:0}))`)
  visit('/')
  wait(`document.body.innerText.includes('接着看下一集')`)
  assert.ok(evaluate(`Array.from(document.querySelectorAll('a')).some(a=>a.textContent.includes('接着看下一集') && a.getAttribute('href')==='/media/next-file')`))
  assert.ok(evaluate(`document.body.innerText.includes('第 2 季 · 第 1 集')`))
  visit('/admin/emby/interfaces')
  wait(`!!document.querySelector('input[placeholder="搜索名称、路径或说明"]')`)
  browser('find', 'placeholder', '搜索名称、路径或说明', 'fill', '/Items/Resume')
  wait(`document.querySelectorAll('main details').length === 1`)
  assert.ok(evaluate(`document.querySelector('main details').textContent.includes('跨季')`))
  assert.ok(evaluate(`document.querySelector('main details').textContent.includes('StartIndex')`))
  browser('find', 'placeholder', '搜索名称、路径或说明', 'fill', 'NextUp')
  wait(`document.querySelectorAll('main details').length === 1`)
  assert.ok(evaluate(`document.querySelector('main details').textContent.includes('已实现')`))
  browser('click', 'main details summary')
  assert.ok(evaluate(`document.querySelector('main details').textContent.includes('SeriesId')`))
  evaluate(`const style=document.createElement('style'); style.textContent='*,*::before,*::after{transition:none!important;animation:none!important}'; document.head.append(style); true`)
  for (const theme of ['light', 'dark']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'`)
    for (const [width, height] of [[390, 844], [768, 1024], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} overflow`)
    }
    const audit = JSON.parse(browser('a11y', '--selector', 'main', '--json'))
    assert.equal(audit.data.violations.length, 0, JSON.stringify(audit.data.violations))
  }
  evaluate(`Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText: async value => {window.copiedPath=value}}})`)
  browser('find', 'role', 'button', 'click', '--name', '复制路径 /Shows/NextUp', '--exact')
  wait(`window.copiedPath === '/Shows/NextUp'`)
  evaluate(`navigator.clipboard.writeText=async()=>{throw new Error('test clipboard failure')}`)
  browser('find', 'role', 'button', 'click', '--name', '复制路径 /Shows/NextUp', '--exact')
  wait(`document.body.innerText.includes('复制失败')`)
  browser('find', 'placeholder', '搜索名称、路径或说明', 'fill', 'no-such-nextup-endpoint')
  wait(`document.body.innerText.includes('没有匹配的接口')`)
  browser('find', 'role', 'button', 'click', '--name', '清除筛选', '--exact')
  browser('find', 'first', 'button[aria-haspopup="listbox"]', 'click')
  browser('find', 'role', 'option', 'click', '--name', '媒体项', '--exact')
  wait(`document.body.innerText.includes('接着看下一集')`)
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'viewer',username:'测试观众',role:'user',tier:'free'}},version:0}))`)
  visit('/admin/emby/interfaces')
  wait(`location.pathname === '/' && document.body.innerText.includes('接着看下一集')`)
  assert.equal(evaluate(`document.querySelectorAll('details').length`), 0)
  console.log('下一集卡片链接、接口目录搜索/筛选/复制、响应式、主题可访问性及管理员访问检查通过')
} catch (error) {
  console.error(browser('snapshot', '-i'))
  throw error
} finally {
  browser('close')
}
