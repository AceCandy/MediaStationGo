// Run from web: node scripts/check-season-actions.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import vm from 'node:vm'
import { URL } from 'node:url'
import console from 'node:console'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const requests = [], messages = []
let failID = '', reloads = 0
const mocks = {
  react: { useRef: value => ({ current: value }), useState: value => [value, () => {}] },
  'react-hot-toast': { default: { loading: () => 'toast', success: text => messages.push(text), error: text => messages.push(text) } },
  '../api/client': { LONG_REQUEST_TIMEOUT: 100, api: { post: async path => { requests.push(path); if (path.includes(failID) && failID) throw Error('fixture failure') } } },
  '../components/MetadataEditDialog': {},
  '../components/ModalShell': {},
}
const adminExports = {}
vm.runInNewContext(ts.transpileModule(readFileSync(new URL('../src/pages/MediaDetailAdminPanel.tsx', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports: adminExports, require })
mocks['./MediaDetailAdminPanel'] = adminExports
const exports = {}
vm.runInNewContext(ts.transpileModule(readFileSync(new URL('../src/pages/LibrarySeasonActions.tsx', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports, require: id => mocks[id] ?? require(id) })
const tree = exports.LibrarySeasonActions({ season: { metadata_id: 'season' }, mediaID: 'file', episodes: [{ metadata_id: 'episode1' }, { metadata_id: 'episode1' }, { metadata_id: 'episode2' }], onChanged: async () => { reloads++ } })
const buttons = []
function visit(node) {
  if (!node || typeof node !== 'object') return
  if (node.type === adminExports.AdminMenuItem) buttons.push(node)
  if (node.props?.role === 'menu') {
    assert.match(node.props.className, /right-0/, 'season menu remains right-aligned')
    assert.match(node.props.className, /w-64/, 'menu uses standard width')
  }
  for (const child of [node.props?.children].flat(Infinity)) visit(child)
}
visit(tree)
assert.equal(buttons.length, 3)
assert.equal(buttons[1].props.title, '刷新季资料及当前 3 集')
for (const [index, item] of buttons.entries()) {
  const button = item.type(item.props)
  assert.equal(button.props.role, 'menuitem')
  assert.match(button.props.className, /text-\[var\(--app-text\)\]/)
  assert.equal(item.props.iconClass, index < 2 ? 'text-[var(--app-gold)]' : 'text-[var(--app-muted)]')
  let clicked = false, closed = false
  item.type({ ...item.props, onClick: () => { clicked = true }, onClose: () => { closed = true } }).props.onClick({ currentTarget: {} })
  assert.ok(clicked && closed, 'shared menu item invokes action and closes menu')
}
await buttons[0].props.onClick()
assert.deepEqual(requests, ['/metadata/season/tmdb/refresh'], 'season refresh never touches episodes')
requests.length = 0
failID = 'episode1'
await buttons[1].props.onClick()
assert.deepEqual(requests, ['/metadata/season/tmdb/refresh', '/metadata/episode1/tmdb/refresh', '/metadata/episode2/tmdb/refresh'], 'whole-season refresh deduplicates versions and continues after failure')
assert.equal(reloads, 2, 'both operations reload current metadata')
assert.match(messages.at(-1), /1 项未完成/, 'partial failure is visible')
console.log('Season refresh scope, deduplication and partial failure checks passed')
