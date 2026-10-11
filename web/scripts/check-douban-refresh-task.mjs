// 隔离浏览器与模拟接口，验证公共任务的基础刷新限额及进度展示。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.argv[2] || 'http://127.0.0.1:6239'
const session = `douban-refresh-task-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const route = (path, data) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data))
const click = name => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const fill = (name, value) => browser('find', 'label', name, 'fill', value)
const wait = code => browser('wait', '--fn', code)
const definition = { key: 'douban_data_refresh', system: 'common', name: '豆瓣资料刷新', description: '限量刷新基础资料与评分，仅调用 API，不抓取网页补充', trigger: '定时 / 手动', action: 'scheduler', current_state: 'idle', schedule_config: { enabled: false, interval_seconds: 86400, min_interval_seconds: 60, max_interval_seconds: 2592000, count: 100 }, latest: { system: 'common', id: 'test-refresh', kind: 'douban_data_refresh', trigger: 'scheduled', name: '豆瓣资料刷新', status: 'completed', started_at: '2026-10-11T12:00:00Z', updated_at: '2026-10-11T12:01:00Z', metrics: { total: 200, selected: 3, processed: 3, changed: 1, unchanged: 1, skipped: 1, failed: 0, remaining: 197, requested: 4 } } }
try {
  browser('open', `${base}/login`)
  route('auth/permissions', { role: 'admin', is_super: true, permissions: {} })
  route('play-profiles', [])
  route('tasks/startup', { state: 'ready', stage: '初始化完成', elapsed_seconds: 0, stage_elapsed_seconds: 0, directories_found: 0, directories_watched: 0, warnings: [] })
  route('tasks/definitions/douban_data_refresh/run', { status: 'queued' })
  route('tasks/definitions/douban_data_refresh/schedule', definition)
  route('tasks?*system=common*', { items: [], total: 0, definitions: [definition] })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'test-token',user:{id:'admin',username:'测试',role:'admin',tier:'free'}},version:0}))`)
  browser('open', `${base}/admin/tasks?system=common`)
  wait("document.body.innerText.includes('豆瓣资料刷新') && document.body.innerText.includes('接口请求 4')")
  assert.ok(evaluate("document.body.innerText.includes('剩余到期 197') && document.body.innerText.includes('未变化 1') && document.body.innerText.includes('跳过 1')"))
  evaluate(`window.writes=[];const open=XMLHttpRequest.prototype.open,send=XMLHttpRequest.prototype.send;XMLHttpRequest.prototype.open=function(method,url,...args){this.taskMethod=method;this.taskURL=String(url);return open.call(this,method,url,...args)};XMLHttpRequest.prototype.send=function(body){if(this.taskURL?.includes('/douban_data_refresh/') && ['POST','PUT'].includes(this.taskMethod))window.writes.push({url:this.taskURL,body:body?JSON.parse(body):null});return send.call(this,body)}`)
  click('设置豆瓣资料刷新周期')
  wait("!!document.querySelector('[role=dialog] input[type=checkbox]')")
  assert.equal(evaluate("document.querySelector('[role=dialog] input[type=checkbox]').checked"), false)
  assert.equal(evaluate("[...document.querySelectorAll('[role=dialog] input[type=number]')].at(-1).value"), '100')
  assert.ok(evaluate("document.querySelector('[role=dialog]').innerText.includes('不抓取网页补充')"))
  for (const value of ['0', '101']) {
    fill('每轮最多更新数量', value)
    click('保存')
    wait("document.querySelector('[role=alert]')?.innerText.includes('1–100')")
    assert.equal(evaluate('window.writes.length'), 0)
  }
  fill('每轮最多更新数量', '25')
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1279, 1280, 1440]) {
      browser('set', 'viewport', String(width), '900')
      evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{}))).then(()=>true)`)
      assert.ok(evaluate("document.documentElement.scrollWidth <= innerWidth && document.querySelector('[role=dialog]').scrollWidth <= document.querySelector('[role=dialog]').clientWidth"), `${theme}/${width}`)
    }
  }
  click('保存')
  wait("!document.querySelector('[role=dialog]')")
  assert.deepEqual(evaluate('window.writes.at(-1).body'), { enabled: false, interval_seconds: 86400, count: 25 })
  click('立即执行豆瓣资料刷新')
  wait("window.writes.some(write=>write.url.endsWith('/run'))")
  assert.equal(evaluate("!!document.querySelector('[role=dialog]')"), false)
  assert.equal(browser('errors').trim(), '')
  console.log('通过：公共任务基础刷新、默认关闭/100条、数量边界及提交、进度与真实请求数、手动限量入口、双主题响应式')
} finally { browser('close') }
