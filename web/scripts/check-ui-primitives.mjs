// UI_TEST_URL=http://127.0.0.1:4191 node scripts/check-ui-primitives.mjs
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { Buffer } from 'node:buffer'
import process from 'node:process'
import console from 'node:console'

const base = process.env.UI_TEST_URL || 'http://127.0.0.1:4191'
const session = `ui-primitives-${process.pid}`
const browser = (...args) => execFileSync('agent-browser', ['--session', session, ...args], { encoding: 'utf8', timeout: 30000 })
const evaluate = code => {
  const result = JSON.parse(browser('--json', 'eval', '-b', Buffer.from(code).toString('base64')))
  assert.equal(result.success, true, result.error)
  return result.data.result
}
const wait = code => browser('wait', '--fn', code)
const route = (path, data) => browser('network', 'route', `${base}/api/${path}`, '--body', JSON.stringify(data))
const pages = [
  ['/admin/settings/general', '系统设置分类', 'a'],
  ['/discover', '发现资料体系', 'button'],
  ['/me?source=hongguo', '我的内容', 'a'],
  ['/libraries', '媒体库视图', 'a'],
  ['/admin/tasks?system=hongguo', '任务体系', 'button'],
  ['/admin/media/files', '自动整理设置分类', 'button'],
  ['/admin/media/downloads', '下载来源', 'button'],
]

try {
  browser('open', `${base}/login`)
  route('auth/permissions', { permissions: { can_view_discover: true }, role: 'admin', is_super: true })
  route('play-profiles', [])
  route('tasks/startup', { state: 'ready', stage: '初始化完成', elapsed_seconds: 0, stage_elapsed_seconds: 0, directories_found: 0, directories_watched: 0, warnings: [] })
  route('libraries*', [])
  route('admin/settings', [])
  route('discover/sections', { sections: [] })
  route('catalogs/hongguo/me?*', { items: [], total: 0 })
  route('files?*', { path: '/style-check', entries: [{ name: '用于检查长文件名与行内操作的示例视频.mkv', path: '/style-check/video.mkv', is_dir: false, size: 1024, modified: 0 }] })
  route('catalogs/hongguo/downloads/config', { root: '', concurrency: 1, verification_concurrency: 1, priority: 'normal' })
  route('**', { items: [], total: 0 })
  evaluate(`localStorage.setItem('mediastationgo-auth', JSON.stringify({ state: { token: 'local-test-token', user: { id: 'ui-admin', username: '样式检查', role: 'admin', tier: 'free' } }, version: 0 }))`)
  const reference = {}
  for (const [path, label, tag] of pages) {
    browser('open', base + path)
    const selector = `[aria-label="${label}"]`
    wait(`!!document.querySelector(${JSON.stringify(selector)})`)
    assert.equal(evaluate(`document.querySelectorAll('#main-content h1').length`), 0, `${label}: redundant page heading`)
    assert.ok(evaluate(`!!document.querySelector(${JSON.stringify(`${selector} ${tag}`)})`), `${label}: native semantics`)
    if (path === '/admin/media/files') evaluate(`document.querySelectorAll('details').forEach(panel => { panel.open = true })`)
    for (const theme of ['dark', 'light']) {
      evaluate(`document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
      for (const [width, height] of [[390, 844], [768, 1024], [1023, 900], [1024, 900], [1440, 900]]) {
        browser('set', 'viewport', String(width), String(height))
        wait(`document.getAnimations().every(a => a.effect?.getComputedTiming().iterations === Infinity || a.playState !== 'running')`)
        const state = evaluate(`(() => {
          const nav = document.querySelector(${JSON.stringify(selector)});
          const items = [...nav.querySelectorAll('a,button')];
          const active = nav.querySelector('[aria-current="page"],[aria-pressed="true"]');
          const inactive = items.find(item => item !== active);
          const style = element => {
            const css = getComputedStyle(element);
            return Object.fromEntries(['backgroundColor','color','borderRadius','fontSize','fontWeight','padding','minHeight'].map(key => [key,css[key]]));
          };
          return { overflow: document.documentElement.scrollWidth > innerWidth,
            touch: items.every(item => item.getBoundingClientRect().height >= 44),
            active: active && style(active), inactive: inactive && style(inactive) };
        })()`)
        assert.equal(state.overflow, false, `${label}/${theme}/${width}: page overflow`)
        assert.equal(state.touch, true, `${label}: 44px target`)
        assert.ok(state.active, `${label}: active state`)
        reference[theme] ??= state
        assert.deepEqual(state.active, reference[theme].active, `${label}/${theme}: active style drift`)
        if (state.inactive) assert.deepEqual(state.inactive, reference[theme].inactive, `${label}/${theme}: inactive style drift`)
        if (path === '/admin/media/files') {
          const colors = evaluate(`Array.from(document.querySelectorAll('.neon-button')).map(button => {
            const css = getComputedStyle(button); return [css.backgroundColor, css.color, css.borderColor];
          })`)
          for (const color of colors) assert.deepEqual(color, colors[0], `${theme}: file action overrides shared colors`)
        }
        if (process.env.UI_SCREENSHOT_DIR && [390, 1440].includes(width)) browser('screenshot', `${process.env.UI_SCREENSHOT_DIR}/${pages.findIndex(item => item[0] === path)}-${theme}-${width}.png`)
      }
    }
    browser('press', 'Tab')
    browser('focus', `${selector} ${tag}`)
    assert.ok(evaluate(`parseFloat(getComputedStyle(document.activeElement).outlineWidth) >= 2 && getComputedStyle(document.activeElement).outlineStyle !== 'none'`), `${label}: keyboard focus`)
  }

  // Exercise the compiled CSS itself: @apply alone does not inherit later theme selectors.
  evaluate(`(() => {
    const probe = document.createElement('section'); probe.id = 'ui-style-probe';
    probe.innerHTML = '<input class="input-field"><input class="input-base"><button class="btn-outline">标准</button><button class="neon-button">别名</button><table class="data-table"><thead><tr><th>表头</th></tr></thead><tbody><tr><td class="text-red-400"><button>长内容与行内操作应完整显示</button></td></tr></tbody></table><span class="text-red-400">错误状态</span>';
    document.body.append(probe);
  })()`)
  for (const theme of ['dark', 'light']) {
    browser('hover', '#ui-style-probe span')
    evaluate(`document.activeElement?.blur(); document.documentElement.dataset.theme=${JSON.stringify(theme)}`)
    wait(`document.querySelector('#ui-style-probe').getAnimations({subtree:true}).every(a => a.playState !== 'running')`)
    const styles = evaluate(`(() => {
      const probe = document.querySelector('#ui-style-probe');
      const style = selector => {
        const css = getComputedStyle(probe.querySelector(selector));
        return Object.fromEntries(['backgroundColor','color','borderColor','borderRadius','padding','fontSize','boxShadow'].map(key => [key,css[key]]));
      };
      return { inputs: ['.input-field','.input-base'].map(style), buttons: ['.btn-outline','.neon-button'].map(style),
        cellColor: getComputedStyle(probe.querySelector('td')).color, statusColor: getComputedStyle(probe.querySelector('span')).color,
        whiteSpace: getComputedStyle(probe.querySelector('td')).whiteSpace, overflow: getComputedStyle(probe.querySelector('td')).overflow };
    })()`)
    for (const input of styles.inputs) assert.deepEqual(input, styles.inputs[0], `${theme}: input alias drift`)
    for (const button of styles.buttons) assert.deepEqual(button, styles.buttons[0], `${theme}: button alias drift`)
    assert.equal(styles.cellColor, styles.statusColor, `${theme}: table must preserve semantic colors`)
    assert.equal(styles.whiteSpace, 'normal', 'table must not force single-line contents')
    assert.equal(styles.overflow, 'visible', 'table must not clip controls')
    for (const [action, selectors] of [['focus', ['.input-field', '.input-base']], ['hover', ['.btn-outline', '.neon-button']]]) {
      const samples = selectors.map(selector => {
        browser(action, `#ui-style-probe ${selector}`)
        wait(`document.querySelector('#ui-style-probe').getAnimations({subtree:true}).every(a => a.playState !== 'running')`)
        return evaluate(`(() => {
          const css = getComputedStyle(document.querySelector(${JSON.stringify(`#ui-style-probe ${selector}`)}));
          return [css.backgroundColor, css.color, css.borderColor, css.boxShadow];
        })()`)
      })
      assert.deepEqual(samples[0], samples[1], `${theme}: alias ${action} drift`)
    }
  }
  assert.equal(browser('errors').trim(), '', 'browser runtime errors')
  console.log('共享样式检查通过：7 处标签、链接/按钮语义、双主题、响应式、键盘焦点、旧类名与表格内容')
} finally {
  browser('close')
}
