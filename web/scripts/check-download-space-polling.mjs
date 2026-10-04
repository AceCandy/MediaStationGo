import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `download-polling-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = (code) => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const route = (path, value) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(value))
const wait = (code) => browser('wait', '--fn', code)
const counts = () => evaluate('window.pollCheck.starts')

try {
  browser('open', `${base}/login`)
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('catalogs/hongguo/downloads/config', { root: '/fixture', concurrency: 3, verification_concurrency: 2, full_verification: true, hardware_verification: false, priority: 'app' })
  route('catalogs/hongguo/downloads/works?*', { items: [{ source_id: '123', title: '轮询测试', total: 1, completed: 1 }], total: 1 })
  route('catalogs/hongguo/downloads/works/123/episodes?*', { items: [{ id: 'fixture', source_id: '123', title: '轮询测试', episode: 1, relative_path: 'fixture/S01E001.mp4', status: 'completed', bytes: 1, total_bytes: 1 }], total: 1 })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'fixture',username:'测试',role:'admin',tier:'free'}},version:0}))`)
  browser('open', `${base}/admin/media/downloads`)
  wait(`document.body.innerText.includes('完成 1/1 集')`)
  evaluate(`(() => {
    let hidden = false
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden })
    window.setPollHidden = (value) => { hidden = value; document.dispatchEvent(new Event('visibilitychange')) }
    const state = window.pollCheck = { starts: { works: 0, episodes: 0 }, active: { works: 0, episodes: 0 }, maximum: { works: 0, episodes: 0 }, hold: false, pending: [] }
    const open = XMLHttpRequest.prototype.open
    XMLHttpRequest.prototype.open = function(method, url, ...args) {
      this.pollKind = /\\/downloads\\/works\\/123\\/episodes/.test(url) ? 'episodes' : /\\/downloads\\/works\\?/.test(url) ? 'works' : undefined
      return open.call(this, method, url, ...args)
    }
    const send = XMLHttpRequest.prototype.send
    XMLHttpRequest.prototype.send = function(...args) {
      const kind = this.pollKind
      if (kind) {
        state.starts[kind]++; state.active[kind]++
        state.maximum[kind] = Math.max(state.maximum[kind], state.active[kind])
        this.addEventListener('loadend', () => state.active[kind]--, { once: true })
      }
      if (kind === 'works' && state.hold) state.pending.push(() => send.apply(this, args))
      else return send.apply(this, args)
    }
  })()`)
  browser('click', 'button[aria-controls="episodes-123"]')
  wait(`document.body.innerText.includes('E001') && window.pollCheck.active.episodes === 0`)
  evaluate('window.setPollHidden(true)')
  const hiddenCounts = counts()
  evaluate('new Promise(resolve => setTimeout(() => resolve(true), 6100))')
  assert.deepEqual(counts(), hiddenCounts, 'hidden page continued polling')

  evaluate('window.setPollHidden(false)')
  wait(`window.pollCheck.starts.works === ${hiddenCounts.works + 1} && window.pollCheck.starts.episodes === ${hiddenCounts.episodes + 1}`)
  wait('window.pollCheck.active.works === 0 && window.pollCheck.active.episodes === 0')

  const beforeFlight = counts()
  evaluate(`window.setPollHidden(true); window.pollCheck.hold = true; for (let i = 0; i < 20; i++) { window.setPollHidden(false); window.setPollHidden(true) } window.setPollHidden(false)`)
  assert.equal(counts().works, beforeFlight.works + 1, 'visibility events overlapped an in-flight request')
  assert.equal(evaluate('window.pollCheck.active.works'), 1)
  evaluate('window.pollCheck.hold = false; window.pollCheck.pending.splice(0).forEach(send => send())')
  wait('window.pollCheck.active.works === 0 && window.pollCheck.active.episodes === 0')
  assert.deepEqual(evaluate('window.pollCheck.maximum'), { works: 1, episodes: 1 })

  evaluate('window.setPollHidden(true)')
  browser('click', 'button[aria-controls="episodes-123"]')
  wait(`document.querySelector('button[aria-controls="episodes-123"]').getAttribute('aria-expanded') === 'false'`)
  const collapsedCounts = counts()
  evaluate('window.setPollHidden(false)')
  wait(`window.pollCheck.starts.works === ${collapsedCounts.works + 1} && window.pollCheck.active.works === 0`)
  assert.equal(counts().episodes, collapsedCounts.episodes, 'collapsed episode listener survived cleanup')
  evaluate('window.setPollHidden(true)')
  console.log('Download-space polling: hidden pause, immediate resume, single flight and collapsed cleanup passed.')
} finally {
  browser('close')
}
