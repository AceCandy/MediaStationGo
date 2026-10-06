// 先启动本地 Web 预览，再运行：node scripts/check-player-request-logs.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'

const base = process.env.PLAYER_LOG_TEST_URL || 'http://127.0.0.1:4179'
const session = `player-logs-check-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('eval', '-b', Buffer.from(code).toString('base64'), '--json')).data.result
const route = (path, body) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(body))
const wait = code => browser('wait', '--fn', code)
const serialNo = '9007199254740993'
const log = { id: 'test-log', serial_no: serialNo, requested_at: '2026-10-07T01:07:19+08:00', method: 'GET', route: '/emby/Users/:userId/Items', status: 200, duration_ms: 3781, ip: '127.0.0.1', body: '', response_body: '', path_params: {}, headers: {}, query: {} }

try {
  browser('open', base + '/login')
  route('auth/permissions', { permissions: {}, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('admin/player-request-logs*', { items: [log], page: 1, page_size: 50, total: 1 })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'admin',username:'测试管理员',role:'admin',tier:'free'}},version:0}))`)
  browser('open', base + '/player-logs')
  wait(`document.body.innerText.includes('${serialNo}')`)
  evaluate(`const style=document.createElement('style'); style.textContent='*,*::before,*::after{transition:none!important;animation:none!important}'; document.head.append(style); true`)
  for (const theme of ['light', 'dark']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'`)
    for (const [width, height] of [[390, 844], [768, 1024], [1024, 768], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} overflow`)
      const selector = width < 1024 ? 'main section button' : 'main tbody tr'
      assert.ok(evaluate(`document.querySelector('${selector}').innerText.includes('${serialNo}')`))
      browser('click', selector)
      wait(`!!document.querySelector('[aria-label="复制流水号"]')`)
      assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('${serialNo}')`))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} dialog overflow`)
      evaluate(`Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText: async value => {window.copiedSerialNo=value}}}); window.copiedSerialNo=''`)
      browser('find', 'role', 'button', 'click', '--name', '复制流水号', '--exact')
      wait(`window.copiedSerialNo === '${serialNo}'`)
      wait(`!document.body.innerText.includes('已复制流水号')`)
      browser('find', 'role', 'button', 'click', '--name', '关闭', '--exact')
    }
  }
  browser('click', 'main tbody tr')
  evaluate(`Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText: async value => {window.copiedSerialNo=value}}})`)
  browser('find', 'role', 'button', 'click', '--name', '复制流水号', '--exact')
  wait(`window.copiedSerialNo === '${serialNo}'`)
  evaluate(`navigator.clipboard.writeText=async()=>{throw new Error('test clipboard failure')}`)
  browser('find', 'role', 'button', 'click', '--name', '复制流水号', '--exact')
  wait(`document.body.innerText.includes('复制失败')`)
  console.log('播放器日志桌面/手机流水号、大整数精度、详情复制及响应式检查通过')
} catch (error) {
  console.error(browser('errors'))
  console.error(browser('snapshot', '-i'))
  throw error
} finally {
  browser('close')
}
