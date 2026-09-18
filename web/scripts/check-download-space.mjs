import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `download-space-check-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = (code) => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const route = (path, value) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(value))
const open = (path) => browser('open', base + path)
const wait = (code) => browser('wait', '--fn', code)
const config = { root: '/downloads/hongguo', temporary_dir: '/downloads/hongguo/downloading', output_dir: '/downloads/hongguo/completed', concurrency: 3, verification_concurrency: 2, full_verification: true, hardware_verification: false, priority: 'official' }
const job = { id: '10000000-0000-0000-0000-000000000001', source_id: '123', title: '用于验证年月归档与长剧名排版的红果短剧', episode: 1, relative_path: '2026/09/剧名 [hongguo-123]/Season 01/S01E001.mp4', status: 'failed', bytes: 0, total_bytes: 0, attempts: 1, error: '来源暂时不可用，可重试', source: 'app', quality: 1080, width: 1080, height: 1922, codec: 'hevc', source_errors: { app: '当前视频已下架（101002）', official: '红果资料 HTTP 404' } }
const episodes = ['downloading', 'verifying', 'publishing', 'waiting_verify', 'failed', 'queued', 'cancelled', 'completed'].map((status, index) => status === 'failed' ? job : { ...job, id: `task-${status}`, status, episode: index + 2, bytes: 1024, total_bytes: 2048, error: '' })
const auth = (role) => evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({state:{token:'local-test-token',user:{id:${JSON.stringify(role)},username:'页面测试',role:${JSON.stringify(role)},tier:'free'}},version:0}))`)

try {
  open('/login')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('tasks/definitions/hongguo_download/executions?*', { items: [{ id: 'execution-1', name: '红果下载：测试 E001', status: 'failed', stage: 'verifying', started_at: '2026-09-16T12:00:00Z', finished_at: '2026-09-16T12:01:00Z', error: '视频校验失败' }], total: 21 })
  route('catalogs/hongguo/downloads/config', config)
  route('catalogs/hongguo/downloads/works?*failed_only=true*', { items: [{ source_id: '123', title: job.title, total: 81, failed: 74, completed: 1, downloading: 1, verifying: 1, publishing: 1, waiting_verify: 1, queued: 1, cancelled: 1 }], total: 1 })
  route('catalogs/hongguo/downloads/works?*', { items: [{ source_id: '123', title: job.title, total: 81, failed: 74, completed: 1, downloading: 1, verifying: 1, publishing: 1, waiting_verify: 1, queued: 1, cancelled: 1 }, { source_id: '456', title: '已完成的测试剧', total: 12, completed: 12 }], total: 2 })
  route('catalogs/hongguo/downloads/works/123/episodes?*', { items: episodes, total: 81 })
  route('catalogs/hongguo/downloads/works/123/retry', { added: 80, skipped: 0 })
  route('catalogs/hongguo/downloads/*/retry', {})
  route('catalogs/hongguo/downloads', { added: 1 })
  route('catalogs/hongguo/status', { enabled: true })
  route('catalogs/hongguo/works?*', { items: [], total: 0 })
  route('catalogs/hongguo/works/123/media?*', { items: [], total: 0 })
  route('catalogs/hongguo/works/123/favorite', { favorite: false })
  route('catalogs/hongguo/works/123', { id: 'work-123', source_id: '123', title: '下载测试剧', kind: 'series', episode_count: 1, total_episodes: 1, accessible_episodes: 1, rating: 0, tags: [], credits: [], artwork: [], episodes: [], overview: '' })
  route('**', {})
  auth('admin')
  open('/admin/media/downloads?page=invalid')
  wait(`document.body.innerText.includes('完成 1/81 集')`)
  assert.ok(evaluate(`location.search.includes('page=1')`))
  assert.ok(!evaluate(`document.querySelector('input[placeholder="例如 /downloads/hongguo"]')`))
  assert.ok(evaluate(`document.querySelector('nav[aria-label="下载来源"] button').classList.contains('border-b-2')`))
  assert.ok(!evaluate(`performance.getEntriesByType('resource').some(r=>r.name.includes('/episodes'))`))
  assert.equal(evaluate(`document.querySelectorAll('article').length`), 2)
  assert.ok(evaluate(`document.body.innerText.includes('✅ 全部完成')`))
  browser('find', 'label', '仅显示含失败集的剧集', 'check')
  wait(`document.querySelectorAll('article').length === 1 && document.body.innerText.includes('共 1 部')`)
  assert.ok(evaluate(`new URLSearchParams(location.search).get('failed_only') === 'true' && new URLSearchParams(location.search).get('page') === '1'`))
  browser('reload')
  wait(`document.querySelectorAll('article').length === 1`)
  assert.ok(evaluate(`document.querySelector('input[type="checkbox"]').checked`))
  browser('find', 'label', '仅显示含失败集的剧集', 'click')
  wait(`document.querySelectorAll('article').length === 2`)
  assert.ok(evaluate(`!new URLSearchParams(location.search).has('failed_only')`))
  browser('find', 'role', 'button', 'click', '--name', '执行记录', '--exact')
  wait(`document.body.innerText.includes('视频校验失败')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('校验中')`))
  browser('find', 'role', 'button', 'click', '--name', '下一页记录', '--exact')
  wait(`document.body.innerText.includes('第 2 页 · 共 21 条')`)
  wait(`performance.getEntriesByType('resource').some(r=>r.name.includes('/executions?') && new URL(r.name).searchParams.get('page')==='2')`)
  browser('press', 'Escape')
  assert.equal(evaluate(`document.activeElement.textContent`), '执行记录')
  assert.ok(!evaluate(`[...document.querySelectorAll('button')].some(b=>b.textContent==='补充下载')`))
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const [width, height] of [[390, 844], [768, 1024], [1024, 900], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}: overflow at ${width}`)
      if (process.env.DOWNLOAD_SCREENSHOT_DIR && [390, 1440].includes(width)) browser('screenshot', `${process.env.DOWNLOAD_SCREENSHOT_DIR}/${theme}-${width}.png`)
    }
  }
  browser('find', 'role', 'button', 'click', '--name', '重试失败集', '--exact')
  wait(`document.body.innerText.includes('已重新入队 80 集')`)
  browser('click', 'button[aria-expanded="false"][aria-controls="episodes-123"]')
  wait(`document.body.innerText.includes('E001')`)
  assert.equal(evaluate(`document.querySelector('[aria-label="分集下载进度"]').getAttribute('aria-valuenow')`), '50')
  assert.ok(evaluate(`!document.querySelector('progress') && document.querySelector('[aria-label="分集下载进度"]').getBoundingClientRect().height<=2`))
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1024, 1440]) {
      browser('set', 'viewport', String(width), '1000')
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth && [...document.querySelectorAll('[data-download-episode]')].every(row=>row.getBoundingClientRect().height<=52)`), `${theme}: episode row is not compact at ${width}`)
      if (process.env.DOWNLOAD_SCREENSHOT_DIR && [390, 1440].includes(width)) {
        evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{})))`)
        browser('screenshot', `${process.env.DOWNLOAD_SCREENSHOT_DIR}/${theme}-episodes-${width}.png`)
      }
    }
  }
  browser('click', '[data-download-episode="1"] summary')
  assert.ok(evaluate(`document.querySelector('[data-download-episode="1"] details[open]').innerText.includes('来源暂时不可用')`))
  for (const text of ['App 接口', '1080p', '1080 × 1922', 'hevc', '已下架', 'HTTP 404']) assert.ok(evaluate(`document.querySelector('[data-download-episode="1"] details[open]').innerText.includes(${JSON.stringify(text)})`))
  browser('click', '[data-download-episode="1"] summary')
  browser('find', 'role', 'button', 'click', '--name', '下一页分集', '--exact')
  wait(`document.body.innerText.includes('分集第 2 页')`)
  browser('click', '[data-download-episode="1"] button')
  wait(`performance.getEntriesByType('resource').some(r=>r.name.endsWith('/${job.id}/retry'))`)
  assert.ok(evaluate(`document.querySelector('button[aria-controls="episodes-123"]').getAttribute('aria-expanded') === 'true'`))
  browser('find', 'role', 'button', 'click', '--name', '设置', '--exact')
  wait(`document.querySelector('input[placeholder="例如 /downloads/hongguo"]')?.value === '/downloads/hongguo'`)
  assert.ok(evaluate(`document.body.innerText.includes('CD2 只备份此目录')`))
  assert.equal(evaluate(`document.querySelector('input[type="number"]').value`), '3')
  browser('find', 'role', 'button', 'click', '--name', '下载接口优先级', '--exact')
  browser('press', 'Escape')
  assert.ok(evaluate(`!!document.querySelector('[role="dialog"]') && !document.querySelector('[role="listbox"]')`), 'Escape should close only the dropdown')
  browser('find', 'label', '并发下载数量', 'fill', '2')
  browser('find', 'role', 'button', 'click', '--name', '下载接口优先级', '--exact')
  assert.ok(evaluate(`document.body.innerText.includes('App → 备用 → 官方网页（推荐）')`))
  browser('find', 'role', 'option', 'click', '--name', '备用 → App → 官方网页', '--exact')
  browser('press', 'Escape')
  assert.ok(evaluate(`!!document.querySelector('[role="dialog"]')`), 'dirty settings closed on Escape')
  browser('find', 'placeholder', '例如 /downloads/hongguo', 'fill', '/downloads/unsaved')
  wait(`performance.getEntriesByType('resource').filter(r=>r.name.includes('/works?')).length >= 3`)
  assert.equal(evaluate(`document.querySelector('input[placeholder="例如 /downloads/hongguo"]').value`), '/downloads/unsaved')
  assert.equal(evaluate(`document.querySelector('input[type="number"]').value`), '2')
  browser('find', 'role', 'button', 'click', '--name', '取消', '--exact')
  wait(`!document.querySelector('[role="dialog"]')`)
  browser('find', 'role', 'button', 'click', '--name', '设置', '--exact')
  wait(`document.querySelector('input[placeholder="例如 /downloads/hongguo"]')?.value === '/downloads/hongguo'`)
  assert.equal(evaluate(`document.querySelector('input[type="number"]').value`), '3')
  assert.ok(evaluate(`document.querySelector('button[aria-label="下载接口优先级"]').innerText.includes('官方网页 → App → 备用')`))
  wait(`getComputedStyle(document.querySelector('[role="dialog"]')).opacity === '1' && getComputedStyle(document.querySelector('.modal-backdrop')).opacity === '1'`)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const [width, height] of [[390, 844], [768, 1024], [1024, 900], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}: modal overflow at ${width}`)
      if (process.env.DOWNLOAD_SCREENSHOT_DIR && width === 390) browser('screenshot', `${process.env.DOWNLOAD_SCREENSHOT_DIR}/${theme}-settings.png`)
    }
  }
  evaluate(`window.downloadWrites=[]; const originalSend=XMLHttpRequest.prototype.send; XMLHttpRequest.prototype.send=function(body){ if(typeof body==='string' && body.includes('"concurrency"')) window.downloadWrites.push(JSON.parse(body)); return originalSend.call(this,body) }`)
  browser('find', 'label', '并发下载数量', 'fill', '11')
  browser('find', 'role', 'button', 'click', '--name', '保存设置', '--exact')
  assert.ok(evaluate(`!document.querySelector('input[type="number"]').checkValidity() && window.downloadWrites.length === 0`))
  browser('find', 'label', '并发下载数量', 'fill', '10')
  for (const invalid of ['0', '21', '1.5', '']) {
    browser('find', 'label', '并发校验数量', 'fill', invalid)
    browser('find', 'role', 'button', 'click', '--name', '保存设置', '--exact')
    assert.equal(evaluate('window.downloadWrites.length'), 0)
  }
  browser('find', 'label', '并发校验数量', 'fill', '20')
  assert.ok(evaluate(`![...document.querySelectorAll('label')].find(l=>l.textContent.includes('启用核显加速校验')).querySelector('input').checked`))
  browser('find', 'label', '启用核显加速校验（VAAPI）', 'click')
  browser('find', 'label', '完整解码校验', 'click')
  assert.ok(evaluate(`document.querySelector('input[type="checkbox"]:disabled') && [...document.querySelectorAll('label')].find(l=>l.textContent.includes('启用核显加速校验')).querySelector('input').disabled`))
  browser('find', 'role', 'button', 'click', '--name', '下载接口优先级', '--exact')
  browser('find', 'role', 'option', 'click', '--name', '备用 → App → 官方网页', '--exact')
  browser('find', 'role', 'button', 'click', '--name', '保存设置', '--exact')
  wait(`document.body.innerText.includes('下载设置已保存')`)
  assert.deepEqual(evaluate('window.downloadWrites'), [{ root: config.root, concurrency: 10, verification_concurrency: 20, full_verification: false, hardware_verification: true, priority: 'fallback' }])
  wait(`!document.querySelector('[role="dialog"]')`)
  browser('find', 'role', 'button', 'click', '--name', '设置', '--exact')
  // 静态响应使用服务端配置；真实持久化与旧请求兼容由 ConfigHTTP 测试覆盖。
  wait(`document.querySelector('input[type="number"]')?.value === '3'`)
  assert.ok(evaluate(`document.querySelector('button[aria-label="下载接口优先级"]').innerText.includes('官方网页 → App → 备用')`))
  browser('find', 'role', 'button', 'click', '--name', '取消', '--exact')
  open('/discover?system=hongguo&id=123')
  wait(`document.body.innerText.includes('下载已更新分集')`)
  browser('find', 'role', 'button', 'click', '--name', '下载已更新分集', '--exact')
  wait(`document.body.innerText.includes('已创建 1 集任务')`)
  auth('user')
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'user', is_super: false })
  open('/discover?system=hongguo&id=123')
  wait(`document.body.innerText.includes('下载测试剧')`)
  assert.ok(!evaluate(`document.body.innerText.includes('下载已更新分集')`))
  open('/admin/media/downloads')
  wait(`!document.querySelector('input[placeholder="例如 /downloads/hongguo"]') && !document.body.innerText.includes('加载中')`)
  assert.ok(!evaluate(`document.body.innerText.includes('下载设置')`))
  console.log('download-space: passed admin/viewer gates, enqueue, retry, config, date layout and responsive checks')
} catch (err) {
  console.error(evaluate('({path:location.href, text:document.body.innerText.slice(-2500), requests:performance.getEntriesByType("resource").map(r=>r.name)})'))
  console.error(browser('errors'))
  console.error(browser('console'))
  throw err
} finally {
  browser('close')
}
