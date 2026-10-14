// 验证榜单周期不提交资料预算，资料任务仍保留预算。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'
const base = process.argv[2] || 'http://127.0.0.1:4179'
const session = `ranking-schedule-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64'))).data.result
const click = name => browser('find', 'role', 'button', 'click', '--name', name, '--exact')
const wait = code => browser('wait', '--fn', code)
try {
  browser('open', base + '/login')
  evaluate(`(async()=>{
    const {api}=await import('/src/api/client.ts');const {useAuthStore}=await import('/src/stores/auth.ts');
    window.scheduleWrites=[];
    window.scheduleDefinitions=[['tmdb_data_rank','tmdb','TMDb 排行榜刷新'],['douban_data_rank','douban','豆瓣排行榜刷新'],['tmdb_data_refresh','tmdb','TMDb 资料刷新']].map(([key,system,name])=>({key,system,name,description:'测试任务',trigger:'定时 / 手动',action:'scheduler',current_state:'idle',schedule_config:{enabled:true,interval_seconds:28800,min_interval_seconds:60,max_interval_seconds:2592000,...(key==='tmdb_data_refresh'?{count:100}:{})}}));
    api.defaults.adapter=async config=>{
      let data={};const path=config.url;
      if(path==='/auth/permissions')data={permissions:{},role:'admin',is_super:true};
      else if(path==='/play-profiles')data=[];
      else if(path==='/tasks/startup')data={state:'ready',warnings:[]};
      else if(path==='/tasks')data={items:[],total:0,definitions:window.scheduleDefinitions.filter(d=>d.system===config.params.system)};
      else if(path.endsWith('/schedule')){
        const body=JSON.parse(config.data);const d=window.scheduleDefinitions.find(d=>path.includes('/'+d.key+'/'));
        if(d.key.endsWith('_rank')&&'count' in body)throw new Error('榜单不支持数量');
        window.scheduleWrites.push(body);Object.assign(d.schedule_config,body);data=d;
      }
      return {data,status:200,headers:{},config};
    };
    useAuthStore.getState().setSession('synthetic-test-token','',{id:'admin',username:'测试',role:'admin',tier:'free'});
  })()`)
  for (const [system, name, budget] of [['tmdb','TMDb 排行榜刷新',false],['douban','豆瓣排行榜刷新',false],['tmdb','TMDb 资料刷新',true]]) {
    evaluate(`history.pushState(null,'','/admin/tasks?system=${system}');dispatchEvent(new PopStateEvent('popstate'));true`)
    wait(`document.body.innerText.includes(${JSON.stringify(name)})`)
    click(`设置${name}周期`)
    wait(`!!document.querySelector('[role=dialog]')`)
    assert.equal(evaluate(`document.querySelector('[role=dialog]').innerText.includes('每轮接口请求预算')`),budget)
    if(budget)browser('find','label','每轮接口请求预算','fill','99')
    browser('find','label','执行周期','fill','6')
    click('保存');wait(`!document.querySelector('[role=dialog]')`)
    assert.deepEqual(evaluate('window.scheduleWrites.at(-1)'),{enabled:true,interval_seconds:21600,...(budget?{count:99}:{})})
    click(`设置${name}周期`);wait(`!!document.querySelector('[role=dialog]')`)
    assert.equal(evaluate(`document.querySelector('[role=dialog] input[type=number]').value`),'6')
    click('取消')
  }
  evaluate(`window.scheduleDefinitions=[
    {key:'tmdb_data_images',name:'图片下载测试',metrics:{processed:8,failed:1}},
    {key:'tmdb_data_season',name:'季资料测试',metrics:{processed:3,selected:3,scanned:7,updated:9,requested:12}}
  ].map(({metrics,...d})=>({...d,system:'tmdb',trigger:'定时',current_state:'idle',schedule_config:{enabled:true,interval_seconds:28800,min_interval_seconds:60,max_interval_seconds:2592000},latest:{status:'completed',metrics}}));history.pushState(null,'','/admin/tasks?system=douban');dispatchEvent(new PopStateEvent('popstate'));true`)
  wait(`!document.body.innerText.includes('TMDb 资料刷新')`)
  evaluate(`history.pushState(null,'','/admin/tasks?system=tmdb');dispatchEvent(new PopStateEvent('popstate'));true`)
  wait(`document.body.innerText.includes('图片下载测试')&&document.body.innerText.includes('季资料测试')`)
  const progress = evaluate(`[...document.querySelectorAll('tr')].map(row=>row.innerText)`)
  assert.ok(progress.some(text=>text.includes('图片下载测试')&&text.includes('本轮处理 8')&&!text.includes('已更新')))
  assert.ok(progress.some(text=>text.includes('季资料测试')&&text.includes('本轮处理 3 / 3')&&text.includes('已核对 7 条元数据')&&text.includes('已更新 9')&&text.includes('接口请求 12')))
  evaluate(`window.scheduleDefinitions=[
    {key:'scheduled',name:'定时开启测试',schedule_config:{enabled:true}},
    {key:'disabled',name:'定时关闭测试',schedule_config:{enabled:false}},
    {key:'event',name:'事件自动测试',automatic:true,schedule_config:{enabled:false}},
    {key:'manual',name:'手动测试',current_state:'running'}
  ].map(d=>({system:'tmdb',trigger:'测试',current_state:'idle',...d}));history.pushState(null,'','/admin/tasks?system=douban');dispatchEvent(new PopStateEvent('popstate'));true`)
  wait(`!document.body.innerText.includes('季资料测试')`)
  evaluate(`history.pushState(null,'','/admin/tasks?system=tmdb');dispatchEvent(new PopStateEvent('popstate'));true`)
  wait(`document.body.innerText.includes('定时开启测试')`)
  for (const width of [390, 1440]) {
    browser('set','viewport',String(width),'900')
    for (const theme of ['light','dark']) {
      evaluate(`document.documentElement.dataset.theme='${theme}'`)
      const dots = evaluate(`[...document.querySelectorAll('[role="img"][aria-label]')].filter(n=>n.getClientRects().length).map(n=>n.getAttribute('aria-label'))`)
      assert.deepEqual(dots.sort(),['已启用定时执行','支持事件自动执行'].sort())
      assert.ok(evaluate(`document.documentElement.scrollWidth<=innerWidth`))
    }
  }
  console.log('通过：周期保存、进度指标、自动执行绿点；定时关闭/手动运行不亮，事件任务亮，桌面手机双主题无溢出。')
} finally { browser('close') }
