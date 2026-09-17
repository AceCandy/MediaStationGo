// 独立浏览器会话和模拟接口，不写实际下载队列或定时配置。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `supplement-task-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = (code) => { const r = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))); assert.equal(r.success, true); return r.data.result }
const route = (path, data) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data))
const click = (name) => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const fill = (name, value) => browser('find', 'label', name, 'fill', value)
const wait = (code) => browser('wait', '--fn', code)
const definition = { key: 'hongguo_download_supplement', system: 'hongguo', name: '红果补充下载', description: '按上线时间补充新作品', trigger: '定时 / 手动', action: 'scheduler', current_state: 'idle', schedule_config: { enabled: false, interval_seconds: 86400, min_interval_seconds: 60, max_interval_seconds: 2592000, count: 10 } }
try {
  browser('open', `${base}/login`)
  route('auth/permissions', { role: 'admin', is_super: true, permissions: {} })
  route('play-profiles', [])
  route('tasks/definitions/hongguo_download_supplement/run', { status: 'queued' })
  route('tasks/definitions/hongguo_download_supplement/schedule', definition)
  route('tasks?*', { items: [], total: 0, definitions: [definition] })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'test-token',user:{id:'admin',username:'测试',role:'admin',tier:'free'}},version:0}))`)
  browser('open', `${base}/admin/tasks?system=hongguo`)
  wait(`document.body.innerText.includes('红果补充下载')`)
  evaluate(`window.writes=[];const open=XMLHttpRequest.prototype.open,send=XMLHttpRequest.prototype.send;XMLHttpRequest.prototype.open=function(method,url,...args){this.taskMethod=method;this.taskURL=String(url);return open.call(this,method,url,...args)};XMLHttpRequest.prototype.send=function(body){if(this.taskURL?.includes('/hongguo_download_supplement/') && ['POST','PUT'].includes(this.taskMethod)){window.writes.push({url:this.taskURL,body:JSON.parse(body)});if(window.failWrite)throw new Error('mock failure')}return send.call(this,body)}`)
  click('立即执行红果补充下载')
  wait(`!!document.querySelector('[role="dialog"] input')`)
  assert.equal(evaluate(`document.querySelector('[role="dialog"] input').value`), '10')
  fill('本次补充数量', '3')
  click('取消')
  assert.equal(evaluate(`window.writes.length`), 0)
  assert.equal(evaluate(`document.activeElement.getAttribute('aria-label')`), '立即执行红果补充下载')
  click('立即执行红果补充下载')
  fill('本次补充数量', '101'); click('确认执行')
  assert.equal(evaluate(`window.writes.length`), 0)
  fill('本次补充数量', '3')
  evaluate(`window.failWrite=true`); click('确认执行')
  wait(`!!document.querySelector('[role="dialog"] [role="alert"]')`)
  assert.equal(evaluate(`document.querySelector('[role="dialog"] input').value`), '3')
  evaluate(`window.failWrite=false`); click('确认执行')
  wait(`!document.querySelector('[role="dialog"]')`)
  assert.deepEqual(evaluate(`window.writes.at(-1).body`), { count: 3 })
  assert.ok(evaluate(`window.writes.every(w=>w.url.endsWith('/run'))`))
  click('设置红果补充下载周期')
  wait(`!!document.querySelector('[role="dialog"] input[type="checkbox"]')`)
  assert.equal(evaluate(`document.querySelector('[role="dialog"] input[type="checkbox"]').checked`), false)
  assert.equal(evaluate(`document.querySelector('[role="dialog"] input[type="number"]').value`), '1')
  assert.equal(evaluate(`[...document.querySelectorAll('[role="dialog"] input[type="number"]')].at(-1).value`), '10')
  fill('每轮补充数量', '0'); click('保存')
  wait(`document.querySelector('[role="alert"]')?.innerText.includes('1–100')`)
  assert.ok(evaluate(`window.writes.every(w=>w.url.endsWith('/run'))`))
  fill('每轮补充数量', '5')
  browser('find', 'label', '启用定时执行', 'check')
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1440]) {
      browser('set', 'viewport', String(width), '900')
      evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{}))).then(()=>true)`)
      assert.ok(evaluate(`document.documentElement.scrollWidth <= innerWidth && document.querySelector('[role="dialog"]').scrollWidth <= document.querySelector('[role="dialog"]').clientWidth`))
      if (process.env.DOWNLOAD_SCREENSHOT_DIR && width === 390) browser('screenshot', `${process.env.DOWNLOAD_SCREENSHOT_DIR}/schedule-${theme}.png`)
    }
  }
  click('保存'); wait(`!document.querySelector('[role="dialog"]')`)
  assert.deepEqual(evaluate(`window.writes.at(-1).body`), { enabled: true, interval_seconds: 86400, count: 5 })
  assert.equal(browser('errors').trim(), '')
  console.log('红果补充任务：入口、默认配置、手动数量隔离、取消/失败、数量边界、周期请求与双主题响应式通过')
} finally { browser('close') }
