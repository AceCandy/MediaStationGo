// Run against an isolated Vite server: node scripts/check-task-startup.mjs http://127.0.0.1:6239
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import process from 'node:process'
import console from 'node:console'
import { URL } from 'node:url'
import { join } from 'node:path'

const session = `task-startup-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('eval', code))
const wait = expression => browser('wait', '--fn', expression)
const controls = "document.querySelectorAll('#startup-probe button[aria-label^=\"立即执行\"], #startup-probe button[aria-label^=\"设置\"]').length"

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
    const element = document.createElement('div'); element.id = 'startup-probe'; document.body.appendChild(element);
    window.startupProbe = { calls: [], holdHistory: true, failStatus: false, status: {
      state: 'starting', stage: '建立媒体库目录监听', elapsed_seconds: 20, stage_elapsed_seconds: 15,
      directories_found: 70000, directories_watched: 128, warnings: []
    }};
    const probe = window.startupProbe;
    api.defaults.adapter = async config => {
      const call = {url: config.url, aborted: false}; probe.calls.push(call);
      config.signal?.addEventListener('abort', () => {call.aborted = true}, {once:true});
      let data = [];
      if (config.url === '/tasks/startup') {
        if (probe.failStatus) throw new Error('startup unavailable');
        data = {...probe.status};
      } else if (config.url === '/tasks') {
        if (probe.holdHistory) await new Promise(resolve => {probe.releaseHistory = resolve});
        data = {items:[],page:1,page_size:1,total:0,definitions:[{
          system:'common',key:'organize',name:'媒体整理',description:'整理下载目录',trigger:'定时 / 手动',current_state:'idle',action:'scheduler',
          schedule_config:{enabled:false,interval_seconds:300,min_interval_seconds:60,max_interval_seconds:2592000}
        }]};
      }
      return {data,status:200,statusText:'OK',headers:{},config};
    };
    probe.root = createRoot(element);
    probe.root.render(React.createElement(MemoryRouter,null,React.createElement(TasksPage)));
    return true;
  })()`)
  wait("document.querySelector('#startup-probe').textContent.includes('正在初始化：建立媒体库目录监听')")
  wait("window.startupProbe.calls.filter(c => c.url === '/tasks/startup').length >= 3")
  assert.equal(evaluate("window.startupProbe.calls.filter(c => c.url === '/tasks').length"), 1, 'slow history does not overlap or block progress')
  assert.equal(evaluate(controls), 0, 'no execution/configuration controls while starting')
  evaluate('window.startupProbe.holdHistory = false; window.startupProbe.releaseHistory(); true')
  wait("document.querySelector('#startup-probe table') !== null")
  assert.equal(evaluate(controls), 0, 'loaded definitions still cannot run before readiness')
  assert.equal(evaluate("document.querySelector('#startup-probe').textContent.includes('未就绪')"), true)

  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme = '${theme}'; true`)
    for (const [width, height] of [[390, 844], [768, 1024], [1024, 768], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.equal(evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true, `${theme} no overflow at ${width}px`)
      if (process.argv[3]) browser('screenshot', join(process.argv[3], `startup-${theme}-${width}.png`))
    }
  }
  evaluate("window.startupProbe.status = {...window.startupProbe.status, state:'ready',stage:'初始化完成'}; true")
  wait(`${controls} > 0`)
  assert.equal(evaluate("document.querySelector('#startup-probe [role=status]') === null"), true, 'successful startup banner clears')
  browser('find', 'role', 'button', 'click', '--name', '设置媒体整理周期', '--exact')
  wait("document.querySelector('[role=dialog]') !== null")
  evaluate('window.startupProbe.failStatus = true; true')
  wait("document.querySelector('#startup-probe').textContent.includes('暂时无法获取启动状态')")
  assert.equal(evaluate(controls), 0, 'status failure hides stale controls')
  assert.equal(evaluate("document.querySelector('[role=dialog]') === null"), true, 'status failure closes configuration')
  evaluate('window.startupProbe.failStatus = false; true')
  wait(`${controls} > 0`)
  assert.equal(evaluate("document.querySelector('[role=dialog]') === null"), true, 'recovery does not reopen stale configuration')
  evaluate("window.startupProbe.failStatus = false; window.startupProbe.status = {...window.startupProbe.status,state:'failed',stage:'恢复任务执行状态',warnings:['恢复任务执行状态未完成，请查看服务日志']}; true")
  wait("document.querySelector('#startup-probe').textContent.includes('初始化未完成：恢复任务执行状态')")
  assert.equal(evaluate(controls), 0)
  evaluate('window.startupProbe.holdHistory = true; true')
  wait("window.startupProbe.calls.filter(c => c.url === '/tasks').length >= 2")
  evaluate('window.startupProbe.root.unmount(); true')
  assert.equal(evaluate("window.startupProbe.calls.filter(c => c.url === '/tasks').at(-1).aborted"), true, 'unmount cancels delayed history')
  assert.equal(evaluate("window.startupProbe.calls.filter(c => c.url === '/tasks/startup').at(-1).aborted"), true, 'unmount cancels readiness requests')
  evaluate('window.startupProbe.releaseHistory(); true')
  assert.equal(browser('errors').trim(), '', 'no browser runtime errors')
  console.log('PASS: independent progress, hidden controls, readiness/failure transitions, responsive themes, cancellation')
} finally {
  browser('close')
}
