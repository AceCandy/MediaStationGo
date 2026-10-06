// 本地 Vite + 合成资料；不访问真实来源和图片。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'
const base=process.env.HUANGGUOAI_TEST_URL||'http://127.0.0.1:4179'
const session=`huangguoai-search-${process.pid}`
const browser=(...args)=>execFileSync('agent-browser',['--session',session,...args],{encoding:'utf8',timeout:30000})
const evaluate=code=>{const r=JSON.parse(browser('--json','eval','-b',Buffer.from(code).toString('base64')));assert.equal(r.success,true,r.error);return r.data.result}
const wait=code=>browser('wait','--fn',code)
const click=name=>browser('find','role','button','click','--name',name,'--exact')
const route=(path,data)=>browser('network','route',base+'/api/'+path,'--body',JSON.stringify(data))
const work=i=>({id:`work-${i}`,source_id:String(1000+i),title:`合成搜索作品${i}`,kind:'series',source_category:'ai-duanju',categories:[],tags:['合成标签'],overview:'合成简介',hydrated:true,artwork_id:'',rating:8.2,episode_count:3,confirmed_episode_count:3})
const local=Array.from({length:52},(_,i)=>work(i));local[1]={...local[1],title:'本地待补齐1',hydrated:false}
const remote=[{...work(0),title:'官网待补齐0',hydrated:false,downloaded:true},{...work(1),title:'官网完整作品1'},...Array.from({length:22},(_,i)=>work(100+i))]
const remote2=[{...work(100),title:'续页待补齐100',hydrated:false},work(51),work(300)]
try {
 browser('open',base+'/login')
 route('auth/permissions',{permissions:{can_view_discover:true},role:'admin',is_super:true});route('play-profiles',[])
 route('catalogs/huangguoai/status',{enabled:true})
 route('catalogs/huangguoai/search?keyword=other&*',{items:[{...work(999),title:'新查询作品'}],has_more:false})
 route('catalogs/huangguoai/search?keyword=empty&*',{items:[],has_more:false})
 route('catalogs/huangguoai/search?*page=1',{items:remote,has_more:true})
 route('catalogs/huangguoai/search?*page=2',{items:remote2,has_more:false})
 route('catalogs/huangguoai/works?keyword=other&*',{items:[],total:0})
 route('catalogs/huangguoai/works?keyword=empty&*',{items:[],total:0})
 route('catalogs/huangguoai/works?*page=1&page_size=50',{items:local.slice(0,50),total:52})
 route('catalogs/huangguoai/works?*page=2&page_size=50',{items:local.slice(49),total:52})
 route('**',{})
 evaluate(`localStorage.setItem('mediastationgo-auth',JSON.stringify({state:{token:'synthetic-token',user:{id:'admin',username:'合成管理员',role:'admin',tier:'free'}},version:0}))`)
 browser('open',base+'/discover?system=huangguoai&keyword=synthetic&mode=remote&tag=old&page=2')
 wait(`document.body.innerText.includes('已显示 72 部')&&!document.querySelector('[role="status"]')`)
 assert.equal(evaluate(`new URLSearchParams(location.search).get('mode')`),null);assert.equal(evaluate(`new URLSearchParams(location.search).get('tag')`),null)
 assert.equal(evaluate(`new URLSearchParams(location.search).get('page')`),'1')
 assert.equal(evaluate(`document.querySelector('[aria-label="搜索范围"]')`),null);assert.equal(evaluate(`document.querySelector('[aria-label="题材标签"]')`),null)
 assert.deepEqual(evaluate(`[...document.querySelectorAll('button[aria-label^="查看"]')].slice(0,3).map(b=>b.getAttribute('aria-label'))`),['查看合成搜索作品0','查看官网完整作品1','查看合成搜索作品100'])
 assert.equal(evaluate(`document.querySelectorAll('[data-huangguoai-downloaded]').length`),1)
 for(const theme of ['dark','light']) {
  evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
  for(const width of [390,640,768,1440]) {browser('set','viewport',String(width),'900');assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth`));if(process.env.HUANGGUOAI_SCREENSHOT_DIR&&[390,1440].includes(width))browser('screenshot',`${process.env.HUANGGUOAI_SCREENSHOT_DIR}/search-${theme}-${width}.png`)}
 }
 evaluate(`window.requests=[];window.failRemotePage=true;window.failLocal=false;window.failRemote=false;window.holdRemote=false;
  const open=XMLHttpRequest.prototype.open,send=XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.open=function(method,url,...rest){this.testURL=String(url);return open.call(this,method,url,...rest)};
  XMLHttpRequest.prototype.send=function(body){window.requests.push(this.testURL);
   if((window.failLocal&&this.testURL.includes('/works?'))||(window.failRemote&&this.testURL.includes('/search?'))||(window.failRemotePage&&this.testURL.includes('/search?')&&this.testURL.includes('page=2')))throw new Error('合成搜索失败');
   if(window.holdRemote&&this.testURL.includes('/search?')&&this.testURL.includes('page=2')){window.releaseSearch=()=>send.call(this,body);return;}return send.call(this,body);}`)
 browser('scrollintoview','[data-testid="huangguoai-load-more"]')
 wait(`document.body.innerText.includes('官网搜索失败')&&document.body.innerText.includes('已显示 74 部')`)
 evaluate(`window.failRemotePage=false`);click('重试官网搜索')
 wait(`document.body.innerText.includes('已显示 75 部')&&document.body.innerText.includes('当前结果已加载完')`)
 assert.equal(evaluate(`window.requests.filter(url=>url.includes('/works?')).length`),1)
 assert.equal(evaluate(`window.requests.filter(url=>url.includes('/search?')).length`),2)
 assert.ok(evaluate(`document.querySelector('button[aria-label="查看合成搜索作品100"]')!==null`))
 browser('scroll','up','20000');evaluate(`window.failLocal=true;window.requests=[]`);click('刷新黄果AI目录')
 wait(`document.body.innerText.includes('资料读取失败')&&document.body.innerText.includes('已显示 24 部')`)
 evaluate(`window.failLocal=false`);click('重试加载')
 wait(`document.body.innerText.includes('已显示 72 部')&&!document.querySelector('[role="alert"]')`)
 assert.equal(evaluate(`window.requests.filter(url=>url.includes('/search?')).length`),1)
 evaluate(`window.failRemote=true;window.requests=[]`);click('刷新黄果AI目录')
 wait(`document.body.innerText.includes('官网搜索失败')&&document.body.innerText.includes('已显示 50 部')`)
 browser('scrollintoview','[data-testid="huangguoai-load-more"]')
 wait(`document.body.innerText.includes('已显示 52 部')&&document.body.innerText.includes('官网搜索失败')`)
 browser('scroll','up','20000');evaluate(`window.failRemote=false`);click('重试官网搜索')
 wait(`document.body.innerText.includes('已显示 74 部')&&!document.querySelector('[role="alert"]')`)
 assert.equal(evaluate(`window.requests.filter(url=>url.includes('/works?')).length`),2)
 evaluate(`window.holdRemote=true`);browser('scrollintoview','[data-testid="huangguoai-load-more"]');wait(`typeof window.releaseSearch==='function'`)
 browser('scroll','up','20000');browser('find','role','searchbox','fill','--name','搜索黄果AI标题或ID','other');click('搜索')
 wait(`document.querySelector('button[aria-label="查看新查询作品"]')!==null&&!document.querySelector('[role="status"]')`)
 evaluate(`window.releaseSearch()`)
 assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看"]').length`),1)
 browser('find','role','searchbox','fill','--name','搜索黄果AI标题或ID','empty');click('搜索')
 wait(`document.body.innerText.includes('暂无作品')&&!document.querySelector('[role="status"]')`)
 assert.equal(evaluate(`document.querySelectorAll('button[aria-label^="查看"]').length`),0)
 console.log('黄果AI统一搜索通过：控件移除/旧参数规范化、官网与本地去重/完整资料优先/下载标记、独立分页与失败重试、刷新归一、过期响应/空结果及暗亮响应式。')
} finally {browser('close')}
