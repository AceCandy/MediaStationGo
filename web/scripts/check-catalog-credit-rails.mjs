// 隔离组件和模拟接口：人员分组、每批20条、失败原页重试、独立游标与取消。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'
const base=process.env.DISCOVER_TEST_URL||'http://127.0.0.1:6243'
const session=`credit-rails-${process.pid}`
const browser=(...args)=>execFileSync('agent-browser',['--session',session,...args],{encoding:'utf8',timeout:30000})
const evaluate=code=>JSON.parse(browser('--json','eval','-b',Buffer.from(code).toString('base64'))).data.result
const wait=code=>browser('wait','--fn',code)
const count=title=>evaluate(`document.querySelector('[aria-label="${title}"]')?.querySelectorAll('img').length`)
const end=title=>evaluate(`(()=>{const row=document.querySelector('[aria-label="${title}列表"]');row.scrollLeft=row.scrollWidth;row.dispatchEvent(new Event('scroll'));return true})()`)
try {
 browser('open',base+'/login')
 browser('set','media','dark','reduced-motion')
 evaluate(`(async()=>{
  const {api}=await import('/src/api/client.ts'); const {TMDbCreditRows}=await import('/src/pages/TMDbCreditRows.tsx');const {CatalogCredits}=await import('/src/pages/CatalogCredits.tsx');
  const dependency=name=>performance.getEntriesByType('resource').find(e=>new URL(e.name).pathname.endsWith('/'+name+'.js')).name;
  const react=await import(dependency('react'));const dom=await import(dependency('react-dom_client'));const React=react.default??react;const {createRoot}=dom.default??dom;
  const host=document.createElement('div');host.style.cssText='position:fixed;inset:0;z-index:100;overflow:auto;background:var(--app-bg);padding:24px';document.body.appendChild(host);
  const p=window.railProbe={calls:[],fail:true,hold:false,root:createRoot(host),host};const image='/api/../brand/mediastationgo-logo.svg';
  api.defaults.adapter=async config=>{const category=config.params.category,page=config.params.page;p.calls.push({category,page,size:config.params.page_size,signal:config.signal});
    if(p.fail&&category==='cast'&&page===2)throw new Error('fixture failure');
    if(p.hold&&category==='crew'&&page===2)await new Promise(resolve=>p.release=resolve);
    const total=category==='cast'?45:25;
    const data=Array.from({length:Math.min(20,Math.max(0,total-(page-1)*20))},(_,i)=>{const n=(page-1)*20+i;return {id:category+n,category,job:category==='crew'?'Stunts':'',character:category==='cast'?'角色'+n:'',person:{person_id:category+n,name:category+n,profile_url:image}}});
    return {data:{data,total,page,page_size:20},status:200,headers:{},config};};
  p.showRemote=()=>p.root.render(React.createElement(TMDbCreditRows,{key:'remote',id:'subject',onPersonClick:v=>p.selected=v.person_id}));
  p.showLocal=()=>p.root.render(React.createElement(CatalogCredits,{key:'local',credits:Array.from({length:47},(_,i)=>({person_id:String(i),name:'本地人员'+i,type:i<45?'Actor':'Director',role:'角色',profile_url:image}))}));
  const {localizedName}=await import('/src/utils/localizedNames.ts');const {tmdbJob}=await import('/src/pages/tmdbLabels.ts');
  p.labels=[localizedName('GB','region','United Kingdom'),localizedName('US','region','United States'),localizedName('en','language','English'),localizedName('invalid','region','来源地区'),tmdbJob('Director / Stunts'),tmdbJob('Unknown Job')];
  p.showRemote();return true;
 })()`)
 wait(`document.querySelector('[aria-label="演员"]')?.querySelectorAll('img').length===20&&document.querySelector('[aria-label="幕后"]')?.querySelectorAll('img').length===20`)
 assert.deepEqual(evaluate('window.railProbe.labels'),['英国','美国','英语','来源地区','导演 / 特技','Unknown Job'])
 assert.deepEqual(evaluate('window.railProbe.calls.map(c=>[c.category,c.page,c.size])'),[['cast',1,20],['crew',1,20]])
 assert.ok(evaluate(`document.querySelector('[aria-label="演员"]').innerText.includes('45')&&document.querySelector('[aria-label="幕后"]').innerText.includes('25')&&document.querySelector('[aria-label="幕后"]').innerText.includes('特技')`))
 end('演员');wait(`document.querySelector('[aria-label="演员"] [role="alert"]')!==null`)
 assert.equal(count('演员'),20);assert.equal(count('幕后'),20)
 end('演员');assert.equal(evaluate('window.railProbe.calls.length'),3)
 evaluate('window.railProbe.fail=false;true');browser('wait','250');browser('find','role','button','click','--name','重试演员','--exact')
 wait(`document.querySelector('[aria-label="演员"]')?.querySelectorAll('img').length===40`)
 assert.deepEqual(evaluate(`window.railProbe.calls.filter(c=>c.category==='cast').map(c=>c.page)`),[1,2,2])
 assert.ok(evaluate(`document.querySelector('[aria-label="演员列表"]').scrollLeft>0`))
 end('演员');wait(`document.querySelector('[aria-label="演员"]')?.querySelectorAll('img').length===45`)
 assert.ok(!evaluate(`document.querySelector('[aria-label="演员"]').innerText.includes('加载更多')`))
 for(const theme of ['dark','light']) {
   evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
   for(const width of [390,768,1440]) {
     browser('set','viewport',String(width),'900');browser('wait','200')
     assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth&&window.railProbe.host.scrollWidth<=window.railProbe.host.clientWidth`))
     if(process.env.DISCOVER_SCREENSHOT_DIR)browser('screenshot',`${process.env.DISCOVER_SCREENSHOT_DIR}/credits-${theme}-${width}.png`)
   }
 }
 evaluate('window.railProbe.hold=true;true');end('幕后');wait(`typeof window.railProbe.release==='function'`)
 end('幕后');assert.equal(evaluate(`window.railProbe.calls.filter(c=>c.category==='crew').length`),2)
 evaluate('window.railProbe.showLocal();true');wait(`document.body.innerText.includes('本地人员0')`)
 assert.ok(evaluate(`window.railProbe.calls.at(-1).signal.aborted`))
 evaluate('window.railProbe.release();true');browser('wait','100')
 assert.equal(count('演员'),20);assert.equal(count('幕后'),2)
 evaluate(`document.querySelector('[aria-label="演员"] button').click();true`);wait(`document.querySelector('[aria-label="演员"]')?.querySelectorAll('img').length===40`)
 end('演员');wait(`document.querySelector('[aria-label="演员"]')?.querySelectorAll('img').length===45`)
 assert.equal(evaluate(`window.railProbe.calls.filter(c=>c.category==='crew').length`),2)
 assert.equal(evaluate(`document.querySelector('[aria-label="演员列表"]').tabIndex`),0)
 assert.ok(!evaluate(`document.body.innerText.includes('cast0')||document.body.innerText.includes('上一页')`))
 assert.equal(browser('errors').trim(),'')
 console.log('人员横滑通过：演员/幕后独立20条、顺序/计数、失败同页重试、追加保留位置、取消隔离、本地分批和双主题响应式。')
} catch(error) {console.error(evaluate('window.railProbe.calls.map(c=>({category:c.category,page:c.page}))'));console.error(browser('snapshot','-i'));throw error} finally {browser('close')}
