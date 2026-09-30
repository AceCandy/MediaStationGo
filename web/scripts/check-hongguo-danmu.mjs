// DANMU_TEST_URL=http://127.0.0.1:4196 node web/scripts/check-hongguo-danmu.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DANMU_TEST_URL || 'http://127.0.0.1:4196'
const session = execFileSync('agent-browser', ['session', 'id', '--scope', 'worktree', '--prefix', 'danmu-check'], { encoding: 'utf8' }).trim()
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const route = (path, data) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data))
const wait = code => browser('wait', '--fn', code)
const open = path => browser('open', base + path)
const providers = [
  { id: 'hg', provider: 'hongguo', description: '红果实时弹幕（参数可空）', enabled: true, has_key: false, hongguo_app: {} },
  { id: 'tmdb', provider: 'tmdb', enabled: true, has_key: true, masked_key: 'fake****key', base_url: 'https://example.invalid' },
]
const edit = name => {
  const selector = `tbody tr:nth-child(${providers.findIndex(p => p.provider === name) + 1}) button[title="编辑"]`
  browser('wait', selector)
  browser('click', selector)
}
const save = () => {
  browser('click', 'tbody button[type=submit]')
  wait(`!document.querySelector(${JSON.stringify('button[title="取消编辑"]')})`)
}
const capture = () => evaluate(`window.danmuWrites=[];window.danmuDeletes=[];const open=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url,...rest){if(method==='DELETE')window.danmuDeletes.push(url);return open.call(this,method,url,...rest)};const send=XMLHttpRequest.prototype.send;XMLHttpRequest.prototype.send=function(body){if(typeof body==='string')window.danmuWrites.push(JSON.parse(body));return send.call(this,body)}`)

try {
  open('/login')
  route('auth/permissions', { permissions: {}, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('libraries*', [])
  route('admin/api-configs', { items: providers })
  route('admin/api-configs/hongguo', providers[0])
  route('admin/api-configs/tmdb', providers[1])
  route('admin/api-proxy-pool', { items: [] })
  route('admin/api-proxy-pool/config', { proxy_pool_type: 'normal', has_resin_proxy_token: false })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'local-test-token',user:{id:'danmu-admin',username:'页面测试',role:'admin',tier:'free'}},version:0}))`)
  open('/admin/integrations/apis')
  wait('document.body.innerText.includes("匿名模式")')
  browser('snapshot', '-i')
  capture()
  edit('hongguo')
  save()
  assert.deepEqual(evaluate('window.danmuWrites.at(-1)'), { enabled: true, hongguo_app: {} })
  edit('hongguo')
  browser('find', 'label', 'Cookie · 未配置', 'fill', 'FAKE_COOKIE')
  browser('find', 'label', 'device_id · 未配置', 'fill', '123456789')
  browser('click', 'summary')
  browser('fill', 'form textarea', '{"aid":"8662"}')
  save()
  assert.deepEqual(evaluate('window.danmuWrites.at(-1)'), { enabled: true, hongguo_app: { cookie: 'FAKE_COOKIE', device_id: '123456789', query: { aid: '8662' } } })
  providers[0].has_key = true
  providers[0].hongguo_app = { cookie: true, device_id: true, query: true }
  browser('network', 'unroute', `${base}/api/admin/api-configs`)
  browser('network', 'unroute', `${base}/api/**`)
  route('admin/api-configs', { items: providers })
  route('**', {})
  browser('reload')
  wait(`!!document.querySelector(${JSON.stringify('button[title="清除红果 App 参数"]')})`)
  capture()
  edit('hongguo')
  assert.equal(evaluate('Array.from(document.querySelectorAll("form input[type=password]")).every(i=>i.value==="")'), true)
  assert.equal(evaluate('document.body.innerText.includes("FAKE_COOKIE") || document.body.innerText.includes("123456789")'), false)
  browser('uncheck', 'form input[type=checkbox]')
  save()
  assert.deepEqual(evaluate('window.danmuWrites.at(-1)'), { enabled: false, hongguo_app: {} })
  edit('hongguo')
  browser('click', 'summary')
  browser('fill', 'form textarea', '{broken')
  browser('click', 'tbody button[type=submit]')
  wait('document.body.innerText.includes("高级参数必须是 JSON 对象")')
  assert.equal(evaluate('window.danmuWrites.length'), 1)
  browser('fill', 'form textarea', '')
  evaluate(`const style=document.createElement('style');style.textContent='*,*::before,*::after{transition:none!important;animation:none!important}';document.head.append(style);true`)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1023, 1024, 1440]) {
      browser('set', 'viewport', String(width), '900')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}: overflow ${width}`)
      assert.ok(evaluate('Array.from(document.querySelectorAll("form input[type=password],form textarea")).every(i=>i.getBoundingClientRect().width>0)'), 'hidden fields')
    }
    if (process.env.DANMU_SCREENSHOT_DIR) browser('screenshot', `${process.env.DANMU_SCREENSHOT_DIR}/${theme}.png`)
  }
  browser('scrollintoview', 'button[title="取消编辑"]')
  browser('find', 'role', 'button', 'click', '--name', '取消编辑', '--exact')
  browser('wait', 'button[title="清除红果 App 参数"]')
  browser('click', 'button[title="清除红果 App 参数"]')
  wait('document.body.innerText.includes("已存弹幕不会删除")')
  browser('find', 'role', 'button', 'click', '--name', '清除', '--exact')
  wait('!document.querySelector("[role=dialog]")')
  wait('window.danmuDeletes.some(url=>url.endsWith("/admin/api-configs/hongguo"))')
  edit('tmdb')
  browser('find', 'label', 'API Key', 'fill', 'FAKE_OLD_KEY')
  save()
  assert.deepEqual(evaluate('window.danmuWrites.at(-1)'), { enabled: true, base_url: 'https://example.invalid', api_key: 'FAKE_OLD_KEY' })
  open('/admin/emby/interfaces')
  wait('document.body.innerText.includes("当前集弹幕")')
  for (const width of [390, 768, 1440]) { browser('set', 'viewport', String(width), '900');assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth')) }
  evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'local-viewer-token',user:{id:'viewer',username:'页面测试',role:'user',tier:'free'}},version:0}))`)
  open('/admin/integrations/apis')
  wait('!location.pathname.startsWith("/admin")')
  console.log('红果表单：匿名保存、增量参数、禁用、敏感值不回显、整组清除、旧 provider、移动端和管理员权限检查通过')
} catch (error) {
  console.error(browser('snapshot', '-i'))
  throw error
} finally {
  browser('close')
}
