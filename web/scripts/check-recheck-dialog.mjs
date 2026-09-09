// Run from web: node scripts/check-recheck-dialog.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { URL } from 'node:url'
import console from 'node:console'
import ts from 'typescript'

const slots = [], requests = []
let cursor = 0, effects = [], tree
const react = {
  useState(initial) {
    const i = cursor++
    if (!(i in slots)) slots[i] = initial
    return [slots[i], value => { slots[i] = typeof value === 'function' ? value(slots[i]) : value }]
  },
  useEffect(callback, deps) {
    const i = cursor++, previous = slots[i]
    if (!previous || deps.some((v, j) => v !== previous.deps[j])) effects.push(() => {
      previous?.cleanup?.()
      slots[i] = { deps, cleanup: callback() }
    })
  },
}
const exports = {}
const source = readFileSync(new URL('../src/pages/TMDbRecheckPanel.tsx', import.meta.url), 'utf8')
const tasksPageSource = readFileSync(new URL('../src/pages/TasksPage.tsx', import.meta.url), 'utf8')
const tasksAPISource = readFileSync(new URL('../src/api/tasks.ts', import.meta.url), 'utf8')
const libraryAPISource = readFileSync(new URL('../src/api/library.ts', import.meta.url), 'utf8')
assert.equal(source.match(/min-h-0 flex-1 space-y-4 overflow-y-auto/g)?.length, 2, 'both recheck dialogs have bounded scroll bodies')
assert.match(source, /className="btn-danger shrink-0 px-3 py-2 text-xs"/, '404 cleanup uses the shared destructive button style')
assert.match(source, /<Trash2 size=\{14\} aria-hidden="true" \/> 删除<\/button>/, '404 cleanup is presented as delete')
assert.match(source, /placeholder="搜索剧名、标题、S\/E 或 ID"/, 'recheck dialog exposes keyword search')
assert.match(source, /controller\.signal, keyword/, 'recheck search is sent to the server')
assert.equal(tasksPageSource.match(/<TaskPendingButton definition=\{definition\}/g)?.length, 2, 'desktop and mobile task names expose pending buttons')
assert.match(tasksPageSource, /definition\.key === 'tmdb_episode_metadata_recheck'[\s\S]*definition\.action === 'media_scrape'/, 'both pending task kinds share the task-name button')
assert.doesNotMatch(tasksPageSource.slice(tasksPageSource.indexOf('function TaskActions'), tasksPageSource.indexOf('function TaskPendingButton')), /查看季\/集复查待办/, 'pending button is not rendered in task actions')
assert.match(tasksPageSource, /pendingDefinition\?\.action === 'media_scrape' && <ScrapeIssuesPanel/, 'scrape issues mount only after opening their task button')
assert.match(tasksPageSource, /ariaLabel="媒体入库刮削待处理"[\s\S]*max-h-\[86vh\][\s\S]*onClose=\{manualTarget \|\| deleteTarget \? undefined : onClose\}/, 'scrape issues use a bounded modal that protects nested dialogs')
assert.match(tasksPageSource, /placeholder="搜索标题、路径、媒体库或原因"/, 'scrape issues expose keyword search')
assert.match(tasksPageSource, /border-gold-500\/30[\s\S]*count > 999 \? '999\+' : count/, 'non-empty pending buttons use the gold count marker')
assert.doesNotMatch(tasksPageSource, /setInterval\([^)]*refreshPendingCounts/, 'pending counts do not follow the three-second task polling loop')
assert.match(tasksAPISource, /keyword: keyword \|\| undefined/, 'recheck API forwards keyword')
assert.match(libraryAPISource, /keyword: options\.keyword \|\| undefined/, 'scrape issues API forwards keyword')
vm.runInNewContext(ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText, { exports, AbortController: globalThis.AbortController, require(id) {
  if (id === 'react') return react
  if (id === 'react/jsx-runtime') return { jsx: (type, props) => ({ type, props }), jsxs: (type, props) => ({ type, props }) }
  if (id === '../api/tasks') return { tasksAPI: { rechecks: (status, page, signal) => new Promise(resolve => requests.push({ status, page, signal, resolve })) } }
  return {}
} })
async function render() {
  await Promise.resolve(); await Promise.resolve()
  cursor = 0; effects = []
  tree = exports.TMDbRecheckPanel({ onClose() {} })
  effects.forEach(run => run())
}
function nodes(node = tree) {
  if (!node || typeof node !== 'object') return []
  if (Array.isArray(node)) return node.flatMap(nodes)
  return [node, ...nodes(node.props?.children ?? null)]
}
function click(text) {
  const button = nodes().find(n => n.type === 'button' && (n.props.children === text || n.props['aria-label'] === text))
  assert.ok(button, text); button.props.onClick()
}
await render()
assert.equal(requests[0].status, 'pending')
const dialog = nodes().find(n => n.props?.ariaLabel === '季/集复查待办')
assert.match(dialog.props.className, /max-h-\[86vh\]/)
assert.ok(nodes().some(n => /min-h-0.*overflow-y-auto/.test(n.props?.className ?? '')), 'dialog body shrinks and scrolls inside the viewport')
click('上游未找到（404）'); await render()
assert.equal(requests[0].signal.aborted, true)
assert.equal(requests[1].status, 'not_found')
requests[1].resolve({ items: [], counts: {}, total: 21, page_size: 20, changes: 0 }); await render()
click('下一页'); await render()
assert.equal(requests[2].page, 2)
click('复查待办'); await render()
assert.equal(requests[3].page, 1)
assert.equal(requests[3].status, 'pending')
requests[2].resolve({ items: [], counts: {}, total: 99, page_size: 20, changes: 0 }); await render()
assert.ok(nodes().some(n => n.props?.role === 'status'), 'stale response must not replace loading')
requests[3].resolve({ items: [], counts: {}, total: 0, page_size: 20, changes: 0 }); await render()
click('复查待办'); await render()
assert.equal(requests.length, 4, 'same tab does not reset or refetch')
click('刷新复查待办'); await render()
assert.equal(requests.length, 5)
slots.forEach(slot => slot?.cleanup?.())
assert.equal(requests[4].signal.aborted, true, 'closing cancels request')
console.log('待办弹窗：搜索、数量提示、分类、分页、刷新与取消检查通过')
