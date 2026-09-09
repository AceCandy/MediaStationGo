// Run: node web/scripts/check-douban-binding.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { URL } from 'node:url'
import console from 'node:console'
import ts from 'typescript'

const slots = [], searches = [], bindings = [], events = []
let cursor = 0, effects = [], tree
const react = {
  useState(initial) {
    const i = cursor++
    if (!(i in slots)) slots[i] = initial
    return [slots[i], value => { slots[i] = typeof value === 'function' ? value(slots[i]) : value }]
  },
  useRef(value) {
    const i = cursor++
    return slots[i] ??= { current: value }
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
const source = readFileSync(new URL('../src/components/DoubanBindingDialog.tsx', import.meta.url), 'utf8')
vm.runInNewContext(ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText, { exports, AbortController: globalThis.AbortController, require(id) {
  if (id === 'react') return react
  if (id === 'react/jsx-runtime') return { jsx: (type, props) => ({ type, props }), jsxs: (type, props) => ({ type, props }) }
  if (id === 'react-hot-toast') return { default: { success() {}, error() {} } }
  if (id === '../api/library') return { mediaAPI: {
    searchDoubanBinding: (metadataID, query, signal) => new Promise(resolve => searches.push({ metadataID, query, signal, resolve })),
    bindDouban: (metadataID, payload) => new Promise((resolve, reject) => bindings.push({ metadataID, payload, resolve, reject })),
  } }
  if (id === './ManualScrapeDialogModel') return { candidateKey: item => item.douban_id }
  return Object.fromEntries(['ModalShell', 'ConfirmDialog', 'ManualScrapeCandidateList'].map(name => [name, name]))
} })
async function render() {
  for (let i = 0; i < 8; i++) await Promise.resolve()
  cursor = 0; effects = []
  tree = exports.DoubanBindingDialog({ media: { metadata_id: 'series-id', metadata_kind: 'series', title: '当前剧名' }, onBound() { events.push('refresh') }, onClose() { events.push('close') } })
  effects.forEach(run => run())
}
function nodes(node) {
  if (!node || typeof node !== 'object') return []
  if (Array.isArray(node)) return node.flatMap(nodes)
  return [node, ...nodes(node.props?.children ?? null)]
}
const node = type => nodes(tree).find(n => n.type === type)
const movie = { title: '豆瓣电影', douban_id: '123', media_type: 'movie' }
const tv = { title: '豆瓣电视剧', douban_id: '456', media_type: 'tv' }
await render()
assert.equal(searches[0].query, '当前剧名')
searches[0].resolve([movie, tv]); await render()
assert.equal(node('ManualScrapeCandidateList').props.requireDoubanType, true)
node('ManualScrapeCandidateList').props.onApply({ douban_id: '789' }); await render()
assert.equal(bindings.length, 0, 'unknown type is rejected')
node('ManualScrapeCandidateList').props.onApply(movie); await render()
assert.equal(bindings.length, 0, 'cross-type apply requires confirmation')
node('ConfirmDialog').props.onClose(false); await render()
assert.equal(bindings.length, 0, 'cancel must not bind')
node('ManualScrapeCandidateList').props.onApply(movie); await render()
node('ConfirmDialog').props.onClose(true); await render()
assert.equal(bindings[0].payload.force, true)
assert.equal(bindings[0].payload.media_type, 'movie')
assert.equal(node('ModalShell').props.onClose, undefined)
node('ManualScrapeCandidateList').props.onApply(tv)
assert.equal(bindings.length, 1, 'pending request prevents double apply')
bindings[0].reject({ response: { data: { error: '保存失败' } } }); await render()
assert.ok(nodes(tree).some(n => n.props?.role === 'alert' && n.props.children === '保存失败'))
assert.equal(events.length, 0, 'failure keeps dialog open')
node('ManualScrapeCandidateList').props.onApply(tv); await render()
assert.equal(bindings[1].payload.force, false)
bindings[1].resolve({ status: 'complete' }); await render()
assert.deepEqual(events, ['refresh', 'close'])
node('form').props.onSubmit({ preventDefault() {} }); await render()
assert.equal(searches[0].signal.aborted, true)
slots.forEach(slot => slot?.cleanup?.())
assert.equal(searches[1].signal.aborted, true, 'unmount cancels search')
console.log('豆瓣绑定：自动搜索、类型确认、取消、强制、失败保留、防重复、刷新和取消请求检查通过')
