// 本地 Vite 与合成 API；不访问来源网站、图片或媒体。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'
const base = process.env.DISCOVER_TEST_URL || 'http://127.0.0.1:4179'
const session = `catalog-rankings-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => { const r=JSON.parse(browser('--json','eval','-b',Buffer.from(code).toString('base64')));assert.equal(r.success,true,r.error);return r.data.result }
const wait = code => browser('wait','--fn',code)
const click = name => browser('find','role','button','click','--name',name,'--exact')
const route = (path, data) => browser('network','route',`${base}/api/${path}`,'--body',JSON.stringify(data))
const items = Array.from({length:43},(_,i)=>({id:`synthetic-${i}`,source_id:String(8000+i),title:`合成榜单作品${i}`,kind:'series',source_category:'ai-duanju',categories:[],tags:['合成题材'],overview:'合成简介，用来检查展开和长文本布局。'.repeat(15),artwork_id:i===1?'synthetic-broken':'',hydrated:i!==2,downloaded:i===0,rating:8.3,episode_count:8,confirmed_episode_count:8,update_text:'更新至8集'}))
try {
 browser('open',base+'/login')
 route('auth/permissions',{permissions:{can_view_discover:true},role:'admin',is_super:true});route('play-profiles',[])
 for(const source of ['hongguo','huangguoai']) {
  route(`catalogs/${source}/status`,{enabled:true})
  for(let p=1;p<=5;p++) route(`catalogs/${source}/works?*page=${p}&page_size=10`,{items:items.slice((p-1)*10,p*10),total:43})
  route(`catalogs/${source}/works?*page=1&page_size=50`,{items,total:43})
  route(`catalogs/${source}/works/8000/episodes?*`,{items:[],total:0})
  route(`catalogs/${source}/works/8000/media?*`,{items:[],total:0})
  route(`catalogs/${source}/works/8000/state`,{favorite:false})
  route(`catalogs/${source}/works/8000`,{...items[0],artwork:[],credits:[],episodes:[]})
  route(`catalogs/${source}/artwork/*`,{})
 }
 route('**',{})
 evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'synthetic-token',user:{id:'admin',username:'合成管理员',role:'admin',tier:'free'}},version:0}))`)
 for(const source of ['hongguo','huangguoai']) {
  browser('open',`${base}/discover?system=${source}&section=rank&rank=${source==='hongguo'?'hot-drama':'hot'}&page=3`)
  wait(`document.querySelectorAll('.ranking-row').length===10&&new URLSearchParams(location.search).get('page')==='1'`)
  assert.equal(evaluate(`document.querySelector('.ranking-position').textContent`),'01')
  assert.ok(evaluate(`document.querySelector('.ranking-row').innerText.includes('8.3分')&&document.querySelector('.ranking-row').innerText.includes('已下载')`))
  assert.ok(evaluate(`document.querySelectorAll('.ranking-row')[2].innerText.includes('待补齐')`))
  assert.ok(evaluate(`!!document.querySelector('[data-testid="${source}-load-more"]')`))
  assert.equal(evaluate(`document.querySelector('nav[aria-label="排行榜分页"]')`),null)
  wait(`!document.querySelectorAll('.ranking-row')[1].querySelector('img')`)
  browser('set','viewport','1440','900');browser('hover','.ranking-header')
  const height=evaluate(`document.querySelector('.ranking-row').getBoundingClientRect().height`)
  wait(`getComputedStyle(document.querySelector('.ranking-poster')).opacity==='0'`)
  browser('hover','.ranking-row');wait(`getComputedStyle(document.querySelector('.ranking-poster')).opacity==='1'`)
  assert.ok(evaluate(`document.querySelector('.ranking-row').getBoundingClientRect().height`)>height+80)
  browser('hover','.ranking-header');wait(`getComputedStyle(document.querySelector('.ranking-poster')).opacity==='0'`)
  evaluate(`document.querySelector('.ranking-row').focus()`);browser('press','Tab');browser('press','Shift+Tab')
  wait(`document.querySelector('.ranking-row').matches(':focus-visible')&&getComputedStyle(document.querySelector('.ranking-poster')).opacity==='1'`)
  assert.equal(evaluate(`getComputedStyle(document.querySelector('.ranking-overview')).opacity`),'1')
  for(const theme of ['dark','light']) {
   evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
   for(const width of [390,639,640,768,1440]) {
    browser('set','viewport',String(width),'900')
    wait(`[...document.querySelectorAll('.ranking-row')].every(e=>e.getAnimations({subtree:true}).every(a=>a.playState!=='running'))`)
    assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth`),`${source}/${theme}/${width} overflow`)
    assert.ok(evaluate(`document.querySelector('.ranking-row').scrollWidth<=document.querySelector('.ranking-row').clientWidth`),`${source}/${theme}/${width} row overflow`)
    if(width===390) {
     assert.ok(evaluate(`document.querySelector('.ranking-header').firstElementChild.getBoundingClientRect().width>280`),`${source}/${theme} squeezed heading`)
     assert.ok(evaluate(`document.querySelector('.ranking-header h2').getBoundingClientRect().height<=72`),`${source}/${theme} unreadable heading`)
    }
    if(process.env.RANKING_SCREENSHOT_DIR&&[390,1440].includes(width)) browser('screenshot',`${process.env.RANKING_SCREENSHOT_DIR}/${source}-${theme}-${width}.png`)
   }
  }
  browser('set','viewport','1440','900');browser('set','media','dark','reduced-motion')
  assert.ok(evaluate(`parseFloat(getComputedStyle(document.querySelector('.ranking-poster')).transitionDuration)`)<=0.001)
  evaluate(`window.rankRequests=[];window.failRankPage=true;window.holdRankPage=false;
   const open=XMLHttpRequest.prototype.open,send=XMLHttpRequest.prototype.send;
   XMLHttpRequest.prototype.open=function(method,url,...rest){this.rankURL=String(url);return open.call(this,method,url,...rest)};
   XMLHttpRequest.prototype.send=function(body){if(this.rankURL.includes('/works?'))window.rankRequests.push(this.rankURL);
    if(this.rankURL.includes('/works?')&&this.rankURL.includes('page=2&')){if(window.failRankPage)throw new Error('合成分页失败');if(window.holdRankPage){window.releaseRankPage=()=>send.call(this,body);return;}}return send.call(this,body);}`)
  const scrollMore = () => {
   evaluate(`document.activeElement?.blur()`);browser('mouse','move','0','0')
   wait(`[...document.querySelectorAll('.ranking-row')].every(e=>e.getAnimations({subtree:true}).every(a=>a.playState!=='running'))`)
   browser('scrollintoview',`[data-testid="${source}-load-more"]`)
  }
  scrollMore();wait(`document.querySelector('[role="alert"]')!==null`)
  assert.equal(evaluate(`document.querySelectorAll('.ranking-row').length`),10)
  assert.equal(evaluate(`new URLSearchParams(location.search).get('page')||'1'`),'1')
  evaluate(`window.failRankPage=false;document.querySelector("main").scrollTo(0,0)`);click('重试加载');wait(`document.querySelectorAll('.ranking-row').length===20`)
  for(const count of [30,40,43]) {scrollMore();wait(`document.querySelectorAll('.ranking-row').length===${count}`);evaluate(`document.querySelector("main").scrollTo(0,0)`)}
  assert.deepEqual(evaluate(`[...document.querySelectorAll('.ranking-position')].map(e=>e.textContent)`),items.map((_,i)=>String(i+1).padStart(2,'0')))
  assert.ok(!evaluate(`[...document.querySelectorAll('button')].some(b=>b.textContent==='加载更多')`))
  assert.deepEqual(evaluate(`window.rankRequests.map(url=>{const p=new URL(url,location.origin).searchParams;return[p.get('page'),p.get('page_size')]})`),[['2','10'],['2','10'],['3','10'],['4','10'],['5','10']])
  click(source==='hongguo'?'刷新红果目录':'刷新黄果AI目录');wait(`document.querySelectorAll('.ranking-row').length===10`)
  click(source==='hongguo'?'AI剧热播榜':'推荐榜');wait(`document.querySelectorAll('.ranking-row').length===10&&new URLSearchParams(location.search).get('rank')==='${source==='hongguo'?'hot-ai-drama':'recommend'}'`)
  evaluate(`window.holdRankPage=true`);scrollMore();wait(`typeof window.releaseRankPage==='function'`)
  click(source==='hongguo'?'红果热播榜':'热播榜');wait(`document.querySelectorAll('.ranking-row').length===10&&document.querySelector('.ranking-position')?.textContent==='01'&&document.querySelector('.ranking-header h2').textContent==='${source==='hongguo'?'红果热播榜':'热播榜'}'`)
  evaluate(`window.releaseRankPage();document.querySelector("main").scrollTo(0,0)`)
  assert.equal(evaluate(`document.querySelectorAll('.ranking-row').length`),10)
  if(source==='hongguo') assert.ok(!evaluate(`[...document.querySelectorAll('button')].some(b=>b.textContent==='多选')`))
  const beforeDetail=evaluate(`window.rankRequests.length`)
  click('查看合成榜单作品0');wait(`document.querySelector('[role="dialog"]')?.innerText.includes('合成简介')`)
  browser('press','Escape');wait(`!document.querySelector('[role="dialog"]')`)
  assert.equal(evaluate(`document.querySelectorAll('.ranking-row').length`),10)
  assert.equal(evaluate(`window.rankRequests.length`),beforeDetail)
  evaluate(`window.holdRankPage=false`);scrollMore();wait(`document.querySelectorAll('.ranking-row').length===20`)
  click('分类');wait(`document.querySelectorAll('button[aria-label^="查看合成榜单作品"]').length===43&&!document.querySelector('.ranking-row')`)
 }
 console.log('排行榜检查通过：10条下滑续载/连续排名/无分页、失败重试/旧响应隔离、hover与键盘展开/减少动效、详情保留、红果榜单无多选、分类回归、暗亮主题与手机布局。')
} finally {browser('close')}
