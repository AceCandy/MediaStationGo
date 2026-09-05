// 启动前端开发服务后运行：node tests/search-entry.mjs [服务地址]
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import process from 'node:process'
import console from 'node:console'

const session = `search-entry-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8' })
const evaluate = (source) => browser('eval', `(async () => { ${source} })()`)
const check = (condition) => evaluate(`if (!(${condition})) throw new Error(${JSON.stringify(condition)}); true`)
const wait = (condition) => browser('wait', '--fn', `Boolean(${condition})`)

try {
  browser('open', process.argv[2] ?? 'http://127.0.0.1:5179')
  wait(`document.querySelector('input[placeholder="请输入您的账号"]')`)
  evaluate(`
    const { api } = await import('/src/api/client.ts');
    const { useAuthStore } = await import('/src/stores/auth.ts');
    window.searchCalls = [];
    window.aiEnabled = true;
    api.defaults.adapter = async (config) => {
      let data = [];
      if (config.url === '/ai/status') data = { enabled: window.aiEnabled };
      if (config.url === '/media') {
        window.searchCalls.push({ mode: 'default', query: config.params.q });
        data = { items: [], total: 0 };
      }
      if (config.url === '/ai/search') {
        const { query } = JSON.parse(config.data);
        window.searchCalls.push({ mode: 'ai', query });
        if (query === '延迟请求') await new Promise(resolve => { window.releaseSearch = resolve });
        data = {
          items: [{ id: 'local-result', library_id: 'test-library', title: query, path: '/movies/test.mkv', media_type: 'movie' }],
          external_items: [{ source: 'tmdb', title: query }],
          intent: { query, language: 'zh' },
        };
      }
      return { data, status: 200, statusText: 'OK', headers: {}, config };
    };
    useAuthStore.getState().setSession('local-test', '', { id: 'search-test', username: '测试', role: 'admin' });
    history.pushState({}, '', '/search');
    dispatchEvent(new PopStateEvent('popstate'));
  `)
  wait(`document.querySelector('[aria-label="开启智能搜索"]')`)
  check(`document.querySelectorAll('input').length === 1`)
  check(`!document.querySelector('header').textContent.includes('Enter')`)
  browser('fill', 'input[name="global-search"]', '孙悟空')
  browser('press', 'Enter')
  wait(`window.searchCalls.some(call => call.mode === 'default' && call.query === '孙悟空')`)
  browser('click', '[aria-label="开启智能搜索"]')
  wait(`window.searchCalls.some(call => call.mode === 'ai' && call.query === '孙悟空')`)
  check(`document.querySelector('form [aria-pressed="true"]') && location.search.includes('mode=ai')`)
  browser('fill', 'input[name="global-search"]', '星际穿越')
  browser('press', 'Enter')
  wait(`document.querySelector('main').textContent.includes('星际穿越')`)
  check(`(() => {
    const main = document.querySelector('main');
    const text = main.textContent;
    return text.indexOf('外部数据源') >= 0 && text.indexOf('本地媒体库') > text.indexOf('外部数据源') &&
      main.querySelector('h2 [title="AI 辅助搜索"] svg');
  })()`)
  check(`!document.querySelector('main').textContent.includes('AI 解析') && !document.querySelector('main').textContent.includes('"language"')`)
  const count = evaluate(`return window.searchCalls.filter(call => call.mode === 'ai').length`).trim()
  browser('press', 'Enter')
  wait(`window.searchCalls.filter(call => call.mode === 'ai').length > ${Number(count)}`)
  browser('fill', 'input[name="global-search"]', '延迟请求')
  browser('press', 'Enter')
  wait(`Boolean(window.releaseSearch)`)
  browser('click', '[aria-label="关闭智能搜索"]')
  wait(`window.searchCalls.some(call => call.mode === 'default' && call.query === '延迟请求')`)
  evaluate(`window.releaseSearch(); await new Promise(resolve => setTimeout(resolve, 100))`)
  check(`!document.querySelector('main').textContent.includes('外部数据源')`)
  browser('click', '[aria-label="开启智能搜索"]')
  wait(`document.querySelector('[aria-label="关闭智能搜索"]')`)
  for (const [width, height] of [[390, 844], [639, 900], [640, 900], [768, 1024], [1440, 900]]) {
    browser('set', 'viewport', String(width), String(height))
    console.log(`检查布局 ${width}x${height}`)
    check(`document.documentElement.scrollWidth <= innerWidth`)
    check(`(() => {
      const input = document.querySelector('input[name="global-search"]');
      const toggle = document.querySelector('[aria-label="关闭智能搜索"]');
      const rect = input.getBoundingClientRect();
      const button = toggle.getBoundingClientRect();
      return rect.width > 100 && rect.left >= 0 && rect.right <= innerWidth &&
        document.elementFromPoint(button.x + button.width / 2, button.y + button.height / 2)?.closest('button') === toggle;
    })()`)
    if (process.argv[3] && (width === 390 || width === 1440)) {
      for (const theme of ['dark', 'light']) {
        evaluate(`document.documentElement.dataset.theme = '${theme}'`)
        browser('screenshot', `${process.argv[3]}/${width}-${theme}.png`)
      }
    }
  }
  evaluate(`
    window.aiEnabled = false;
    const { useAuthStore } = await import('/src/stores/auth.ts');
    useAuthStore.getState().setUser({ id: 'unavailable-test', username: '测试', role: 'admin' });
  `)
  wait(`!location.search.includes('mode=ai')`)
  check(`!document.querySelector('form [aria-pressed]') && document.querySelector('input[name="global-search"]')`)
  evaluate(`
    window.aiEnabled = true;
    const { useAuthStore } = await import('/src/stores/auth.ts');
    useAuthStore.getState().setUser({ id: 'denied-test', username: '测试', role: 'user' });
    history.pushState({}, '', '/search?q=权限测试&mode=ai');
    dispatchEvent(new PopStateEvent('popstate'));
  `)
  wait(`!location.search.includes('mode=ai') && window.searchCalls.some(call => call.query === '权限测试')`)
  check(`!window.searchCalls.some(call => call.mode === 'ai' && call.query === '权限测试')`)
  assert.equal(browser('errors').trim(), '')
  console.log('搜索入口：普通搜索、智能切换、重复提交、旧请求隔离、能力降级及五档布局检查通过。')
} catch (error) {
  console.error(browser('snapshot', '-i'))
  throw error
} finally {
  browser('close')
}
