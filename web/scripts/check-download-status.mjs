import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `download-status-check-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const route = (path, value) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(value))
const wait = code => browser('wait', '--fn', code)
const work = { source_id: '123', title: '合并状态测试', kind: 'series', total: 9, downloading: 1, verifying: 1, publishing: 1, waiting_verify: 1, queued: 1, failed: 1, pending_review: 1, cancelled: 1, completed: 1 }

try {
  browser('open', base + '/login')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  for (const source of ['hongguo', 'huangguoai']) {
    route(`catalogs/${source}/downloads/config`, { root: '/downloads/test', concurrency: 1, verification_concurrency: 1 })
    route(`catalogs/${source}/downloads/works?*`, { items: [work], total: 1 })
    const items = ['downloading', 'waiting_verify', 'verifying', 'publishing'].map((status, i) => ({ id: `task-${i}`, source_id: '123', status, episode: i + 1, relative_path: 'test.mp4', bytes: 1, total_bytes: 2, attempts: 1 }))
    route(`catalogs/${source}/downloads/works/123/episodes?*`, { items, total: 4 })
  }
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'status-test',username:'页面测试',role:'admin',tier:'free'}},version:0}))`)
  for (const source of ['hongguo', 'huangguoai']) {
    browser('open', `${base}/admin/media/downloads?source=${source}`)
    wait(`document.querySelector('article')?.innerText.includes('↓ 下载中 4')`)
    assert.ok(evaluate(`performance.getEntriesByType('resource').some(r => r.name.includes('/downloads/works?') && new URL(r.name).searchParams.get('status') === 'active')`))
    const expected = ['↓ 下载中', '◷ 等待下载', '⚠ 失败', ...(source === 'huangguoai' ? ['⚠ 待人工确认'] : []), '⊘ 已取消', '✓ 已完成']
    assert.deepEqual(evaluate(`[...document.querySelector('article').querySelectorAll('span.rounded-full')].map(span => span.textContent.slice(0, span.textContent.lastIndexOf(' ')))`), expected)
    browser('find', 'role', 'button', 'click', '--name', source === 'hongguo' ? '作品状态' : '黄果AI下载状态', '--exact')
    assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="option"]')].map(option => option.textContent)`), ['全部状态', ...expected])
    browser('press', 'Escape')
    wait(`!document.querySelector('[role="listbox"]')`)
    browser('click', `button[aria-controls="${source === 'hongguo' ? 'episodes' : 'hga-episodes'}-123"]`)
    wait(`document.querySelectorAll('[data-download-episode]').length === 4`)
    assert.deepEqual(evaluate(`[...document.querySelectorAll('[data-download-episode] span.rounded')].map(span => span.textContent)`), Array(4).fill('↓ 下载中'))
    for (const legacy of ['verifying', 'publishing', 'waiting_verify']) {
      browser('open', `${base}/admin/media/downloads?source=${source}&status=${legacy}`)
      wait(`new URLSearchParams(location.search).get('status') === 'downloading' && document.querySelector('article')?.innerText.includes('↓ 下载中 4')`)
    }
  }
  console.log('download-status: passed merged counts, filter order, active requests, episode labels and legacy URLs for both sources')
} finally {
  browser('close')
}
