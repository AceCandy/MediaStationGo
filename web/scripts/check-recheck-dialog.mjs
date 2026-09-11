// Run from web: node scripts/check-recheck-dialog.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { URL } from 'node:url'
import console from 'node:console'
import ts from 'typescript'

const slots = [], requests = [], fileRequests = [], previews = []
let cursor = 0, effects = [], tree
const react = {
  useState(initial) {
    const i = cursor++
    if (!(i in slots)) slots[i] = initial
    return [slots[i], value => { slots[i] = typeof value === 'function' ? value(slots[i]) : value }]
  },
  useRef(initial) {
    const i = cursor++
    if (!(i in slots)) slots[i] = { current: initial }
    return slots[i]
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
assert.equal(source.match(/min-h-0 flex-1 space-y-4 overflow-y-auto/g)?.length, 1, 'files share the recheck list scroll body')
assert.doesNotMatch(source, /ariaLabel="复查关联文件"/, 'delete does not open a file-selection modal')
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
vm.runInNewContext(ts.transpileModule(source + '\nexports.TMDbRecheckFiles = TMDbRecheckFiles', {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText, { exports, AbortController: globalThis.AbortController, require(id) {
  if (id === 'react') return react
  if (id === 'react/jsx-runtime') return { jsx: (type, props) => ({ type, props }), jsxs: (type, props) => ({ type, props }) }
  if (id === '../api/tasks') return { tasksAPI: {
    rechecks: (status, page, signal) => new Promise(resolve => requests.push({ status, page, signal, resolve })),
    recheckFiles: (id, page, signal) => new Promise(resolve => fileRequests.push({ id, page, signal, resolve })),
  } }
  if (id === '../api/library') return { mediaAPI: { getSTRMDeleteTarget: id => new Promise((resolve, reject) => previews.push({ id, resolve, reject })) } }
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
  if (Array.isArray(node)) return node.flatMap(child => nodes(child ?? null))
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
click('上游未收录 / 待核对'); await render()
assert.equal(requests[0].signal.aborted, true)
assert.equal(requests[1].status, 'not_found')
requests[1].resolve({ items: [{ metadata_id: 'episode-1', status: 'not_found', season_num: 1, episode_num: 1 }], counts: {}, total: 21, page_size: 20, changes: 0 }); await render()
const scrollAreas = nodes().filter(n => /overflow-y-auto/.test(n.props?.className ?? ''))
assert.equal(scrollAreas.length, 1, 'only the lower list scrolls')
const scrollingNodes = nodes(scrollAreas[0])
assert.ok(scrollingNodes.some(n => n.type === 'article'), 'media rows are inside the scroll area')
assert.ok(!scrollingNodes.some(n => n.type === 'form' || n.props?.role === 'group'), 'search and tabs stay outside the scroll area')
const controls = nodes().find(n => n.props?.className === 'shrink-0 space-y-4 p-5 pb-0')
assert.ok(nodes(controls).some(n => n.type === 'form'), 'fixed controls contain search')
assert.ok(nodes(controls).some(n => Array.isArray(n.props?.children) && n.props.children.includes(' · 待归并变更 ')), 'counts stay in the fixed controls')
const inlineFiles = nodes().find(n => n.type === exports.TMDbRecheckFiles)
assert.ok(inlineFiles, '404 files render directly inside the pending list')
inlineFiles.props.onTarget({ id: 'media-2', value: { target_path: '/local/second.mkv' } }); await render()
const confirmation = nodes().find(n => n.props?.mediaID === 'media-2')
assert.ok(confirmation, 'selected media goes directly to delete confirmation')
assert.equal(confirmation.props.target.target_path, '/local/second.mkv')
assert.equal(nodes().find(n => n.props?.ariaLabel === '季/集复查待办').props.onClose, undefined, 'Escape cannot close the list behind confirmation')
confirmation.props.onClose(); await render()
assert.ok(!nodes().some(n => n.props?.mediaID), 'cancelling dismisses confirmation')
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
slots.length = 0
let selected = null
const pending = { current: false }
async function renderFiles() {
  await Promise.resolve(); await Promise.resolve()
  cursor = 0; effects = []
  tree = exports.TMDbRecheckFiles({ item: { metadata_id: 'episode-1' }, busy: false, pending, onTarget: value => { selected = value } })
  effects.forEach(run => run())
}
await renderFiles()
assert.equal(fileRequests[0].id, 'episode-1', 'inline files load without clicking delete')
fileRequests[0].resolve({ items: [
  { media_id: 'media-1', path: '/strm/first.strm', can_preview: false },
  { media_id: 'media-2', path: '/strm/second.strm', can_preview: true },
], has_more: true }); await renderFiles()
const fileRows = nodes().filter(n => n.type === 'li')
assert.equal(fileRows.length, 2, 'each media gets its own visible row')
assert.equal(nodes(fileRows[0]).find(n => n.type === 'button').props.disabled, true, 'unsupported media cannot be deleted')
const deleteButton = nodes(fileRows[1]).find(n => n.type === 'button')
pending.current = true
deleteButton.props.onClick()
assert.equal(previews.length, 0, 'another row resolving a path blocks concurrent previews')
pending.current = false
deleteButton.props.onClick(); deleteButton.props.onClick(); await renderFiles()
assert.equal(previews.length, 1, 'repeated clicks cannot start duplicate previews')
assert.equal(previews[0].id, 'media-2', 'delete resolves the clicked media only')
assert.equal(selected, null, 'confirmation waits for safe path resolution')
previews[0].resolve({ target_path: '/local/second.mkv' }); await renderFiles()
assert.equal(selected.id, 'media-2')
assert.equal(selected.value.target_path, '/local/second.mkv')
selected = null
deleteButton.props.onClick(); await renderFiles()
previews[1].reject(new Error('untrusted path')); await renderFiles()
assert.equal(selected, null, 'failed path resolution cannot open confirmation')
assert.ok(nodes().some(n => n.props?.role === 'alert'))
click('下一页'); await renderFiles()
assert.equal(fileRequests[1].page, 2, 'additional media remain paginated')
slots.forEach(slot => slot?.cleanup?.())
assert.equal(fileRequests[1].signal.aborted, true, 'unmount cancels file loading')
console.log('待办弹窗：搜索、分类、分页、文件平铺、删除目标、取消与失败保护检查通过')
