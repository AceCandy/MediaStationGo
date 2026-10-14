// 隔离 API，验证榜单刷新、失败回退、切换取消及分类搜索。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'
const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `ranking-refresh-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const wait = code => browser('wait', '--fn', code)
const click = name => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const navigate = path => evaluate(`history.pushState(null,'',${JSON.stringify(path)});dispatchEvent(new PopStateEvent('popstate'));true`)
try {
 browser('open', base + '/login')
 evaluate(`(async()=>{
  const {api}=await import('/src/api/client.ts');const {useAuthStore}=await import('/src/stores/auth.ts');
  const probe=window.rankProbe={calls:[],fail:false,hold:false,releases:[],version:0};
  const stamp=()=>probe.version?'2026-10-14T08:30:00Z':'2026-10-13T08:30:00Z';
  api.defaults.adapter=async config=>{
   const path=config.url;const call={path,...config.params,aborted:false};probe.calls.push(call);
   config.signal?.addEventListener('abort',()=>{call.aborted=true},{once:true});
   const isRefresh=path.endsWith('/refresh')||config.params?.refresh===1;
   if(isRefresh&&probe.hold)await new Promise(resolve=>probe.releases.push(resolve));
   let data={};
   if(path==='/auth/permissions')data={permissions:{can_view_discover:true},role:'admin',is_super:true};
   else if(path==='/play-profiles')data=[];
   else if(path==='/discover/sections')data={sections:['tmdb','douban'].flatMap(source=>[{key:source+'_popular_movie',label:source+'测试榜',provider:source},{key:source+'_hot_tv',label:source+'另一榜',provider:source}])};
   else if(path==='/discover/feed'){
    const key=config.params.sections;
    if(isRefresh&&!probe.fail)probe.version++;
    data={[key]:[{source:key.startsWith('tmdb')?'tmdb':'douban',title:'测试榜单作品',media_type:'movie',tmdb_id:10,douban_id:'100',overview:'作品简介'}],_meta:{[key]:{page:1,has_next:false,updated_at:stamp(),...(isRefresh&&probe.fail?{stale:true,warning:'榜单刷新失败，显示上次结果'}:{})}}};
   }else if(path.endsWith('/refresh')){
    if(probe.fail)throw new Error('模拟刷新失败');probe.version++;data={updated_at:stamp()};
   }else if(path.endsWith('/works')){
    if(probe.failRead)throw new Error('模拟列表读取失败');
    data={items:[{id:'fixture',source_id:'100',title:'测试榜单作品',kind:'series',source_category:'ai-duanju',hydrated:false,artwork_id:'',overview:'摘要',tags:[],rating:0}],total:1,updated_at:stamp()};
   }else if(path.endsWith('/status'))data={enabled:true};
   else if(path.endsWith('/subjects')||path.endsWith('/categories'))data={data:[],total:0};
   else if(path==='/discover/library-status')data={items:[]};
   return {data,status:200,headers:{},config};
  };
  useAuthStore.getState().setSession('synthetic-ranking-token','',{id:'ranking-admin',username:'测试',role:'admin',tier:'free'});
  return true;
 })()`)
 for(const source of ['tmdb','douban','hongguo','huangguoai']) {
  const rank=source==='hongguo'?'hot-drama':source==='huangguoai'?'hot':source+'_popular_movie'
  evaluate(`window.rankProbe.version=0;window.rankProbe.fail=false`)
  navigate(`/discover?system=${source}&section=rank&rank=${rank}&keyword=legacy&tmdb_query=legacy`)
  wait(`document.querySelector('.ranking-header time')?.dateTime==='2026-10-13T08:30:00Z'&&document.querySelectorAll('.ranking-row').length===1`)
  assert.equal(evaluate(`document.querySelector('main form[role="search"]')`),null)
  assert.equal(evaluate(`document.querySelector('.ranking-header input')`),null)
  const before=evaluate(`window.rankProbe.calls.length`)
  evaluate(`window.rankProbe.fail=true`);click('刷新当前榜单')
  wait(`document.querySelector('[role="alert"]')!==null`)
  assert.equal(evaluate(`document.querySelector('.ranking-header time').dateTime`),'2026-10-13T08:30:00Z')
  assert.equal(evaluate(`document.querySelectorAll('.ranking-row').length`),1)
  const requests=evaluate(`window.rankProbe.calls.slice(${before})`)
  assert.ok(requests.some(c=>c.path.endsWith('/refresh')||c.refresh===1))
  evaluate(`window.rankProbe.fail=false;window.rankProbe.hold=true`);click('刷新当前榜单')
  wait(`window.rankProbe.releases.length===1`)
  assert.equal(evaluate(`document.querySelector('[aria-label="刷新当前榜单"]').disabled`),true)
  evaluate(`document.querySelector('[aria-label="刷新当前榜单"]').click()`)
  assert.equal(evaluate(`window.rankProbe.releases.length`),1)
  evaluate(`window.rankProbe.hold=false;window.rankProbe.releases.splice(0).forEach(fn=>fn())`)
  wait(`document.querySelector('.ranking-header time')?.dateTime==='2026-10-14T08:30:00Z'&&!document.querySelector('[aria-label="刷新当前榜单"]').disabled`)
  if(source==='hongguo'||source==='huangguoai') {
   evaluate(`window.rankProbe.failRead=true`);click('刷新当前榜单')
   wait(`/资料.*失败/.test(document.querySelector('[role="alert"]')?.textContent||'')`)
   assert.equal(evaluate(`document.querySelectorAll('.ranking-row').length`),1)
   assert.equal(evaluate(`document.querySelector('.ranking-header time').dateTime`),'2026-10-14T08:30:00Z')
   evaluate(`window.rankProbe.failRead=false`);click('刷新当前榜单')
   wait(`!document.querySelector('[role="alert"]')&&!document.querySelector('[aria-label="刷新当前榜单"]').disabled`)
  }
  // 刷新途中切换，旧请求必须取消且不能重置新榜。
  evaluate(`window.rankProbe.hold=true`);click('刷新当前榜单');wait(`window.rankProbe.releases.length===1`)
  const other=source==='hongguo'?'AI剧热播榜':source==='huangguoai'?'推荐榜':source+'另一榜'
  click(other);wait(`window.rankProbe.calls.some(c=>c.aborted)`)
  evaluate(`window.rankProbe.hold=false;window.rankProbe.releases.splice(0).forEach(fn=>fn())`)
  wait(`document.querySelector('.ranking-header h2')?.textContent===${JSON.stringify(other)}`)
  for(const theme of ['light','dark'])for(const width of [390,768,1440]){
   browser('set','viewport',String(width),'900');evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
   assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth`),source+'/'+theme+'/'+width)
   if(process.env.RANKING_SCREENSHOT_DIR&&source==='hongguo'&&[390,1440].includes(width))browser('screenshot',`${process.env.RANKING_SCREENSHOT_DIR}/${source}-${theme}-${width}.png`)
  }
  click('分类');wait(`document.querySelector('main input')!==null`)
 }
 console.log('榜单刷新检查通过：四来源搜索隔离、时间、失败保留、成功更新、防重复、切换取消、分类搜索、暗亮响应布局。')
} finally { browser('close') }
