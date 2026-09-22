// 使用独立 Vite 开发服务；所有 API 均在浏览器内模拟。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const session = `search-loading-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('eval', '-b', Buffer.from(code).toString('base64')))
const wait = code => browser('wait', '--fn', code)
try {
  browser('open', `${process.argv[2] ?? 'http://127.0.0.1:6201'}/login`)
  evaluate(`(async () => {
    const {SearchPage} = await import('/src/pages/SearchPage.tsx');
    const {useLayoutSearch} = await import('/src/components/useLayoutSearch.ts');
    const {api} = await import('/src/api/client.ts');
    const {useAuthStore} = await import('/src/stores/auth.ts');
    const dependency = name => performance.getEntriesByType('resource').find(e=>new URL(e.name).pathname.endsWith('/'+name+'.js')).name;
    const react = await import(dependency('react'));
    const dom = await import(dependency('react-dom_client'));
    const React = react.default ?? react, h = React.createElement;
    const {createRoot} = dom.default ?? dom;
    const {MemoryRouter, useNavigate, useLocation, Routes, Route} = await import(dependency('react-router-dom'));
    document.getElementById('root').style.display='none';
    const probe = window.searchProbe = { calls:[], fail:false, hold:false, releases:[] };
    api.defaults.adapter = async config => {
      if(config.url==='/ai/status') return {data:{enabled:true},status:200,headers:{},config};
      const call={url:config.url, ...config.params, aborted:false}; probe.calls.push(call);
      config.signal?.addEventListener('abort',()=>{call.aborted=true},{once:true});
      if(probe.hold) await new Promise(resolve=>probe.releases.push(resolve));
      if(probe.fail && config.params?.page===2) throw new Error('test failure');
      const q=config.params?.q??'AI', page=config.params?.page??1;
      const count=q==='empty'||(q==='sparse'&&page===1)?0:config.url==='/ai/search'?1:page===3?5:30;
      const items=Array.from({length:count},(_,i)=>({id:q+'-'+((page-1)*30+i),title:q+'条目'+((page-1)*30+i),metadata_id:q+'-'+((page-1)*30+i),metadata_kind:'movie',type:'movie'}));
      return {data:{items,total:q==='empty'?0:65,external_items:[]},status:200,headers:{},config};
    };
    useAuthStore.setState({user:{id:'probe',role:'admin',username:'测试'},token:null});
    function Shell(){
      const navigate=useNavigate(), location=useLocation();probe.navigate=navigate;
      const layout=useLayoutSearch({pathname:location.pathname,locationSearch:location.search,navigate});probe.layout=layout;
      return h(Routes,null,h(Route,{path:'/search',element:h(SearchPage)}),h(Route,{path:'*',element:h('p',null,'离开搜索')}));
    }
    const element=document.createElement('div');element.id='search-probe';document.body.appendChild(element);
    probe.root=createRoot(element);
    probe.root.render(h(React.StrictMode,null,h(MemoryRouter,{initialEntries:['/search?q=first']},h(Shell))));
    return true;
  })()`)
  wait("document.querySelector('#search-probe').textContent.includes('已加载 30/65')")
  assert.equal(evaluate("window.searchProbe.calls.filter(c=>c.url==='/media').length"), 1)
  assert.equal(evaluate("window.searchProbe.calls.find(c=>c.url==='/media').page_size"), 30)
  evaluate('window.searchProbe.fail=true; true')
  browser('find', 'role', 'button', 'click', '--name', '加载更多', '--exact')
  wait("document.querySelector('#search-probe').textContent.includes('搜索失败')")
  assert.ok(evaluate("document.querySelector('#search-probe').textContent.includes('已加载 30/65')"))
  evaluate('window.searchProbe.fail=false; true')
  browser('find', 'role', 'button', 'click', '--name', '加载更多', '--exact')
  wait("document.querySelector('#search-probe').textContent.includes('已加载 60/65')")
  assert.deepEqual(evaluate("window.searchProbe.calls.filter(c=>c.url==='/media').map(c=>c.page)"), [1,2,2])
  browser('find', 'role', 'button', 'click', '--name', '加载更多', '--exact')
  wait("document.querySelector('#search-probe').textContent.includes('65 个条目')")
  assert.equal(evaluate("[...document.querySelectorAll('#search-probe button')].some(b=>b.textContent==='加载更多')"), false)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'; true`)
    for (const [width, height] of [[390, 844], [768, 1024], [1440, 900]]) {
      browser('set', 'viewport', String(width), String(height))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width}`)
    }
  }
  evaluate("window.searchProbe.navigate('/search?q=page-cancel'); true")
  wait("document.querySelector('#search-probe').textContent.includes('已加载 30/65')")
  evaluate('window.searchProbe.hold=true; true')
  browser('find', 'role', 'button', 'click', '--name', '加载更多', '--exact')
  wait("window.searchProbe.calls.some(c=>c.q==='page-cancel' && c.page===2)")
  evaluate("window.searchProbe.hold=false; window.searchProbe.navigate('/search?q=empty'); true")
  wait("document.querySelector('#search-probe').textContent.includes('未找到匹配')")
  assert.ok(evaluate("window.searchProbe.calls.find(c=>c.q==='page-cancel' && c.page===2).aborted"))
  evaluate('window.searchProbe.releases.splice(0).forEach(resolve=>resolve()); true')
  evaluate("window.searchProbe.hold=true; window.searchProbe.navigate('/search?q=old'); true")
  wait("window.searchProbe.calls.some(c=>c.q==='old')")
  evaluate("window.searchProbe.hold=false; window.searchProbe.navigate('/search?q=empty'); true")
  wait("document.querySelector('#search-probe').textContent.includes('未找到匹配')")
  assert.ok(evaluate("window.searchProbe.calls.find(c=>c.q==='old').aborted"))
  evaluate('window.searchProbe.releases.splice(0).forEach(resolve=>resolve()); true')
  assert.ok(evaluate("document.querySelector('#search-probe').textContent.includes('未找到匹配')"))
  evaluate("window.searchProbe.navigate('/search?q=sparse'); true")
  wait("document.querySelector('#search-probe').textContent.includes('已加载 0/65')")
  browser('find', 'role', 'button', 'click', '--name', '加载更多', '--exact')
  wait("document.querySelector('#search-probe').textContent.includes('已加载 30/65')")
  assert.deepEqual(evaluate("window.searchProbe.calls.filter(c=>c.q==='sparse').map(c=>c.page)"), [1,2])
  evaluate("window.searchProbe.navigate('/search?q=smart&mode=ai'); true")
  wait("document.querySelector('#search-probe').textContent.includes('1 个条目')")
  assert.equal(evaluate("window.searchProbe.calls.filter(c=>c.url==='/ai/search').length"), 1)
  evaluate("window.searchProbe.hold=true; window.searchProbe.navigate('/search?q=smart-old&mode=ai'); true")
  wait("window.searchProbe.calls.filter(c=>c.url==='/ai/search').length===2")
  evaluate("window.searchProbe.hold=false; window.searchProbe.navigate('/search?q=empty'); true")
  wait("document.querySelector('#search-probe').textContent.includes('未找到匹配')")
  assert.ok(evaluate("window.searchProbe.calls.filter(c=>c.url==='/ai/search').at(-1).aborted"))
  evaluate('window.searchProbe.releases.splice(0).forEach(resolve=>resolve()); true')
  evaluate("window.searchProbe.hold=true; window.searchProbe.navigate('/search?q=unmount'); true")
  wait("window.searchProbe.calls.some(c=>c.q==='unmount')")
  evaluate("window.searchProbe.navigate('/away'); true")
  wait("window.searchProbe.calls.find(c=>c.q==='unmount').aborted")
  evaluate("window.searchProbe.layout.setQuery('suggest-old'); window.searchProbe.layout.setFocused(true); true")
  wait("window.searchProbe.calls.some(c=>c.q==='suggest-old')")
  evaluate("window.searchProbe.hold=false; window.searchProbe.layout.setQuery('suggest-new'); true")
  wait("window.searchProbe.calls.some(c=>c.q==='suggest-new')")
  assert.ok(evaluate("window.searchProbe.calls.find(c=>c.q==='suggest-old').aborted"))
  evaluate('window.searchProbe.releases.splice(0).forEach(resolve=>resolve()); true')
  wait("window.searchProbe.layout.cards[0]?.rep.title.startsWith('suggest-new')")
  evaluate("window.searchProbe.hold=true; window.searchProbe.layout.setQuery('suggest-unmount'); true")
  wait("window.searchProbe.calls.some(c=>c.q==='suggest-unmount')")
  evaluate('window.searchProbe.root.unmount(); true')
  assert.ok(evaluate("window.searchProbe.calls.find(c=>c.q==='suggest-unmount').aborted"))
  assert.equal(browser('errors').trim(), '')
  console.log('PASS: 首屏 30、翻页重试、结果完整、过期/卸载取消、顶栏取消、AI 模式、StrictMode')
} finally { browser('close') }
