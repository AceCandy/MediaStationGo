// 隔离 Vite + 模拟 API：分类与榜单共用 TMDB 详情，保持来源事实与权限边界。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `discover-detail-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const wait = code => browser('wait', '--fn', code)
const click = name => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const close = () => { browser('press', 'Escape'); wait(`!document.querySelector('[role="dialog"]')`) }
try {
  browser('open', base + '/login')
  browser('set', 'media', 'dark', 'reduced-motion')
  evaluate(`(async()=>{
    const {api}=await import('/src/api/client.ts'); const {useAuthStore}=await import('/src/stores/auth.ts');
    const p=window.detailProbe={calls:[],fail:false,pending:false,role:'user',auth:useAuthStore};
    const image='/api/../brand/mediastationgo-logo.svg';
    const id='11111111-1111-4111-8111-111111111111', personID='22222222-2222-4222-8222-222222222222';
    const subject={id,kind:'series',tmdb_id:123,title:'来源整剧',original_name:'Source title',overview:'来源完整简介。'.repeat(20),poster_url:image,backdrop_url:image,release_date:'2026-09-01',rating:8.2,vote_count:1200,status:'Returning Series',quality:3,fields:{episode_run_time:[45,50],genres:[{id:18,name:'剧情'}],production_countries:[{name:'中国'}],spoken_languages:[{name:'中文'}],external_ids:{imdb_id:'tt123456'},number_of_seasons:1}};
    const person={id:'profile-is-not-person-id',person_id:personID,tmdb_id:456,name:'来源演员',original_name:'Actor',biography:'来源人物简介',department:'Acting',profile_url:image,fields:{birthday:'2000-01-01'},quality:3};
    const items=[{source:'tmdb',tmdb_id:123,media_type:'tv',title:'榜单整剧',overview:'榜单摘要',poster_url:image},{source:'tmdb',tmdb_id:123,media_type:'movie',title:'榜单电影',poster_url:image}];
    const page=data=>({data,total:data.length,page:1,page_size:30});
    api.defaults.adapter=async config=>{
      const url=config.url;p.calls.push({url,method:config.method,params:config.params});let data={};
      if(url==='/auth/permissions') data={permissions:{can_view_discover:true},role:p.role,is_super:p.role==='admin'};
      else if(url==='/play-profiles') data=[];
      else if(url==='/discover/sections') data={sections:[{key:'tmdb_trending_day',label:'TMDB 今日趋势',provider:'tmdb'}]};
      else if(url==='/discover/feed') data={tmdb_trending_day:items,_meta:{tmdb_trending_day:{page:1,has_next:false}}};
      else if(url==='/discover/library-status') data={items:p.role==='user'?[{tmdb_id:123,media_type:'tv'}]:[]};
      else if(url.startsWith('/discover/tmdb/')) {if(p.fail) throw new Error('test failure');data={center_id:id};}
      else if(url.endsWith('/ensure')) {if(p.ensureFail) throw new Error('ensure unavailable');p.missing=false;data={};}
      else if(url.endsWith('/credits')) {if(p.creditFail) throw new Error('credits unavailable'); const n=config.params.page;data={data:[{id:'credit',category:'cast',character:n===2?'第二页角色':'来源角色',person,subject}],total:61,page:n,page_size:60};}
      else if(url.endsWith('/seasons')) {if(p.childFail) throw new Error('seasons unavailable');const n=config.params.page;data={data:[{...subject,id:'33333333-3333-4333-8333-333333333333',kind:'season',title:n===2?'第二页季':'第一页季',number:n}],total:31,page:n,page_size:30};}
      else if(url.endsWith('/works')) data=page([{id:'work',category:'cast',character:'来源角色',person,subject}]);
      else if(url.includes('/people/')) data={data:{person,source_url:'https://www.themoviedb.org/person/456',components:[],missing_scopes:[],refresh_pending:false}};
      else if(url.includes('/subjects/')) {if(p.readFail) throw new Error('read unavailable');data={data:{subject,source_url:'https://www.themoviedb.org/tv/123',components:[],missing_scopes:p.pending||p.missing?['detail']:[],refresh_pending:p.pending}};}
      return {data,status:200,headers:{},config};
    };
    useAuthStore.getState().setSession('fixture-token','',{id:'viewer',username:'测试观众',role:'user',tier:'free'});
    history.pushState(null,'','/discover?system=tmdb&section=rank');dispatchEvent(new PopStateEvent('popstate'));return true;
  })()`)
  wait(`document.querySelector('button[aria-label="查看榜单整剧"]')?.innerText.includes('已入库')`)
  assert.ok(!evaluate(`document.querySelector('button[aria-label="查看榜单电影"]').innerText.includes('已入库')`))
  assert.ok(!evaluate(`window.detailProbe.calls.some(c=>c.url.startsWith('/discover/tmdb/')||c.url.includes('/subjects/'))`))
  click('查看榜单整剧')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源角色')`)
  const text = evaluate(`document.querySelector('[role="dialog"]').innerText`)
  for (const value of ['来源整剧','2026-09-01','45 / 50 分钟 / 集','剧情','中国','中文','来源演员','8.2','1,200 人评分','持续播出','剧情简介']) assert.ok(text.includes(value), value)
  assert.ok(!text.includes('资料管理') && !text.includes('原始') && !text.includes('本地路径'))
  assert.ok(evaluate(`document.querySelector('[role="dialog"] [aria-label="TMDB 评分"]')!==null`))
  assert.ok(!evaluate(`window.detailProbe.calls.some(c=>c.url.endsWith('/ensure')||c.url.endsWith('/refresh')||c.url.includes('/admin/'))`))
  assert.deepEqual(evaluate(`[...document.querySelectorAll('[role="dialog"] a')].map(a=>a.href)`), ['https://www.themoviedb.org/tv/123','https://www.imdb.com/title/tt123456/'])
  for (const theme of ['dark','light']) {
    evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    for (const width of [390,768,1024,1440]) {
      browser('set','viewport',String(width),'900'); browser('wait','200')
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth&&document.querySelector('[role="dialog"]').scrollWidth<=document.querySelector('[role="dialog"]').clientWidth`), `${theme}/${width}`)
      if(process.env.DISCOVER_SCREENSHOT_DIR) browser('screenshot',`${process.env.DISCOVER_SCREENSHOT_DIR}/tmdb-${theme}-${width}.png`)
    }
  }
  // 季集与演职员页码仅成功后提交；失败页可原页重试，旧页及控件保持可见。
  for (const [region, flag, oldText, newText] of [['季集资料','childFail','第一页季','第二页季'], ['演职关系资料','creditFail','来源角色','第二页角色']]) {
    evaluate(`window.detailProbe[${JSON.stringify(flag)}]=true;true`)
    browser('click', `[aria-label="${region}"] footer button:last-child`)
    wait(`document.querySelector('[role="dialog"]')?.innerText.includes('读取失败，已保留上次结果')`)
    assert.ok(evaluate(`document.querySelector('[aria-label="${region}"]').innerText.includes(${JSON.stringify(oldText)})&&document.querySelector('[aria-label="${region}"]').innerText.includes('第 1 / 2 页')`))
    evaluate(`window.detailProbe[${JSON.stringify(flag)}]=false;true`); click('重试关联资料')
    wait(`document.querySelector('[aria-label="${region}"]')?.innerText.includes(${JSON.stringify(newText)})&&document.querySelector('[aria-label="${region}"]').innerText.includes('第 2 / 2 页')`)
    const endpoint = region === '季集资料' ? '/seasons' : '/credits'
    assert.deepEqual(evaluate(`window.detailProbe.calls.filter(c=>c.url.endsWith('${endpoint}')).slice(-2).map(c=>c.params.page)`), [2,2])
    wait(`document.querySelector('[aria-label="${region}"] footer button:first-child')?.disabled===false`)
    browser('wait', '250')
    browser('click', `[aria-label="${region}"] footer button:first-child`)
    wait(`document.querySelector('[aria-label="${region}"]')?.innerText.includes(${JSON.stringify(oldText)})`)
  }
  click('查看来源演员'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源人物简介')`)
  assert.ok(evaluate(`window.detailProbe.calls.some(c=>c.url==='/catalogs/tmdb/people/22222222-2222-4222-8222-222222222222')`))
  click('查看作品履历'); wait(`document.querySelector('button[aria-label="查看来源整剧"]')!==null`)
  click('查看来源整剧'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源角色')`)
  assert.equal(evaluate(`document.querySelectorAll('[role="dialog"]').length`),1)
  click('返回 来源演员'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源人物简介')`)
  close()
  wait(`document.activeElement.getAttribute('aria-label')==='查看榜单整剧'`)
  evaluate(`window.detailProbe.fail=true;true`)
  click('查看榜单整剧'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('完整资料暂时不可用')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('榜单摘要')`))
  evaluate(`window.detailProbe.fail=false;true`); click('重试详情'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源角色')`); close()
  evaluate(`window.detailProbe.readFail=true;true`)
  click('查看榜单整剧'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('读取失败')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('榜单整剧')&&document.querySelector('[role="dialog"]').innerText.includes('榜单摘要')&&document.querySelector('[role="dialog"] img')!==null`))
  evaluate(`window.detailProbe.readFail=false;true`); click('重试'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源角色')`); close()
  evaluate(`window.detailProbe.creditFail=true;true`)
  click('查看榜单整剧'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('演职关系读取失败')`)
  assert.ok(evaluate(`document.querySelector('[role="dialog"]').innerText.includes('来源完整简介')`))
  evaluate(`window.detailProbe.creditFail=false;true`); click('重试关联资料'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('来源角色')`); close()
  evaluate(`window.detailProbe.missing=true;window.detailProbe.ensureFail=true;true`)
  click('查看榜单整剧'); wait(`document.querySelector('[role="dialog"] button')&&document.querySelector('[role="dialog"]')?.innerText.includes('重试读取')`)
  evaluate(`window.detailProbe.readFail=true;true`); click('重试读取')
  wait(`document.querySelector('[role="dialog"]')?.innerText.includes('读取失败')`)
  evaluate(`window.detailProbe.readFail=false;window.detailProbe.ensureFail=false;true`); click('重试读取')
  wait(`!window.detailProbe.missing&&!document.querySelector('[role="dialog"]')?.innerText.includes('重试读取')`); close()
  const ensureStart = evaluate(`window.detailProbe.calls.filter(c=>c.url.endsWith('/ensure')).length`)
  // 只加速轮询间隔，验证耗尽后不再请求，手动重读可取得后台结果。
  evaluate(`window.detailProbe.pending=true;window.savedTimeout=window.setTimeout;window.setTimeout=(fn,ms,...args)=>window.savedTimeout(fn,ms===2000?10:ms,...args);true`)
  click('查看榜单整剧'); wait(`document.querySelector('[role="dialog"]')?.innerText.includes('资料仍在后台补全')`)
  const count = evaluate(`window.detailProbe.calls.filter(c=>c.url==='/catalogs/tmdb/subjects/11111111-1111-4111-8111-111111111111').length`)
  evaluate(`new Promise(resolve=>window.savedTimeout(()=>resolve(true),100))`)
  assert.equal(evaluate(`window.detailProbe.calls.filter(c=>c.url==='/catalogs/tmdb/subjects/11111111-1111-4111-8111-111111111111').length`),count)
  assert.equal(evaluate(`window.detailProbe.calls.filter(c=>c.url.endsWith('/ensure')).length`),ensureStart+1)
  evaluate(`window.detailProbe.pending=false;true`); click('重新读取'); wait(`!document.querySelector('[role="dialog"]')?.innerText.includes('资料仍在后台补全')`)
  assert.equal(evaluate(`window.detailProbe.calls.filter(c=>c.url==='/catalogs/tmdb/subjects/11111111-1111-4111-8111-111111111111').length`),count+1)
  evaluate(`window.setTimeout=window.savedTimeout;true`); close()
  assert.equal(browser('errors').trim(),'')
  console.log('通过：榜单来源身份→共享详情、来源事实、评分/演职员/人物作品、深浅主题、响应式、按需补全、轮询上限与重试、焦点恢复')
} catch(error) {
  console.error(evaluate('window.detailProbe.calls')); console.error(browser('snapshot','-i')); console.error(browser('errors')); throw error
} finally { browser('close') }
