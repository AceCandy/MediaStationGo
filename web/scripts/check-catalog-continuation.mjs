// 本地模拟目录分页，验证追加、失败重试、筛选取消与紧凑导航。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:6243'
const session = `catalog-continuation-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const wait = code => browser('wait', '--fn', code)
const navigate = path => evaluate(`history.pushState(null,'',${JSON.stringify(path)});dispatchEvent(new PopStateEvent('popstate'));true`)
const click = name => { browser('wait', '250'); browser('find', 'role', 'button', 'click', '--name', name, '--exact') }
const cards = `document.querySelectorAll('main button[aria-label^="查看作品"]')`
const scrollEnd = () => evaluate(`${cards}[${cards}.length-1].scrollIntoView({block:'start'});true`)
try {
  browser('open', base + '/login')
  browser('set', 'viewport', '1440', '900')
  browser('set', 'media', 'dark', 'reduced-motion')
  evaluate(`(async()=>{
    const {api}=await import('/src/api/client.ts'); const {useAuthStore}=await import('/src/stores/auth.ts');
    const p=window.catalogProbe={calls:[],fail:true,hold:false,releases:[]};
    api.defaults.adapter=async config=>{
      const path=config.url;const q=config.params||{};let data={};
      if(path==='/auth/permissions') data={permissions:{can_view_discover:true},role:'admin',is_super:true};
      else if(path==='/play-profiles') data=[];
      else if(path.endsWith('/categories')) data={data:[{value:path.includes('douban')?'恐怖':'27',label:'恐怖'}]};
      else if(path.endsWith('/subjects')||path.endsWith('/people')) {
        const call={path,...q,aborted:false};p.calls.push(call);config.signal?.addEventListener('abort',()=>{call.aborted=true},{once:true});
        if(p.hold&&q.page===2) await new Promise(resolve=>p.releases.push(resolve));
        if(p.fail&&q.page===2){p.fail=false;throw new Error('test failure')}
        const page=Number(q.page); const all=Array.from({length:65},(_,i)=>{
          const id='11111111-1111-4111-8111-'+String(i+1).padStart(12,'0');const title='作品'+(q.genre?'恐怖':'')+(i+1);
          return {id,...(path.endsWith('/people')?{person_id:id}:{}),tmdb_id:i+1,douban_id:String(i+1),entity_kind:'movie',kind:'movie',title,name:title,year:'2026',release_date:'2026-01-01',quality:3,rating:8,has_payload:true,fields:{},poster_url:'',profile_url:''};
        });
        const rows=all.slice((page-1)*30,page*30);if(page===2) rows[0]=all[29];
        data={data:rows,total:65,page,page_size:30};
      } else if(path==='/discover/library-status'){p.calls.push({path,size:q?.items?.length});data={items:[]}}
      else if(path.endsWith('/credits')) data={data:[],total:0,page:1,page_size:20};
      else if(path.includes('/douban/subjects/')) data={data:{id:path.split('/').pop(),douban_id:'1',title:'详情',entity_kind:'movie',has_payload:true,credits:[]}};
      else if(path.includes('/subjects/')) data={data:{subject:{id:path.split('/').pop(),kind:'movie',title:'详情',fields:{}},components:[],parents:[],jobs:[],missing_scopes:[],credits:[]}};
      return {data,status:200,headers:{},config};
    };
    useAuthStore.getState().setSession('local-fixture','',{id:'admin',username:'本地测试',role:'admin',tier:'free'});return true;
  })()`)
  for (const source of ['tmdb', 'douban']) {
    evaluate('window.catalogProbe.calls=[];window.catalogProbe.fail=true;window.scrollTo(0,0);true')
    navigate(`/discover?system=${source}&page=9`)
    wait(`${cards}.length===30 && window.catalogProbe.calls.some(c=>c.path==='/admin/${source}/subjects'&&c.page===1) && !new URLSearchParams(location.search).has('page')`)
    assert.equal(evaluate('new URLSearchParams(location.search).has("page")'), false)
    assert.equal(evaluate(`document.querySelector('[aria-label="来源内容"]').getBoundingClientRect().width < 300`), true)
    assert.equal(evaluate(`document.querySelector('main').innerText.includes('下一页')`), false)
    evaluate(`window.savedCatalogNode=${cards}[0];window.savedCatalogCalls=window.catalogProbe.calls.filter(c=>c.page).length;true`)
    click('查看作品1');wait(`document.querySelector('[role="dialog"]')!==null`)
    scrollEnd();browser('wait','250')
    assert.equal(evaluate('window.catalogProbe.calls.filter(c=>c.page).length'),evaluate('window.savedCatalogCalls'))
    browser('press','Escape');wait(`document.querySelector('[role="dialog"]')===null`)
    assert.ok(evaluate(`${cards}[0]===window.savedCatalogNode`))
    scrollEnd()
    wait(`document.querySelector('[role="alert"]')!==null`)
    assert.equal(evaluate(`${cards}.length`), 30)
    browser('wait', '300')
    assert.equal(evaluate('window.catalogProbe.calls.filter(c=>c.page===2).length'), 1)
    click(source === 'tmdb' ? '重试' : '重试读取')
    wait(`${cards}.length===59`)
    assert.deepEqual(evaluate('window.catalogProbe.calls.filter(c=>c.page).map(c=>c.page)'), [1, 2, 2])
    scrollEnd();wait(`${cards}.length===64`)
    wait(`document.querySelector('main').innerText.includes('资料已加载完')`)
    click('恐怖'); wait(`${cards}.length===30 && ${cards}[0].textContent.includes('恐怖')`)
    evaluate('window.scrollTo(0,0);true')
    click('重新读取列表');wait(`${cards}.length===30`)
    evaluate('window.catalogProbe.hold=true;true');scrollEnd();wait('window.catalogProbe.releases.length>0')
    click('全部');wait(`${cards}.length===30 && !${cards}[0].textContent.includes('恐怖')`)
    evaluate('window.catalogProbe.hold=false;window.catalogProbe.releases.splice(0).forEach(r=>r());window.scrollTo(0,0);true')
    assert.ok(evaluate('window.catalogProbe.calls.some(c=>c.page===2&&c.aborted)'))
    browser('wait', '250');assert.equal(evaluate(`${cards}.length`),30)
    for (const [theme,width] of [['light',390],['dark',1440]]) {
      browser('set','viewport',String(width),'900')
      evaluate(`document.documentElement.dataset.theme='${theme}';window.scrollTo(0,0);true`)
      browser('wait','250')
      assert.ok(evaluate('document.documentElement.scrollWidth<=innerWidth'))
      assert.ok(evaluate(`document.querySelector('[aria-label="来源内容"]').getBoundingClientRect().width<300`))
      if(process.env.CATALOG_SCREENSHOT_DIR) browser('screenshot',`${process.env.CATALOG_SCREENSHOT_DIR}/${source}-${theme}.png`)
    }
  }
  navigate('/discover?system=tmdb&kind=person')
  wait(`${cards}.length===30`)
  scrollEnd();wait(`${cards}.length===59`)
  assert.ok(evaluate(`window.catalogProbe.calls.some(c=>c.path.endsWith('/people')&&c.page===2)`))
  console.log('Catalog continuation checks passed')
} catch (error) {
  console.error(JSON.stringify(evaluate(`({calls:window.catalogProbe.calls, text:document.querySelector('main')?.innerText.slice(-600),cards:document.querySelectorAll('main button[aria-label^="查看作品"]').length})`)), null, 2)
  throw error
} finally { browser('close') }
