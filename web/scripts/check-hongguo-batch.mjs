// 先启动本地 Web 预览；全部 API 使用模拟响应，不写实际下载队列或聚合。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'
import { setTimeout as delay } from 'node:timers/promises'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `hongguo-batch-${process.pid}`
function browser(...args) { return execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 }) }
function evaluate(code) {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
function waitFor(code) { browser('wait', '--fn', code) }
function click(name) { browser('find', 'role', 'button', 'click', '--name', name, '--exact') }
function route(path, data) { browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data)) }
const items = ['山海初见', '山海归来', '待补齐作品', '独立电影'].map((title, index) => ({
  id: `work-${index}`, source_id: String(91001 + index), title, hydrated: index !== 2,
  kind: index === 3 ? 'movie' : 'series', source_category: 'real-drama', artwork_id: '',
  tags: [], rating: 0, episode_count: 10,
}))
function setup(root = '/mock/downloads', role = 'admin') {
  browser('network', 'unroute')
  route('auth/permissions', { permissions: { can_view_discover: true }, role, is_super: role === 'admin' })
  route('play-profiles', [])
  route('catalogs/hongguo/search?*', { items, total: items.length })
  route('catalogs/hongguo/works?*', { items, total: items.length })
  route('catalogs/hongguo/status', { enabled: true })
  route('catalogs/hongguo/downloads/config', { root })
  route('catalogs/hongguo/downloads', { added: 10 })
  route('catalogs/hongguo/groups/mock-group', { id: 'mock-group', title: '山海合集', members: items.slice(0, 2).map((work, index) => ({ ...work, season_number: index + 1 })) })
  route('catalogs/hongguo/groups', { id: 'mock-group' })
  route('**', {})
  evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'test-token',user:{id:${JSON.stringify(role)},username:'测试',role:${JSON.stringify(role)},tier:'free'}},version:0}))`)
  browser('open', `${base}/discover?system=hongguo&keyword=山海`)
  waitFor(`document.querySelector('[aria-label="查看山海初见"]') !== null`)
  evaluate(`window.batchWrites=[]; window.failSource=''; window.failGroup=false; window.holdSource='';
    const open=XMLHttpRequest.prototype.open, send=XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.open=function(method,url,...rest){this.batchURL=String(url);this.batchMethod=method;return open.call(this,method,url,...rest)};
    XMLHttpRequest.prototype.send=function(body){
      if(this.batchMethod==='GET' && this.batchURL.includes('/hongguo/works?') && window.failLocal) throw new Error('模拟本地列表失败');
      if(this.batchMethod==='POST') {
        window.batchWrites.push({url:this.batchURL,body:JSON.parse(body)});
        if((this.batchURL.endsWith('/downloads') && JSON.parse(body).source_id===window.failSource) || (this.batchURL.endsWith('/groups') && window.failGroup)) throw new Error('模拟请求失败');
        if(this.batchURL.endsWith('/downloads') && JSON.parse(body).source_id===window.holdSource) { window.releaseBatch=()=>send.call(this,body); return; }
      }
      return send.call(this,body);
    }`)
}
function selectPair() { click('多选'); click('选择山海归来'); click('选择山海初见') }

// 复用本脚本独有的浏览器会话，通过浏览器原生触摸事件验证手柄，不依赖合成 DOM 事件。
async function touchDrag() {
  const { cdpUrl } = JSON.parse(browser('--json', 'get', 'cdp-url')).data
  const socket = new globalThis.WebSocket(cdpUrl)
  await new Promise((resolve, reject) => { socket.addEventListener('open', resolve, { once: true }); socket.addEventListener('error', reject, { once: true }) })
  let sequence = 0
  const send = (method, params, sessionId) => new Promise((resolve, reject) => {
    const id = ++sequence
    const receive = (event) => { const result = JSON.parse(event.data); if (result.id !== id) return; socket.removeEventListener('message', receive); if (result.error) reject(new Error(result.error.message)); else resolve(result.result) }
    socket.addEventListener('message', receive); socket.send(JSON.stringify({ id, method, params, sessionId }))
  })
  try {
    const { targetInfos } = await send('Target.getTargets', {})
    const target = targetInfos.find((item) => item.type === 'page' && item.url.startsWith(base))
    assert.ok(target)
    const { sessionId } = await send('Target.attachToTarget', { targetId: target.targetId, flatten: true })
    await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 1 }, sessionId)
    const [a, b] = evaluate(`[...document.querySelectorAll('[role="dialog"] button[aria-label^="拖动"]')].map(button=>{if(getComputedStyle(button).touchAction!=='none')throw new Error('touch action');const r=button.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})`)
    await send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ ...a, id: 1 }] }, sessionId)
    for (let i = 1; i <= 12; i++) { await send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: a.x, y: a.y + (b.y - a.y) * i / 12, id: 1 }] }, sessionId); await delay(32) }
    await send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] }, sessionId)
    await send('Emulation.setTouchEmulationEnabled', { enabled: false }, sessionId)
  } finally { socket.close() }
}

try {
  browser('open', `${base}/login`)
  setup()
  selectPair()
  assert.ok(evaluate(`document.querySelector('[aria-label="选择待补齐作品"]').disabled`))
  assert.equal(evaluate(`document.querySelector('[role="dialog"]')`), null)
  assert.equal(evaluate(`new URLSearchParams(location.search).get('id')`), null)
  click('选择山海初见'); click('选择山海初见')
  assert.ok(evaluate(`document.body.innerText.includes('已选 2 部')`))
  click('聚合所选')
  waitFor(`document.querySelector('[aria-label="确认跨季聚合"]') !== null`)
  assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="dialog"] li')].map(li=>li.innerText.match(/9100[12]/)[0])`), ['91002', '91001'])
  click('上移山海初见')
  assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="dialog"] li')].map(li=>li.innerText.match(/9100[12]/)[0])`), ['91001', '91002'])
  waitFor(`document.querySelector('[role="dialog"]').getAnimations({subtree:true}).every(a=>a.playState!=='running')`)
  const [start, end] = evaluate(`[...document.querySelectorAll('[role="dialog"] button[aria-label^="拖动"]')].map(b=>{const r=b.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})`)
  browser('mouse', 'move', String(Math.round(start.x)), String(Math.round(start.y))); browser('mouse', 'down')
  for (let step = 1; step <= 8; step++) browser('mouse', 'move', String(Math.round(start.x)), String(Math.round(start.y + (end.y - start.y) * step / 8)))
  browser('mouse', 'up')
  waitFor(`[...document.querySelectorAll('[role="dialog"] li')][0].innerText.includes('91002')`)
  browser('focus', '[aria-label="拖动山海初见调整季序"]'); browser('press', 'ArrowUp')
  assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="dialog"] li')].map(li=>li.innerText.match(/9100[12]/)[0])`), ['91001', '91002'])
  waitFor(`[...document.querySelectorAll('[role="dialog"] li')].every(li=>getComputedStyle(li).transform==='none')`)
  await touchDrag()
  waitFor(`[...document.querySelectorAll('[role="dialog"] li')][0].innerText.includes('91002')`)
  click('上移山海初见')
  browser('find', 'label', '聚合剧名', 'fill', '山海合集')
  browser('press', 'Shift+Tab')
  assert.equal(evaluate(`document.activeElement.textContent`), '确认聚合')
  browser('press', 'Tab')
  assert.equal(evaluate(`document.activeElement.tagName`), 'INPUT')
  browser('press', 'Escape')
  assert.ok(evaluate(`document.querySelector('[role="dialog"]') !== null`))
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 768, 1024, 1440]) {
      browser('set', 'viewport', String(width), '900')
      waitFor(`document.querySelector('[role="dialog"]').getAnimations({subtree:true}).every(a=>a.playState!=='running') && Number(getComputedStyle(document.querySelector('.modal-backdrop')).opacity)===1`)
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth && document.querySelector('[role="dialog"]').scrollWidth<=document.querySelector('[role="dialog"]').clientWidth`))
      if (process.env.DISCOVER_SCREENSHOT_DIR && width === 390) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/batch-${theme}.png`)
    }
  }
  click('取消')
  assert.equal(evaluate(`window.batchWrites.length`), 0)
  assert.equal(evaluate(`document.activeElement.textContent`), '聚合所选')
  browser('press', 'Enter')
  waitFor(`document.querySelector('[aria-label="上移山海初见"]') !== null`)
  click('上移山海初见')
  browser('find', 'label', '聚合剧名', 'fill', '山海合集')
  evaluate(`window.failGroup=true`)
  click('确认聚合')
  waitFor(`document.body.innerText.includes('聚合失败')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"] input').value==='山海合集'`))
  evaluate(`window.failGroup=false`)
  click('确认聚合')
  waitFor(`!document.querySelector('[role="dialog"]') && document.body.innerText.includes('聚合已保存')`)
  assert.deepEqual(evaluate(`window.batchWrites.at(-1).body`), { title: '山海合集', members: [{ source_id: '91001', season_number: 1 }, { source_id: '91002', season_number: 2 }] })
  assert.ok(evaluate(`document.body.innerText.includes('已选 0 部')`))
  assert.equal(evaluate(`document.activeElement.textContent`), '已选 0 部')
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label="已关联剧集"]').length`), 2)
  assert.ok(evaluate(`[...document.querySelectorAll('button[aria-label="已关联剧集"]')].every(b=>!b.textContent && b.querySelector('svg') && b.getBoundingClientRect().width===24 && !b.parentElement.closest('button'))`))
  evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{})))`)
  browser('find', 'first', 'button[aria-controls="group-91001"]', 'hover')
  waitFor(`document.querySelector('#group-91001')?.innerText.includes('山海归来')`)
  assert.ok(evaluate(`document.querySelector('#group-91001').innerText.includes('第 2 季') && !document.querySelector('#group-91001').innerText.includes('山海初见')`))
  browser('focus', 'button[aria-controls="group-91001"]'); browser('press', 'Tab')
  assert.equal(evaluate(`document.activeElement.id`), 'group-91001')
  browser('press', 'Escape')
  waitFor(`!document.querySelector('#group-91001')`)
  assert.ok(evaluate(`!document.querySelector('#group-91001') && document.activeElement.getAttribute('aria-controls')==='group-91001'`))
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 1440]) {
      browser('set', 'viewport', String(width), '900')
      // 断点切换的布局过渡结束后再取点击坐标，避免落在移动中的卡片之外。
      evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{}))).then(()=>true)`)
      assert.ok(evaluate(`(()=>{const b=document.querySelector('button[aria-controls="group-91001"]');const label=b.parentElement.parentElement.querySelector('[data-hongguo-episode-label]');const r=b.getBoundingClientRect(),t=label.getBoundingClientRect();return r.right<=t.left && t.left-r.right<=8 && Math.abs((r.top+r.bottom-t.top-t.bottom)/2)<1})()`), 'group icon must precede and align with episode label')
      browser('click', 'button[aria-controls="group-91001"]')
      waitFor(`document.querySelector('#group-91001')?.innerText.includes('山海归来')`)
      assert.ok(evaluate(`(()=>{const r=document.querySelector('#group-91001').getBoundingClientRect();return r.left>=0 && r.right<=innerWidth && r.bottom<=innerHeight})()`))
      assert.ok(evaluate(`(()=>{const p=document.querySelector('#group-91001').getBoundingClientRect(),b=document.querySelector('button[aria-controls="group-91001"]').getBoundingClientRect();return p.bottom<=b.top || p.top>=b.bottom})()`), 'popup must not cover its trigger')
      assert.equal(evaluate(`new URLSearchParams(location.search).get('id')`), null)
      if (process.env.DISCOVER_SCREENSHOT_DIR) {
        evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect?.getTiming().iterations!==Infinity).map(a=>a.finished.catch(()=>{})))`)
        assert.ok(evaluate(`document.querySelector('#group-91001')?.innerText.includes('山海归来')`))
        browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/group-${theme}-${width}.png`)
      }
      browser('press', 'Escape')
    }
  }
  click('选择独立电影'); click('选择山海初见')
  assert.ok(evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent==='聚合所选').disabled`))
  click('清空选择')
  click('选择山海初见'); click('选择山海归来')
  evaluate(`window.failSource='91002'`)
  click('下载所选')
  waitFor(`document.body.innerText.includes('1 部失败，已保留勾选')`)
  assert.ok(evaluate(`document.querySelector('[aria-label="选择山海归来"]').getAttribute('aria-pressed')==='true' && document.querySelector('[aria-label="选择山海初见"]').getAttribute('aria-pressed')==='false'`))
  evaluate(`window.failSource=''`)
  click('下载所选')
  waitFor(`document.body.innerText.includes('已选 0 部')`)
  assert.deepEqual(evaluate(`window.batchWrites.filter(r=>r.url.endsWith('/downloads')).map(r=>r.body.source_id)`), ['91001', '91002', '91002'])
  click('选择山海初见'); click('分类')
  waitFor(`document.querySelector('[aria-label="查看山海初见"]') !== null`)
  assert.ok(!evaluate(`document.body.innerText.includes('已选 1 部')`))
  setup()
  selectPair()
  evaluate(`window.holdSource='91002'`)
  click('下载所选')
  waitFor(`typeof window.releaseBatch==='function'`)
  assert.ok(evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent==='下载所选').disabled`))
  click('分类')
  waitFor(`document.querySelector('[aria-label="查看山海初见"]')!==null`)
  evaluate(`window.releaseBatch()`)
  waitFor(`performance.getEntriesByType('resource').some(r=>r.name.endsWith('/catalogs/hongguo/downloads'))`)
  assert.equal(evaluate(`window.batchWrites.length`), 1)
  assert.ok(!evaluate(`document.body.innerText.includes('已提交')`))
  setup('')
  selectPair(); click('下载所选')
  waitFor(`document.body.innerText.includes('请先在下载空间设置存储目录')`)
  assert.equal(evaluate(`window.batchWrites.length`), 0)
  assert.ok(evaluate(`document.body.innerText.includes('已选 2 部')`))
  setup()
  evaluate(`window.failLocal=true`)
  click('刷新红果目录')
  waitFor(`document.body.innerText.includes('本地资料加载失败') && !!document.querySelector('[aria-label="查看山海初见"]')`)
  selectPair(); click('聚合所选')
  waitFor(`!!document.querySelector('[role="dialog"]')`)
  click('确认聚合')
  waitFor(`!document.querySelector('[role="dialog"]') && document.body.innerText.includes('聚合已保存')`)
  evaluate(`window.failLocal=false`); click('重试加载')
  waitFor(`!document.body.innerText.includes('本地资料加载失败')`)
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label="已关联剧集"]').length`), 2, 'late local results erased saved membership')
  setup('/mock/downloads', 'user')
  assert.ok(!evaluate(`[...document.querySelectorAll('button')].some(b=>b.textContent==='多选')`))
  assert.equal(browser('errors').trim(), '')
  console.log('红果多选：季序、取消、失败保留、下载请求、未配置目录、权限与双主题布局通过')
} catch (err) { console.error(evaluate(`({text:document.body.innerText, requests:performance.getEntriesByType('resource').filter(r=>r.name.includes('/api/')).map(r=>r.name)})`)); console.error(browser('errors')); throw err }
finally { browser('close') }
