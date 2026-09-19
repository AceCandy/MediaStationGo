// Run against a Web preview: SETTINGS_TEST_URL=http://127.0.0.1:4187 node scripts/check-settings-tabs.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.SETTINGS_TEST_URL || 'http://127.0.0.1:4187'
const session = `settings-check-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const route = (path, data) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data))
const wait = code => browser('wait', '--fn', code)
const open = path => browser('open', base + path)
const settingKey = 'playback.auto_mark_previous_episodes'
const checkbox = 'input[aria-label="自动标记本季前面的集数"]'
const select = tab => browser('click', `nav[aria-label="系统设置分类"] a[href="/admin/settings/${tab}"]`)

try {
  open('/login')
  route('auth/permissions', { permissions: {}, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('libraries*', [])
  route('admin/settings', [])
  route('admin/recognition-words', { enabled: true, local_text: '', shared_urls: [], rule_count: 0 })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:'settings-admin',username:'页面测试',role:'admin',tier:'free'}},version:0}))`)
  open('/admin/settings/watching')
  wait(`!!document.querySelector(${JSON.stringify(checkbox)})`)
  assert.equal(evaluate(`document.querySelectorAll('#main-content h1').length`), 0, 'redundant settings page heading')
  assert.equal(evaluate(`document.querySelector(${JSON.stringify(checkbox)}).checked`), false)
  browser('check', checkbox)
  select('general')
  wait(`location.pathname.endsWith('/general') && !!document.querySelector('nav[aria-label="系统设置分类"]')`)
  select('watching')
  wait(`!!document.querySelector(${JSON.stringify(checkbox)})`)
  assert.equal(evaluate(`document.querySelector(${JSON.stringify(checkbox)}).checked`), true, 'tab switch lost unsaved draft')
  evaluate(`window.settingWrites=[]; const send=XMLHttpRequest.prototype.send; XMLHttpRequest.prototype.send=function(body){if(typeof body==='string' && body.includes(${JSON.stringify(settingKey)})) window.settingWrites.push(JSON.parse(body)); return send.call(this,body)}`)
  browser('find', 'role', 'button', 'click', '--name', '保存', '--exact')
  wait(`document.body.innerText.includes('所有更改已保存')`)
  assert.deepEqual(evaluate('window.settingWrites'), [{ key: settingKey, value: 'true' }])
  browser('network', 'unroute', `${base}/api/admin/settings`)
  browser('network', 'unroute', `${base}/api/**`)
  route('admin/settings', [{ key: settingKey, value: 'true' }])
  route('**', {})
  browser('reload')
  wait(`document.querySelector(${JSON.stringify(checkbox)})?.checked === true`)
  for (const tab of ['general', 'playback', 'recognition-words', 'access', 'watching']) {
    select(tab)
    wait(`document.querySelector('nav[aria-label="系统设置分类"] [aria-current="page"]')?.getAttribute('href') === '/admin/settings/${tab}'`)
    assert.equal(evaluate(`document.querySelector('nav[aria-label="系统设置分类"] [aria-current="page"]').getAttribute('href')`), `/admin/settings/${tab}`)
  }
  select('recognition-words')
  wait(`document.querySelector('nav[aria-label="系统设置分类"] [aria-current="page"]')?.getAttribute('href') === '/admin/settings/recognition-words' && document.body.innerText.includes('自定义识别词')`)
  browser('find', 'label', '本地识别词', 'fill', '草稿 => 保留')
  assert.equal(evaluate(`document.querySelector('textarea[placeholder]').value`), '草稿 => 保留', 'recognition draft was not entered')
  select('watching')
  wait(`document.querySelector('nav[aria-label="系统设置分类"] [aria-current="page"]')?.getAttribute('href') === '/admin/settings/watching'`)
  select('recognition-words')
  wait(`document.querySelector('nav[aria-label="系统设置分类"] [aria-current="page"]')?.getAttribute('href') === '/admin/settings/recognition-words'`)
  assert.equal(evaluate(`document.querySelector('textarea[placeholder]').value`), '草稿 => 保留')
  select('watching')
  wait(`document.querySelector('nav[aria-label="系统设置分类"] [aria-current="page"]')?.getAttribute('href') === '/admin/settings/watching'`)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1023, 1024, 1440]) {
      browser('set', 'viewport', String(width), '900')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}: overflow at ${width}`)
      assert.ok(evaluate(`Array.from(document.querySelectorAll('nav[aria-label="系统设置分类"] a')).every(a => a.getBoundingClientRect().height >= 44)`))
      if (process.env.SETTINGS_SCREENSHOT_DIR && [390, 1440].includes(width)) browser('screenshot', `${process.env.SETTINGS_SCREENSHOT_DIR}/${theme}-${width}.png`)
    }
  }
  assert.equal(evaluate(`document.querySelectorAll('aside a[href="/admin/settings"]').length`), 1)
  assert.equal(evaluate(`document.querySelector('aside a[href="/admin/settings"]').getAttribute('aria-current')`), 'page')
  assert.equal(evaluate(`document.querySelectorAll('aside a[href^="/admin/settings"]').length`), 1)
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-viewer-token',user:{id:'viewer',username:'页面测试',role:'user',tier:'free'}},version:0}))`)
  open('/admin/settings/watching')
  wait(`!location.pathname.startsWith('/admin/settings')`)
  assert.ok(!evaluate(`!!document.querySelector('nav[aria-label="系统设置分类"]')`))
  console.log('Settings tabs: draft, save payload, reload, legacy paths, permissions and responsive themes passed')
} finally {
  browser('close')
}
