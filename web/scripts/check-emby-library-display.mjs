// EMBY_DISPLAY_TEST_URL=http://127.0.0.1:4193 node scripts/check-emby-library-display.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.EMBY_DISPLAY_TEST_URL || 'http://127.0.0.1:4193'
const session = `emby-display-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = (code) => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const route = (path, data) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data))
const wait = (code) => browser('wait', '--fn', code)
const click = (name) => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const openDialog = () => {
  click('Emby 媒体库展示')
  wait(`document.querySelectorAll('[role="dialog"] [role="switch"]').length === 3 && getComputedStyle(document.querySelector('[role="dialog"]')).transform === 'none'`)
}
const order = () => evaluate(`Array.from(document.querySelectorAll('[role="dialog"] li > span')).map(el => el.textContent)`)
const key = 'emby.library_display'
const saved = [{ id: 'b', hidden: false }, { id: 'a', hidden: true }]

try {
  browser('open', base + '/login')
  route('auth/permissions', { permissions: {}, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('libraries*', ['电影', '电视剧', '新增库'].map((name, i) => ({ id: ['a', 'b', 'c'][i], name, type: 'movie', roots: [], enabled: true })))
  route('admin/settings', [{ key, value: JSON.stringify(saved) }])
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'display-admin',username:'页面测试',role:'admin'}},version:0}))`)
  browser('open', base + '/libraries')
  wait(`document.body.innerText.includes('Emby 媒体库展示')`)
  evaluate(`window.settingWrites=[]; const send=XMLHttpRequest.prototype.send; XMLHttpRequest.prototype.send=function(body){if(typeof body==='string' && body.includes('${key}')) window.settingWrites.push(JSON.parse(body)); return send.call(this,body)}`)
  openDialog()
  assert.deepEqual(order(), ['电视剧', '电影', '新增库'])
  assert.equal(evaluate(`document.querySelector('[aria-label="显示电影"]').checked`), false)
  assert.equal(evaluate(`!!document.querySelector('[aria-label="置顶电视剧"]')`), false)
  click('置顶新增库')
  assert.deepEqual(order(), ['新增库', '电视剧', '电影'])
  assert.equal(evaluate(`!!document.querySelector('[aria-label="置顶新增库"]')`), false)
  assert.equal(evaluate(`document.querySelectorAll('[role="dialog"] button[aria-label^="置顶"]').length`), 2)
  assert.ok(evaluate(`Array.from(document.querySelectorAll('[role="dialog"] li')).every(el => el.getBoundingClientRect().height <= 56)`))
  click('置顶电影')
  browser('check', '[aria-label="显示电影"]')
  click('取消')
  assert.deepEqual(evaluate('window.settingWrites'), [])
  assert.equal(evaluate('document.activeElement.textContent'), ' Emby 媒体库展示')
  openDialog()
  assert.deepEqual(order(), ['电视剧', '电影', '新增库'])
  const points = evaluate(`Array.from(document.querySelectorAll('[role="dialog"] li > button:first-child')).map(el => { const r=el.getBoundingClientRect(); return {x:Math.round(r.x+r.width/2),y:Math.round(r.y+r.height/2)} })`)
  browser('mouse', 'move', String(points[1].x), String(points[1].y))
  browser('mouse', 'down')
  browser('mouse', 'move', String(points[0].x), String(points[0].y - 10), '--steps', '24', '--duration', '800')
  browser('mouse', 'up')
  assert.deepEqual(order(), ['电影', '电视剧', '新增库'])
  browser('focus', '[aria-label="拖动电影排序，也可使用上下方向键"]')
  browser('press', 'ArrowDown')
  assert.deepEqual(order(), ['电视剧', '电影', '新增库'])
  browser('press', 'ArrowUp')
  assert.deepEqual(order(), ['电影', '电视剧', '新增库'])
  browser('check', '[aria-label="显示电影"]')
  browser('uncheck', '[aria-label="显示电视剧"]')
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'`)
    for (const width of [390, 768, 1023, 1024, 1440]) {
      browser('set', 'viewport', String(width), '900')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme} overflow at ${width}`)
      assert.ok(evaluate(`document.querySelector('[role="dialog"]').scrollWidth <= document.querySelector('[role="dialog"]').clientWidth`))
    }
    browser('mouse', 'move', '0', '0')
    wait(`Array.from(document.querySelectorAll('[role="dialog"] button')).every(el => el.getAnimations().every(a => a.playState !== 'running'))`)
    const audit = JSON.parse(browser('--json', 'a11y', '--selector', '[role="dialog"]'))
    assert.equal(audit.success, true)
    assert.equal(audit.data.violations.length, 0, JSON.stringify(audit.data.violations))
    if (process.env.EMBY_DISPLAY_SCREENSHOT_DIR) browser('screenshot', `${process.env.EMBY_DISPLAY_SCREENSHOT_DIR}/${theme}.png`)
  }
  click('保存')
  wait(`!document.querySelector('[role="dialog"]')`)
  const expected = [{ id: 'a', hidden: false }, { id: 'b', hidden: true }, { id: 'c', hidden: false }]
  assert.deepEqual(evaluate('window.settingWrites'), [{ key, value: JSON.stringify(expected) }])
  browser('network', 'unroute', `${base}/api/admin/settings`)
  browser('network', 'unroute', `${base}/api/**`)
  route('admin/settings', [{ key, value: JSON.stringify(expected) }])
  route('**', {})
  openDialog()
  assert.deepEqual(order(), ['电影', '电视剧', '新增库'])
  assert.equal(evaluate(`document.querySelector('[aria-label="显示电视剧"]').checked`), false)
  browser('network', 'unroute', `${base}/api/admin/settings`)
  browser('network', 'unroute', `${base}/api/**`)
  browser('network', 'route', `${base}/api/admin/settings`, '--abort')
  route('**', {})
  click('保存')
  wait(`document.body.innerText.includes('保存失败')`)
  assert.ok(evaluate(`!!document.querySelector('[role="dialog"]')`))
  click('取消')
  click('Emby 媒体库展示')
  wait(`document.querySelector('[role="alert"]')?.textContent.includes('加载失败')`)
  browser('network', 'unroute', `${base}/api/admin/settings`)
  browser('network', 'unroute', `${base}/api/**`)
  route('admin/settings', [{ key, value: JSON.stringify(expected) }])
  route('**', {})
  click('重试')
  wait(`document.querySelectorAll('[role="dialog"] [role="switch"]').length === 3`)
  click('取消')
  browser('open', base + '/admin/emby/interfaces')
  wait(`!!document.querySelector('input[type="search"]')`)
  browser('fill', 'input[type="search"]', '/Users/:userId/Views')
  wait(`document.querySelectorAll('#main-content details').length === 1`)
  browser('click', '#main-content summary')
  evaluate(`navigator.clipboard.writeText=async () => {}`)
  click('复制路径 /Users/:userId/Views')
  wait(`document.body.innerText.includes('已复制')`)
  evaluate(`navigator.clipboard.writeText=async () => {throw new Error('denied')}`)
  click('复制路径 /Users/:userId/Views')
  wait(`document.body.innerText.includes('复制失败')`)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'`)
    for (const width of [390, 768, 1440]) {
      browser('set', 'viewport', String(width), width === 390 ? '844' : width === 768 ? '1024' : '900')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'))
    }
    browser('mouse', 'move', '0', '0')
    wait(`Array.from(document.querySelectorAll('#main-content *')).every(el => el.getAnimations().every(a => a.playState !== 'running'))`)
    const audit = JSON.parse(browser('--json', 'a11y', '--selector', '#main-content'))
    assert.equal(audit.success, true)
    if (audit.data.violations.length) console.warn(`现有接口说明页 ${theme} 可访问性检查：${JSON.stringify(audit.data.violations)}`)
  }
  browser('fill', 'input[type="search"]', 'no-such-endpoint')
  wait(`document.body.innerText.includes('没有匹配的接口')`)
  click('清除筛选')
  browser('click', '#main-content button[aria-haspopup="listbox"]')
  browser('find', 'role', 'option', 'click', '--name', '用户与媒体库', '--exact')
  wait(`document.querySelectorAll('#main-content details').length > 0`)
  assert.ok(evaluate(`Array.from(document.querySelectorAll('#main-content details')).some(el => el.textContent.includes('管理员统一配置'))`))
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-viewer-token',user:{id:'viewer',username:'页面测试',role:'user'}},version:0}))`)
  browser('open', base + '/admin/emby/interfaces')
  wait(`!location.pathname.startsWith('/admin/emby')`)
  browser('open', base + '/libraries')
  wait(`document.body.innerText.includes('电影')`)
  assert.ok(!evaluate(`document.body.innerText.includes('Emby 媒体库展示')`))
  console.log('Emby 展示：顺序、显隐、取消、保存请求、重新加载、失败保留、普通用户及响应式检查通过')
} finally {
  browser('close')
}
