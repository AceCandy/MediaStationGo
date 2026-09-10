// Run against a Vite dev server: node scripts/check-storage-loading.mjs http://127.0.0.1:6200
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import process from 'node:process'
import console from 'node:console'
import { URL } from 'node:url'

const session = `storage-loading-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], {
  encoding: 'utf8', timeout: 30000,
})
const evaluate = code => JSON.parse(browser('eval', code))

try {
  browser('open', new URL('/login', process.argv[2] ?? 'http://127.0.0.1:6200').href)
  evaluate(`(async () => {
    const {LayoutWorkspace} = await import('/src/components/LayoutSections.tsx');
    const {StoragePage} = await import('/src/pages/StoragePage.tsx');
    const {api} = await import('/src/api/client.ts');
    // Reuse Vite's exact module URLs so the router contexts are shared with the app.
    const dependency = name => performance.getEntriesByType('resource')
      .find(entry => new URL(entry.name).pathname.endsWith('/' + name + '.js')).name;
    const react = await import(dependency('react'));
    const dom = await import(dependency('react-dom_client'));
    const React = react.default ?? react;
    const {createRoot} = dom.default ?? dom;
    const {MemoryRouter,Routes,Route,Link,useLocation} = await import(dependency('react-router-dom'));
    document.getElementById('root').style.display = 'none';
    const h = React.createElement;
    window.storageProbe = { calls: [], delay: 250 };
    const probe = window.storageProbe;
    // All API calls stay in this browser; no authenticated backend access is needed.
    api.defaults.adapter = async config => {
      const call = {url: config.url, aborted: false};
      probe.calls.push(call);
      config.signal?.addEventListener('abort', () => { call.aborted = true; }, {once:true});
      if (config.url === '/storage') await new Promise(resolve => setTimeout(resolve, probe.delay));
      return {data: config.url === '/storage' ? {total_bytes:0,by_library:[]} : {
        goroutines:1,go_version:'test',cpu_percent:0,memory_total:1,memory_used:0,disk_total:1,disk_used:0
      },status:200,statusText:'OK',headers:{},config};
    };
    function Shell() {
      const location = useLocation();
      return h('div', null,
        h(Link,{to:'/',id:'probe-home'},'home'),
        h(Link,{to:'/storage',id:'probe-storage'},'storage'),
        h(LayoutWorkspace,{routeKey:location.pathname}));
    }
    probe.mount = (strict, path = '/') => {
      probe.root?.unmount();
      document.getElementById('storage-probe')?.remove();
      probe.calls = [];
      const element = document.createElement('div');
      element.id = 'storage-probe';
      document.body.appendChild(element);
      probe.root = createRoot(element);
      const routes = h(MemoryRouter,{initialEntries:[path]},h(Routes,null,
        h(Route,{element:h(Shell)},h(Route,{index:true,element:h('p',null,'home page')}),
          h(Route,{path:'storage',element:h(StoragePage)}))));
      probe.root.render(strict ? h(React.StrictMode,null,routes) : routes);
    };
    return true;
  })()`)

  for (const strict of [false, true]) {
    evaluate(`window.storageProbe.mount(${strict}); true`)
    browser('wait', '#probe-storage')
    browser('click', '#probe-storage')
    browser('wait', '--fn', "document.querySelector('#storage-probe table') !== null")
    assert.equal(evaluate("window.storageProbe.calls.filter(c => c.url === '/storage').length"), 1,
      `one storage request on navigation, strict=${strict}`)
    browser('wait', '--fn', "window.storageProbe.calls.filter(c => c.url === '/stats/monitor').length >= 3")
    assert.equal(evaluate("window.storageProbe.calls.filter(c => c.url === '/storage').length"), 1,
      'hardware polling does not repeat storage statistics')

    browser('click', '#probe-home')
    browser('wait', '--fn', "document.querySelector('#storage-probe').textContent.includes('home page')")
    evaluate('window.storageProbe.delay = 1500; true')
    browser('click', '#probe-storage')
    browser('wait', '--fn', "window.storageProbe.calls.filter(c => c.url === '/storage').length === 2")
    browser('click', '#probe-home')
    browser('wait', '--fn', "window.storageProbe.calls.filter(c => c.url === '/storage').at(-1).aborted")
    assert.equal(evaluate("window.storageProbe.calls.filter(c => c.url === '/storage').at(-1).aborted"), true,
      'navigation away cancels pending storage request')
    evaluate('window.storageProbe.delay = 250; true')
  }
  evaluate("window.storageProbe.mount(true, '/storage'); true")
  browser('wait', '--fn', "document.querySelector('#storage-probe table') !== null")
  assert.equal(evaluate("window.storageProbe.calls.filter(c => c.url === '/storage').length"), 1,
    'direct load also sends one storage request in StrictMode')
  for (const [width, height] of [[390, 844], [768, 1024], [1440, 900]]) {
    browser('set', 'viewport', String(width), String(height))
    assert.equal(evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true,
      `no document overflow at ${width}px`)
  }
  assert.equal(browser('errors').trim(), '', 'no browser runtime errors')
  console.log('PASS: route navigation, StrictMode, direct load, monitor polling, and cancellation')
} finally {
  browser('close')
}
