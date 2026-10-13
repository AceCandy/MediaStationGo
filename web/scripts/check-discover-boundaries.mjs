// 用模拟接口验证失败页、会话隔离、无图卡片和嵌套弹窗边界。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:6241'
const session = `discover-boundaries-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const wait = code => browser('wait', '--fn', `Boolean(${code})`)
const click = name => browser('find', 'role', 'button', 'click', '--name', name, '--exact')

try {
  browser('open', base + '/login')
  browser('set', 'media', 'dark', 'reduced-motion')
  browser('set', 'viewport', '1000', '500')
  evaluate(`(async()=>{
    const {api}=await import('/src/api/client.ts');
    const {useAuthStore}=await import('/src/stores/auth.ts');
    const p=window.boundaryProbe={auth:useAuthStore,pages:[],sections:[],hold:true,releases:[],sectionsError:false,pageError:true};
    const item=id=>({source:'tmdb',tmdb_id:id,media_type:'movie',title:'作品 '+id,poster_url:id===1?'':id===2?'/api/../missing-image.svg':'/api/../brand/mediastationgo-logo.svg'});
    api.defaults.adapter=async config=>{
      let data={};
      if(config.url==='/auth/permissions') data={permissions:{can_view_discover:true},role:'admin',is_super:true};
      else if(config.url==='/play-profiles') data=[];
      else if(config.url==='/discover/sections') {
        p.sections.push(useAuthStore.getState().sessionVersion);
        if(p.hold){p.hold=false;await new Promise(resolve=>p.releases.push(resolve));}
        if(p.sectionsError) throw new Error('Network error');
        data={sections:[{key:'tmdb_trending_day',label:'测试趋势',provider:'tmdb'}]};
      } else if(config.url==='/discover/feed') {
        const page=config.params.page;p.pages.push(page);
        data={tmdb_trending_day:page===1?Array.from({length:24},(_,i)=>item(i+1)):p.pageError?[]:[item(25)],_meta:{tmdb_trending_day:{page,has_next:page===1||p.pageError,error:page===2&&p.pageError?'第二页失败':undefined}}};
      } else if(config.url==='/discover/library-status') data={items:[]};
      else if(config.url.startsWith('/discover/tmdb/')) data={center_id:'11111111-1111-4111-8111-111111111111'};
      else if(config.url.endsWith('/credits')) data={data:[],total:0,page:1,page_size:60};
      else if(config.url.includes('/subjects/')) data={data:{subject:{id:'11111111-1111-4111-8111-111111111111',kind:'movie',title:'来源详情',overview:'来源简介',fields:{}},components:[],missing_scopes:[],refresh_pending:false}};
      return {data,status:200,headers:{},config};
    };
    useAuthStore.getState().setSession('boundary-token-1','',{id:'same-user',username:'测试',role:'admin',tier:'free'});
    history.pushState(null,'','/discover?system=tmdb&section=rank');dispatchEvent(new PopStateEvent('popstate'));
    return true;
  })()`)
  wait(`window.boundaryProbe.releases.length===1`)
  evaluate(`window.boundaryProbe.auth.getState().setSession('boundary-token-2','',{id:'same-user',username:'测试',role:'admin',tier:'free'});true`)
  wait(`window.boundaryProbe.sections.length>=2&&document.querySelector('button[aria-label="查看作品 1"]')`)
  evaluate(`window.boundaryProbe.releases.splice(0).forEach(resolve=>resolve());true`)
  assert.equal(evaluate(`new Set(window.boundaryProbe.sections).size`), 2)
  assert.ok(evaluate(`Boolean(document.querySelector('button[aria-label="查看作品 1"] [aria-label="暂无海报"]'))`))
  wait(`document.querySelector('button[aria-label="查看作品 2"] [aria-label="暂无海报"]')`)
  assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看作品 "]').length`), 24)
  evaluate(`const main=document.querySelector('#main-content');main.scrollTo(0,main.scrollHeight);true`)
  wait(`document.body.innerText.includes('第二页失败')`)
  assert.deepEqual(evaluate(`window.boundaryProbe.pages`), [1, 2])
  assert.ok(!evaluate(`document.body.innerText.includes('榜单已加载完')`))
  evaluate(`window.boundaryProbe.pageError=false;true`)
  click('重试')
  wait(`document.querySelector('button[aria-label="查看作品 25"]')`)
  assert.deepEqual(evaluate(`window.boundaryProbe.pages`), [1, 2, 2])
  wait(`document.body.innerText.includes('榜单已加载完')`)
  evaluate(`window.boundaryProbe.sectionsError=true;window.boundaryProbe.auth.getState().setSession('boundary-token-3','',{id:'same-user',username:'测试',role:'admin',tier:'free'});true`)
  wait(`document.body.innerText.includes('榜单列表读取失败')`)
  assert.ok(!evaluate(`document.body.innerText.includes('至少选择一个推荐源')`))
  evaluate(`window.boundaryProbe.sectionsError=false;true`)
  click('重新加载')
  wait(`document.querySelector('button[aria-label="查看作品 1"]')`)
  click('查看作品 1')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源简介')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').contains(document.activeElement)`))
  for (let index = 0; index < 8; index++) browser('press', 'Tab')
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').contains(document.activeElement)`))
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  assert.equal(evaluate(`document.activeElement.getAttribute('aria-label')`), '查看作品 1')
  evaluate(`(async()=>{
    const dependency=name=>performance.getEntriesByType('resource').find(entry=>new URL(entry.name).pathname.endsWith('/'+name+'.js')).name;
    const react=await import(dependency('react'));const dom=await import(dependency('react-dom_client'));const React=react.default??react;const {createRoot}=dom.default??dom;const {ModalShell}=await import('/src/components/ModalShell.tsx');
    const host=document.createElement('div');host.style.cssText='position:fixed;bottom:0;left:0;z-index:45';document.body.appendChild(host);window.modalTest={host,root:createRoot(host)};
    function Test(){const [open,setOpen]=React.useState(false);const [child,setChild]=React.useState(false);return React.createElement(React.Fragment,null,
      React.createElement('button',{onClick:()=>setOpen(true)},'测试打开父层'),open&&React.createElement(ModalShell,{onClose:()=>setOpen(false),ariaLabel:'父层'},
        React.createElement('button',{onClick:()=>setChild(true)},'测试打开子层'),React.createElement('button',{onClick:()=>setOpen(false)},'测试关闭父层'),
        child&&React.createElement(ModalShell,{onClose:()=>setChild(false),ariaLabel:'子层',zIndex:70},React.createElement('button',{onClick:()=>setChild(false)},'测试关闭子层'),React.createElement('button',null,'子层末尾'))));}
    window.modalTest.root.render(React.createElement(Test));return true;
  })()`)
  wait(`document.body.innerText.includes('测试打开父层')`)
  click('测试打开父层'); wait(`document.querySelector('[aria-label="父层"]')`)
  click('测试打开子层'); wait(`document.querySelector('[aria-label="子层"]')`)
  assert.ok(evaluate(`document.querySelector('[aria-label="子层"]').contains(document.activeElement)`))
  browser('press', 'Shift+Tab')
  assert.equal(evaluate(`document.activeElement.textContent`), '子层末尾')
  browser('press', 'Tab')
  assert.equal(evaluate(`document.activeElement.textContent`), '测试关闭子层')
  browser('press', 'Escape'); wait(`!document.querySelector('[aria-label="子层"]')`)
  assert.ok(evaluate(`Boolean(document.querySelector('[aria-label="父层"]'))&&document.body.style.overflow==='hidden'`))
  assert.equal(evaluate(`document.activeElement.textContent`), '测试打开子层')
  browser('press', 'Escape'); wait(`!document.querySelector('[aria-label="父层"]')`)
  assert.equal(evaluate(`document.activeElement.textContent`), '测试打开父层')
  assert.equal(evaluate(`document.body.style.overflow`), '')
  console.log('发现边界：同账号重登录、缺图占位、失败页原页重试、榜单读取失败、嵌套焦点与关闭恢复检查通过')
} catch (error) {
  console.error(browser('errors'))
  throw error
} finally {
  evaluate(`if(window.modalTest){window.modalTest.root.unmount();window.modalTest.host.remove()}true`)
  browser('close')
}
