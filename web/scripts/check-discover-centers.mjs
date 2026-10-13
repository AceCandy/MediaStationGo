// 隔离 Vite 服务下的发现来源与资料浏览检查，接口全部模拟。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:6238'
const session = `discover-centers-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const wait = code => browser('wait', '--fn', code)
const click = name => { browser('wait', '250'); return browser('find', 'role', 'button', 'click', '--name', name, '--exact') }
const navigate = path => evaluate(`history.pushState(null,'',${JSON.stringify(path)});dispatchEvent(new PopStateEvent('popstate'));true`)

try {
  browser('open', base + '/login')
  browser('set', 'media', 'dark', 'reduced-motion')
  evaluate(`(async () => {
    const {api}=await import('/src/api/client.ts');
    const {useAuthStore}=await import('/src/stores/auth.ts');
    const probe=window.centerProbe={calls:[],hold:'',releases:[],role:'user'};
    const image='/api/../brand/mediastationgo-logo.svg';
    const ids=['11111111-1111-4111-8111-111111111111','22222222-2222-4222-8222-222222222222','33333333-3333-4333-8333-333333333333','44444444-4444-4444-8444-444444444444','55555555-5555-4555-8555-555555555555'];
    const subject=(id,kind,title,number)=>({id,kind,title,tmdb_id:ids.indexOf(id)+10,number,original_name:'Source title',overview:'已保存的资料简介',release_date:'2026-01-01',rating:8.2,vote_count:50,status:'Released',quality:3,fields:{runtime:110,genres:[{id:1,name:'剧情'}]},poster_url:image,backdrop_url:'',fetched_at:'2026-10-12T00:00:00Z'});
    const movie=subject(ids[0],'movie','资料电影'),series=subject(ids[1],'series','资料电视剧'),season=subject(ids[2],'season','第一季',1),episode=subject(ids[3],'episode','第一集',1);
    const person={id:ids[4],person_id:ids[4],tmdb_id:18,name:'资料演员',biography:'人物简介',department:'Acting',quality:3,fields:{birthday:'2000-01-01'},profile_url:image};
    const douban={id:ids[0],douban_id:'100',entity_kind:'movie',title:'豆瓣作品',year:'2026',rating:8.1,quality:'complete',has_payload:true,poster_url:image,fetched_at:null,last_attempt_at:null,next_retry_at:null};
    const page=(data,total=data.length)=>({data,total,page:1,page_size:30});
    const detail=(v,isPerson=false)=>({data:{subject:isPerson?undefined:v,person:isPerson?v:undefined,source_url:'https://www.themoviedb.org/movie/10',parents:[],components:[{component:'detail',language:'zh-CN',fetched_at:'2026-10-12T00:00:00Z',raw_json:{admin:'raw'}}],jobs:[],refresh_pending:false,missing_scopes:isPerson?['filmography']:[]}});
    api.defaults.adapter=async config=>{
      const path=config.url.replace('/admin/tmdb','/catalogs/tmdb').replace('/admin/douban','/catalogs/douban');
      if(path==='/auth/permissions') return {data:{permissions:{can_view_discover:true},role:probe.role,is_super:probe.role==='admin'},status:200,headers:{},config};
      if(path==='/play-profiles') return {data:[],status:200,headers:{},config};
      const call={url:config.url,path,method:config.method,...config.params,aborted:false};probe.calls.push(call);
      config.signal?.addEventListener('abort',()=>{call.aborted=true},{once:true});
      if(probe.hold===path) await new Promise(resolve=>probe.releases.push(resolve));
      let data={};
      if(path.endsWith('/categories')) data={data:path.includes('/douban/')?[{value:'剧情',label:'剧情'}]:config.params?.kind==='person'?[{value:'Acting',label:'Acting'}]:[{value:'1',label:'剧情'},{value:'2',label:'喜剧'}]};
      else if(path==='/catalogs/tmdb/subjects') data=page([config.params?.kind==='tv'?series:movie],31);
      else if(path==='/catalogs/tmdb/people') data=page([person]);
      else if(path.endsWith('/seasons')) data=page([season]);
      else if(path.endsWith('/episodes')) data=page([episode]);
      else if(path.endsWith('/credits')) data=page([{id:'credit',category:'cast',character:'主角',person,subject:movie}]);
      else if(path.endsWith('/works')) data=page([{id:'work',category:'cast',character:'主角',person,subject:movie}]);
      else if(path.startsWith('/catalogs/tmdb/people/')) data=detail(person,true);
      else if(path.startsWith('/catalogs/tmdb/subjects/')) data=detail([movie,series,season,episode].find(v=>path.endsWith(v.id))||movie);
      else if(path.endsWith('/refresh')&&path.includes('/douban/')) data={data:{subject:douban}};
      else if(path==='/catalogs/douban/subjects/ensure') data={data:JSON.parse(config.data).douban_id==='999'?{...douban,id:ids[1],douban_id:'999',title:'尚未保存作品',has_payload:false,quality:'partial',refresh_pending:true}:douban};
      else if(path==='/catalogs/douban/subjects') data=page(config.params?.keyword==='999' ? [] : config.params?.keyword==='100' ? [{...douban,id:ids[1],douban_id:'101'},douban] : [douban]);
      else if(path.startsWith('/catalogs/douban/subjects/')) {
        if(path==='/catalogs/douban/subjects/'+ids[1]) data={data:{...douban,id:ids[1],douban_id:'999',title:'补全后作品',overview:'已自动补全',quality:'complete',refresh_pending:false}};
        else
        data={data:{...douban,overview:'豆瓣完整简介',genres:['剧情'],credits:[],directors:['豆瓣导演'],raw_json:{admin:'raw'}}};
      }
      else if(path==='/discover/sections') data={sections:[{key:'tmdb_trending_day',label:'TMDb 今日趋势',provider:'tmdb'},{key:'tmdb_latest_movie',label:'TMDb 最新电影',provider:'tmdb'},{key:'douban_hot_movie',label:'豆瓣热门电影',provider:'douban'},{key:'bangumi_calendar',label:'Bangumi 每日放送',provider:'bangumi'}]};
      else if(path==='/discover/feed') {const key=config.params.sections;data={[key]:key==='douban_hot_movie'?[{source:'douban',douban_id:'100',media_type:'movie',title:'豆瓣榜单作品',poster_url:image},{source:'douban',douban_id:'999',media_type:'movie',title:'尚未保存作品',overview:'榜单摘要',poster_url:image}]:[],_meta:{[key]:{page:1,has_next:false}}};}
      else if(path==='/discover/library-status') data={items:[]};
      probe.auth=useAuthStore;
      return {data,status:200,headers:{},config};
    };
    probe.auth=useAuthStore;
    useAuthStore.getState().setSession('test-viewer-token','',{id:'viewer',username:'测试观众',role:'user',tier:'free'});
    return true;
  })()`)
  navigate('/discover')
  wait(`document.querySelector('button[aria-label="查看资料电影"]') !== null`)
  assert.deepEqual(evaluate(`[...document.querySelector('[aria-label="发现资料体系"]').querySelectorAll('button')].map(b=>b.textContent)`), ['TMDB', '豆瓣', '红果短剧', '黄果 AI', 'Bangumi'])
  assert.ok(!evaluate(`document.querySelector('a[href="/admin/data-center"]') || document.body.innerText.includes('搜索 / 添加资料')`))
  assert.equal(evaluate(`window.centerProbe.calls.filter(c=>c.path==='/catalogs/tmdb/subjects').length`), 1)
  assert.ok(!evaluate(`window.centerProbe.calls.some(c=>c.path==='/discover/feed'||c.path.includes('douban'))`))
  click('剧情'); wait(`window.centerProbe.calls.some(c=>c.path==='/catalogs/tmdb/subjects'&&c.genre==='1'&&c.page===1)`)
  click('喜剧'); wait(`window.centerProbe.calls.some(c=>c.path==='/catalogs/tmdb/subjects'&&c.genre==='2')`)
  click('全部'); wait(`new URLSearchParams(location.search).get('genre')===null`)
  click('查看资料电影')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('已保存的资料简介')`)
  assert.ok(!evaluate(`document.querySelector('[role="dialog"]').innerText.match(/刷新来源资料|原始|来源记录|重试图片/)`))
  assert.ok(!evaluate(`window.centerProbe.calls.some(c=>c.path.endsWith('/ensure')||c.path.endsWith('/refresh'))`))
  browser('press', 'Escape')
  wait(`!document.querySelector('[role="dialog"]')`)
  assert.equal(evaluate(`document.activeElement.getAttribute('aria-label')`), '查看资料电影')
  click('下一页')
  wait(`window.centerProbe.calls.some(c=>c.path==='/catalogs/tmdb/subjects'&&c.page===2)`)
  click('电视剧')
  wait(`document.querySelector('button[aria-label="查看资料电视剧"]') !== null`)
  assert.ok(!evaluate(`new URLSearchParams(location.search).has('page')`))
  click('查看资料电视剧')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('第一季')`)
  browser('find','role','button','click','--name','查看第一季')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('第一集')`)
  browser('find','role','button','click','--name','第 1 集 · 第一集')
  wait(`document.querySelector('[role="dialog"]')?.querySelector('h2')?.textContent==='第一集'`)
  assert.ok(evaluate(`window.centerProbe.calls.some(c=>c.path.endsWith('/seasons'))&&window.centerProbe.calls.some(c=>c.path.endsWith('/episodes'))`))
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('人物'); wait(`document.querySelector('button[aria-label="查看资料演员"]') !== null`)
  click('表演'); wait(`window.centerProbe.calls.some(c=>c.path==='/catalogs/tmdb/people'&&c.department==='Acting'&&c.page===1)`);
  click('查看资料演员'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('人物简介')`)
  assert.ok(!evaluate(`window.centerProbe.calls.some(c=>c.path.endsWith('/works'))`))
  click('查看作品履历'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('参演与幕后作品')`)
  assert.ok(!evaluate(`window.centerProbe.calls.some(c=>c.path.endsWith('/ensure')||c.path.endsWith('/refresh'))`))
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('豆瓣'); wait(`document.querySelector('button[aria-label="查看豆瓣作品"]') !== null`)
  assert.equal(evaluate(`location.search`), '?system=douban&type=movie')
  click('剧情'); wait(`window.centerProbe.calls.some(c=>c.path==='/catalogs/douban/subjects'&&c.genre==='剧情'&&c.type==='movie')`);
  click('查看豆瓣作品'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('豆瓣完整简介')`)
  assert.ok(!evaluate(`document.querySelector('[role="dialog"]').innerText.match(/更新豆瓣|原始豆瓣|资料状态|下次可重试/)`))
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('榜单'); wait(`window.centerProbe.calls.some(c=>c.sections==='douban_hot_movie')`)
  assert.equal(evaluate(`window.centerProbe.calls.filter(c=>c.path==='/discover/feed').length`), 1)
  assert.equal(evaluate(`document.querySelector('[aria-label="搜索 TMDb 作品"]')`), null)
  const rankingDetailStart = evaluate(`window.centerProbe.calls.length`)
  click('查看豆瓣榜单作品')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('豆瓣完整简介')`)
  const rankingDetailCalls = evaluate(`window.centerProbe.calls.slice(${rankingDetailStart})`)
  assert.ok(rankingDetailCalls.some(c=>c.path==='/catalogs/douban/subjects/ensure'&&c.method==='post'))
  assert.deepEqual(rankingDetailCalls.filter(c=>c.path.startsWith('/catalogs/douban/subjects/')&&c.method==='get').map(c=>c.path), ['/catalogs/douban/subjects/11111111-1111-4111-8111-111111111111'])
  assert.ok(!evaluate(`document.querySelector('[role="dialog"]').innerText.match(/更新豆瓣|原始豆瓣|资料状态/)`))
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('查看尚未保存作品')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('已自动补全')`)
  assert.ok(evaluate(`window.centerProbe.calls.filter(c=>c.path==='/catalogs/douban/subjects/ensure').length===2`))
  assert.ok(!evaluate(`window.centerProbe.calls.some(c=>c.path.endsWith('/refresh'))`))
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('TMDB'); wait(`document.querySelector('button[aria-label="查看资料电影"]') !== null`)
  click('榜单'); wait(`window.centerProbe.calls.some(c=>c.sections==='tmdb_trending_day')`)
  assert.deepEqual(evaluate(`[...document.querySelector('[aria-label="tmdb榜单切换"]').querySelectorAll('button')].map(o=>o.textContent)`), ['TMDb 今日趋势', 'TMDb 最新电影'])
  click('TMDb 最新电影')
  wait(`new URLSearchParams(location.search).get('rank')==='tmdb_latest_movie'&&window.centerProbe.calls.some(c=>c.sections==='tmdb_latest_movie')`)
  evaluate(`history.back();true`); wait(`new URLSearchParams(location.search).get('rank')==='tmdb_trending_day'`)
  click('Bangumi'); wait(`window.centerProbe.calls.some(c=>c.sections==='bangumi_calendar')`)
  assert.equal(evaluate(`document.querySelector('[aria-label="来源内容"]').textContent`), '榜单')
  assert.ok(evaluate(`window.centerProbe.calls.filter(c=>c.path==='/discover/feed').every(c=>!c.sections.includes(','))`))
  navigate('/discover?system=douban&subject=11111111-1111-4111-8111-111111111111')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('豆瓣完整简介')`)
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  evaluate(`window.centerProbe.hold='/catalogs/douban/subjects';true`)
  navigate('/discover?system=douban&keyword=old')
  wait(`window.centerProbe.releases.length===1`)
  click('TMDB'); wait(`document.querySelector('button[aria-label="查看资料电影"]') !== null`)
  assert.ok(evaluate(`window.centerProbe.calls.find(c=>c.keyword==='old').aborted`))
  evaluate(`window.centerProbe.hold='';window.centerProbe.releases.splice(0).forEach(resolve=>resolve());true`)
  navigate('/discover?system=tmdb&system=douban&section=wrong&section=rank&kind=oops&kind=person&page=-1&page=2&subject=------------------------------------&person=invalid&provider=douban')
  wait(`document.querySelector('button[aria-label="查看资料电影"]')!==null&&location.search==='?system=tmdb&kind=movie'`)
  assert.ok(!evaluate(`document.querySelector('[role="dialog"]')`))
  navigate('/discover?system=douban&section=rank&rank=tmdb_latest_movie&rank=douban_hot_movie')
  wait(`location.search==='?system=douban&section=rank&rank=douban_hot_movie'`)
  navigate('/discover?system=tmdb&subject=11111111-1111-4111-8111-111111111111&subject=22222222-2222-4222-8222-222222222222')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('已保存的资料简介')&&new URLSearchParams(location.search).getAll('subject').length===1`)
  browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 639, 640, 768, 1023, 1024, 1440]) {
      browser('set', 'viewport', String(width), '900')
      browser('wait', '250')
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth`), `${theme}/${width}`)
      if (process.env.DISCOVER_SCREENSHOT_DIR && [390, 1440].includes(width)) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/centers-${theme}-${width}.png`)
    }
  }
  evaluate(`window.centerProbe.role='admin';window.centerProbe.auth.getState().setSession('test-admin-token','',{id:'admin',username:'管理员',role:'admin',tier:'free'});true`)
  wait(`document.body.innerText.includes('搜索 / 添加资料')&&window.centerProbe.calls.some(c=>c.url==='/admin/tmdb/subjects')`)
  click('查看资料电影'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源记录与刷新状态')`)
  browser('click', '[role="dialog"] header summary')
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('刷新来源资料')`))
  browser('press','Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('豆瓣'); wait(`document.querySelector('button[aria-label="查看豆瓣作品"]')!==null`)
  click('查看豆瓣作品'); wait(`document.querySelector('[role="dialog"] header summary')!==null`)
  browser('click','[role="dialog"] header summary')
  const categoryActions=evaluate(`[...document.querySelectorAll('[role="dialog"] header button')].map(b=>b.getAttribute('aria-label'))`)
  browser('press','Escape'); wait(`!document.querySelector('[role="dialog"]')`)
  click('榜单'); wait(`document.querySelector('button[aria-label="查看豆瓣榜单作品"]')!==null`)
  click('查看豆瓣榜单作品'); wait(`document.querySelector('[role="dialog"] header summary')!==null`)
  browser('click','[role="dialog"] header summary')
  assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="dialog"] header button')].map(b=>b.getAttribute('aria-label'))`),categoryActions)
  for (const theme of ['dark', 'light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390, 1440]) {
      browser('set', 'viewport', String(width), '900'); browser('wait', '250')
      assert.ok(evaluate(`document.querySelector('[role="dialog"]').scrollWidth<=document.querySelector('[role="dialog"]').clientWidth`))
      assert.ok(evaluate(`document.querySelector('[role="dialog"] header a').getBoundingClientRect().height<60`))
      if (process.env.DISCOVER_SCREENSHOT_DIR) browser('screenshot', `${process.env.DISCOVER_SCREENSHOT_DIR}/douban-menu-${theme}-${width}.png`)
    }
  }
  const beforeRefresh=evaluate(`window.centerProbe.calls.filter(c=>c.path.endsWith('/refresh')).length`)
  click('基础更新豆瓣资料'); wait(`window.centerProbe.calls.filter(c=>c.path.endsWith('/refresh')).length===${beforeRefresh+1}`)
  assert.ok(evaluate(`window.centerProbe.calls.filter(c=>c.path.endsWith('/refresh')).at(-1).url.startsWith('/admin/douban/')`))
  assert.equal(browser('errors').trim(), '')
  console.log('通过：五个来源入口、资料与榜单隔离、普通用户只读、季集与人物履历、URL/历史/取消、管理员控件和双主题响应式')
} catch (error) {
  console.error(evaluate(`({search:location.search,calls:window.centerProbe.calls.slice(-8),overflow:[...document.querySelectorAll('main *')].filter(e=>e.getBoundingClientRect().right>innerWidth).map(e=>({tag:e.tagName,cls:e.className,right:e.getBoundingClientRect().right})).slice(0,10)})`))
  console.error(browser('snapshot', '-i'))
  console.error(browser('errors'))
  throw error
} finally {
  browser('close')
}
