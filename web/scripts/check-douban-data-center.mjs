// 使用隔离 Vite 开发服务：node scripts/check-douban-data-center.mjs [URL]；所有 API 均模拟。
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import console from 'node:console'
import process from 'node:process'

const base = process.argv[2] || 'http://127.0.0.1:6239'
const session = `douban-center-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => JSON.parse(browser('eval', '-b', Buffer.from(code).toString('base64'), '--json')).data.result
const wait = code => browser('wait', '--fn', code)
const text = 'document.querySelector("main")?.innerText'
const click = name => {
  if(name==='关闭详情') {
    browser('focus','[aria-label="关闭详情"]')
    browser('press','Enter')
    wait("!document.querySelector('[role=dialog]')")
  } else browser('find', 'role', 'button', 'click', '--name', name, '--exact')
}
const navigate = path => evaluate(`history.pushState(null,'',${JSON.stringify(path)}); dispatchEvent(new PopStateEvent('popstate')); true`)

try {
  browser('open', base + '/login')
  browser('set','media','dark','reduced-motion')
  browser('network', 'route', base + '/api/**', '--body', '{}')
  evaluate(`(async () => {
    const {api}=await import('/src/api/client.ts');
    const {useAuthStore}=await import('/src/stores/auth.ts');
    const probe=window.doubanProbe={calls:[],hold:'',releases:[],failList:false,failDetail:false,refreshMode:'success',role:'admin',updated:false,emptyList:false};
    const subject=(id,kind,quality='complete',has_payload=true)=>({id,douban_id:id.startsWith('111')?'100':id.slice(0,3),entity_kind:kind,title:kind==='movie'?'测试电影'+ '很长的标题'.repeat(8):'历史单集',year:'2026',rating:probe.updated?9.2:8.1,quality,has_payload,fetched_at:has_payload?'2026-10-10T12:00:00Z':null,last_attempt_at:'2026-10-10T12:00:00Z',next_retry_at:null});
    probe.movie=subject('11111111-1111-4111-8111-111111111111','movie');
    probe.episode=subject('22222222-2222-4222-8222-222222222222','episode');
    api.defaults.adapter=async config=>{
      if(config.url==='/auth/permissions') return {data:{permissions:{can_view_discover:true},role:probe.role,is_super:probe.role==='admin'},status:200,headers:{},config};
      if(config.url==='/play-profiles') return {data:[],status:200,headers:{},config};
      if(!config.url.startsWith('/admin/douban/subjects')) return {data:{},status:200,headers:{},config};
      const kind=config.method==='post'?'refresh':config.url==='/admin/douban/subjects'?'list':'detail';
      const call={kind,...config.params,aborted:false};probe.calls.push(call);
      config.signal?.addEventListener('abort',()=>{call.aborted=true},{once:true});
      if(probe.hold===kind) await new Promise(resolve=>probe.releases.push(resolve));
      const fail=kind==='list'?probe.failList:kind==='detail'?probe.failDetail:probe.refreshMode==='failure';
      if(fail) throw Object.assign(new Error('test failure'),{config,response:{status:503,data:{message:'模拟读取失败，已有资料已保留'}}});
      let data;
      if(kind==='list'){
        const p=config.params, empty=p.keyword==='empty'||probe.emptyList;
        let rows=p.page===2?[subject('33333333-3333-4333-8333-333333333333','series')]:[subject(probe.movie.id,'movie'),probe.episode,subject('44444444-4444-4444-8444-444444444444','movie','degraded'),subject('55555555-5555-4555-8555-555555555555','movie','partial'),subject('66666666-6666-4666-8666-666666666666','movie','',false)];
        if(p.type) rows=rows.filter(row=>row.entity_kind===p.type);
        data={data:empty?[]:rows,total:empty?0:p.type?rows.length:31,page:p.page,page_size:30};
      }else{
        const item=config.url.includes(probe.episode.id)?probe.episode:config.url.includes('66666666')?subject('66666666-6666-4666-8666-666666666666','movie','',false):subject(probe.movie.id,'movie');
        const detail={...item,original_name:'Original title',overview:'已落库的简介',release_date:'2026-01-01',genres:['剧情'],languages:['中文'],countries:['中国'],raw_json:{title:item.title,intro:'已落库的简介',private_detail:'原始资料'}};
        if(kind==='refresh') {if(probe.refreshMode==='success') probe.updated=true; detail.rating=probe.updated?9.2:8.1; data={data:{subject:detail,degraded:probe.refreshMode==='degraded'}};}
        else data={data:detail};
      }
      return {data,status:200,headers:{},config};
    };
    probe.auth=useAuthStore;
    useAuthStore.getState().setSession('local-test-token','',{id:'admin',username:'测试管理员',role:'admin',tier:'free'});
    history.pushState(null,'','/admin/data-center?keyword= &type=invalid&page=01&page=2');
    dispatchEvent(new PopStateEvent('popstate'));
    return true;
  })()`)
  wait(`${text}?.includes('测试电影')`)
  assert.equal(evaluate('location.search'), '')
  assert.ok(evaluate(`!!document.querySelector('a[href="/admin/data-center"]')`))
  assert.ok(evaluate(`${text}.includes('未获取资料') && ${text}.includes('降级资料') && ${text}.includes('摘要资料')`))
  assert.ok(evaluate("window.doubanProbe.calls.filter(c=>c.kind==='list').every(c=>c.page_size===30)"))
  assert.equal(evaluate("window.doubanProbe.calls.filter(c=>c.kind==='refresh').length"), 0)

  evaluate('window.doubanProbe.emptyList=true; true')
  click('重新读取列表')
  wait(`${text}?.includes('暂无豆瓣资料')`)
  evaluate('window.doubanProbe.emptyList=false; true')
  click('重新读取列表')
  wait(`${text}?.includes('测试电影')`)
  browser('click','main tbody tr:last-child button')
  wait("document.querySelector('[role=dialog]')?.innerText.includes('该记录尚未成功获取资料')")
  assert.equal(evaluate("!!document.querySelector('[role=dialog] details')"),false)
  click('关闭详情')

  click('下一页')
  wait("location.search.includes('page=2') && document.querySelector('main').innerText.includes('整剧')")
  click('上一页')
  wait(`${text}?.includes('测试电影')`)
  browser('fill', '[aria-label="名称或豆瓣 ID"]', ' 筛选 ')
  click('搜索')
  wait("new URLSearchParams(location.search).get('keyword')==='筛选'")
  assert.equal(evaluate('new URLSearchParams(location.search).has("page")'), false)
  browser('click', '[aria-label="资料类型"]')
  browser('find', 'role', 'option', 'click', '--name', '集', '--exact')
  wait(`${text}?.includes('历史单集') && !${text}?.includes('测试电影')`)
  browser('find', 'role', 'button', 'click', '--name', '查看', '--exact')
  wait("!!document.querySelector('[role=dialog]') && document.querySelector('[role=dialog]').innerText.includes('已落库的简介')")
  assert.ok(evaluate("document.querySelector('[role=dialog]').innerText.includes('历史季和集资料仅支持查看')"))
  assert.equal(evaluate("[...document.querySelectorAll('[role=dialog] button')].some(b=>b.textContent.includes('更新'))"), false)
  click('关闭详情')

  navigate('/admin/data-center')
  wait(`${text}?.includes('测试电影')`)
  for (const theme of ['light','dark']) {
    evaluate(`document.documentElement.dataset.theme='${theme}'; true`)
    for (const [width,height] of [[390,844],[768,1024],[1279,900],[1280,900],[1440,900]]) {
      console.log(`检查布局：${theme}/${width}`)
      browser('set','viewport',String(width),String(height))
      browser('wait','150')
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} list overflow`)
      const selector=width<1280?'main article button':'main tbody tr button'
      browser('click',selector)
      wait("document.querySelector('[role=dialog]')?.innerText.includes('已落库的简介')")
      assert.equal(evaluate("document.querySelector('[role=dialog] details').open"), false)
      browser('click','[role=dialog] summary')
      browser('wait','250')
      assert.ok(evaluate("document.querySelector('[role=dialog] pre').innerText.includes('原始资料')"))
      assert.ok(evaluate('document.documentElement.scrollWidth <= innerWidth'), `${theme}/${width} dialog overflow`)
      click('关闭详情')
      wait("!document.querySelector('[role=dialog]')")
    }
  }

  browser('click','main tbody tr button')
  wait("document.querySelector('[role=dialog]')?.innerText.includes('已落库的简介')")
  evaluate("window.doubanProbe.hold='refresh'; true")
  click('更新豆瓣资料')
  wait("window.doubanProbe.calls.some(c=>c.kind==='refresh')")
  evaluate("document.querySelector('[role=dialog] footer button').click(); true")
  assert.equal(evaluate("window.doubanProbe.calls.filter(c=>c.kind==='refresh').length"),1)
  assert.ok(evaluate("document.querySelector('[role=dialog] footer button').disabled"))
  evaluate("window.doubanProbe.hold=''; window.doubanProbe.releases.splice(0).forEach(resolve=>resolve()); true")
  wait("document.querySelector('[role=dialog]')?.innerText.includes('9.2')")
  evaluate("window.doubanProbe.refreshMode='failure'; true")
  click('更新豆瓣资料')
  wait("document.querySelector('[role=dialog]')?.innerText.includes('模拟读取失败') && !document.querySelector('[role=dialog] footer button').disabled")
  assert.ok(evaluate("document.querySelector('[role=dialog]').innerText.includes('9.2')"))
  evaluate("window.doubanProbe.refreshMode='degraded'; true")
  click('更新豆瓣资料')
  wait("document.body.innerText.includes('完整接口受限，已保留已有完整资料')")
  click('关闭详情')

  evaluate("window.doubanProbe.failList=true; true")
  click('重新读取列表')
  wait(`${text}?.includes('模拟读取失败')`)
  evaluate("window.doubanProbe.failList=false; true")
  click('重试读取')
  wait(`${text}?.includes('测试电影')`)
  evaluate("window.doubanProbe.failDetail=true; true")
  browser('click','main tbody tr button')
  wait("document.querySelector('[role=dialog]')?.innerText.includes('模拟读取失败')")
  evaluate("window.doubanProbe.failDetail=false; true")
  click('重试读取详情')
  wait("document.querySelector('[role=dialog]')?.innerText.includes('已落库的简介')")
  click('关闭详情')

  evaluate("window.doubanProbe.hold='list'; true")
  navigate('/admin/data-center?keyword=old')
  wait("window.doubanProbe.calls.some(c=>c.keyword==='old')")
  evaluate("window.doubanProbe.hold=''; true")
  navigate('/admin/data-center?keyword=empty')
  wait(`${text}?.includes('没有符合条件')`)
  assert.ok(evaluate("window.doubanProbe.calls.find(c=>c.keyword==='old').aborted"))
  evaluate("window.doubanProbe.releases.splice(0).forEach(resolve=>resolve()); true")
  assert.ok(evaluate(`${text}.includes('没有符合条件')`))
  navigate('/admin/data-center')
  wait(`${text}?.includes('测试电影')`)
  evaluate("window.doubanProbe.hold='detail'; true")
  browser('click','main tbody tr button')
  wait("window.doubanProbe.releases.length===1")
  click('关闭详情')
  assert.ok(evaluate("window.doubanProbe.calls.filter(c=>c.kind==='detail').at(-1).aborted"))
  evaluate("window.doubanProbe.hold=''; window.doubanProbe.releases.splice(0).forEach(resolve=>resolve()); true")

  evaluate("window.doubanProbe.hold='refresh'; true")
  click('更新豆瓣 100')
  wait("window.doubanProbe.releases.length===1")
  evaluate("window.doubanProbe.role='user'; window.doubanProbe.auth.getState().setSession('local-viewer-token','',{id:'viewer',username:'测试用户',role:'user',tier:'free'}); true")
  wait("!document.querySelector('a[href=\"/admin/data-center\"]') && !document.querySelector('[aria-label=\"豆瓣资料列表\"]')")
  assert.ok(evaluate("window.doubanProbe.calls.filter(c=>c.kind==='refresh').at(-1).aborted"))
  evaluate("window.doubanProbe.hold=''; window.doubanProbe.releases.splice(0).forEach(resolve=>resolve()); true")
  assert.equal(browser('errors').trim(), '')
  console.log('通过：管理员入口、URL/分页/类型、质量/空态、详情/JSON、强刷/失败/降级、防重复、取消/会话隔离、深浅主题和响应式布局')
} catch(error) {
  console.error(browser('snapshot','-i'))
  console.error(browser('errors'))
  throw error
} finally {
  browser('close')
}
