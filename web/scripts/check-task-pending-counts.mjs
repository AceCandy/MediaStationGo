// Run against an isolated Vite server: node scripts/check-task-pending-counts.mjs http://127.0.0.1:6239
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import process from 'node:process'
import console from 'node:console'
import { URL } from 'node:url'

const session = `task-pending-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('eval', code))
const wait = expression => browser('wait', '--fn', expression)
const click = name => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const countCalls = 'window.pendingProbe.calls.filter(c => c.url === "/tasks/pending-counts")'

try {
  browser('open', new URL('/login', process.argv[2] ?? 'http://127.0.0.1:6239').href)
  evaluate(`(async () => {
    const {TasksPage} = await import('/src/pages/TasksPage.tsx');
    const {api} = await import('/src/api/client.ts');
    const dependency = name => performance.getEntriesByType('resource').find(entry => new URL(entry.name).pathname.endsWith('/' + name + '.js')).name;
    const react = await import(dependency('react'));
    const dom = await import(dependency('react-dom_client'));
    const React = react.default ?? react;
    const {createRoot} = dom.default ?? dom;
    const {MemoryRouter} = await import(dependency('react-router-dom'));
    document.getElementById('root').style.display = 'none';
    const element = document.createElement('div'); element.id = 'pending-probe'; document.body.appendChild(element);
    const probe = window.pendingProbe = {calls:[],hold:true,fail:false,counts:{rechecks:11,scrape_issues:9,updated_at:'2026-09-24T07:00:00Z'}};
    api.defaults.adapter = async config => {
      const call = {url:config.url,refresh:config.params?.refresh,aborted:false}; probe.calls.push(call);
      config.signal?.addEventListener('abort', () => {call.aborted=true}, {once:true});
      let data = [];
      if (config.url === '/tasks/startup') data = {state:'ready',stage:'初始化完成',elapsed_seconds:1,stage_elapsed_seconds:0,directories_found:0,directories_watched:0,warnings:[]};
      else if (config.url === '/tasks') data = {items:[],definitions:[{system:'catalog',key:'media_scrape',name:'媒体入库刮削',description:'处理待刮削媒体',trigger:'事件 / 手动',current_state:'idle',action:'media_scrape'}]};
      else if (config.url === '/tasks/pending-counts') {
        if (probe.hold) await new Promise(resolve => {probe.release=resolve});
        if (probe.fail) throw new Error('counts unavailable');
        data = {...probe.counts};
      } else if (config.url === '/media/scrape-issues') data = {items:[],total:0,page:1,page_size:30};
      return {data,status:200,statusText:'OK',headers:{},config};
    };
    probe.mount = () => {probe.root=createRoot(element);probe.root.render(React.createElement(MemoryRouter,null,React.createElement(TasksPage)))};
    probe.mount(); return true;
  })()`)
  wait('document.querySelector("#pending-probe table") !== null')
  assert.equal(evaluate(`${countCalls}.length`), 1, 'one initial cache read')
  assert.equal(evaluate(`${countCalls}[0].refresh === undefined`), true, 'entry does not force recomputation')
  assert.equal(evaluate(`document.querySelector('#pending-probe button[aria-label="刷新待办统计"]').disabled`), true)
  evaluate('window.pendingProbe.hold=false; window.pendingProbe.release(); true')
  wait('document.querySelector("#pending-probe").textContent.includes("待办统计更新于")')
  wait('window.pendingProbe.calls.filter(c => c.url === "/tasks").length >= 2')
  assert.equal(evaluate(`${countCalls}.length`), 1, 'task polling never refreshes counts')
  click('查看媒体入库刮削待处理，9 条')
  wait('document.querySelector("[role=dialog]") !== null')
  click('关闭媒体入库刮削待处理')
  wait('document.querySelector("[role=dialog]") === null')
  assert.equal(evaluate(`${countCalls}.length`), 1, 'closing a dialog does not refresh counts')
  evaluate('window.pendingProbe.fail=true; true')
  click('刷新待办统计')
  wait('document.querySelector("#pending-probe [role=alert]") !== null')
  assert.equal(evaluate(`${countCalls}.at(-1).refresh`), 1, 'manual refresh requests recomputation')
  assert.equal(evaluate('document.querySelector("#pending-probe").textContent.includes("保留上次结果")'), true)
  assert.equal(evaluate(`document.querySelector('#pending-probe button[aria-label="查看媒体入库刮削待处理，9 条"]') !== null`), true)
  evaluate('window.pendingProbe.fail=false; window.pendingProbe.counts.scrape_issues=3; true')
  click('刷新待办统计')
  browser('wait', '#pending-probe button[aria-label="查看媒体入库刮削待处理，3 条"]')
  wait('document.querySelector("#pending-probe [role=alert]") === null')
  assert.equal(evaluate('document.querySelector("#pending-probe [role=alert]") === null'), true)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme = '${theme}'; true`)
    for (const [width, height] of [[390,844],[768,1024],[1024,768],[1440,900]]) {
      browser('set','viewport',String(width),String(height))
      assert.equal(evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true, `${theme} no overflow at ${width}px`)
    }
  }
  evaluate('window.pendingProbe.root.unmount(); window.pendingProbe.mount(); true')
  wait(`${countCalls}.length === 4`)
  assert.equal(evaluate(`${countCalls}.at(-1).refresh === undefined`), true, 'returning to the page reads cached counts')
  evaluate('window.pendingProbe.hold=true; true')
  click('刷新待办统计')
  wait(`${countCalls}.length === 5`)
  evaluate('window.pendingProbe.root.unmount(); true')
  assert.equal(evaluate(`${countCalls}.at(-1).aborted`), true, 'unmount aborts refresh')
  evaluate('window.pendingProbe.release(); true')
  assert.equal(browser('errors').trim(), '', 'no runtime errors')
  console.log('PASS: cached entry, independent task loading, manual refresh, retained errors, dialog close, remount, responsive themes and cancellation')
} finally {
  browser('close')
}
