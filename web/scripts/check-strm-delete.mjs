// Run from web: node scripts/check-strm-delete.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { URL } from 'node:url'
import vm from 'node:vm'
import console from 'node:console'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('../src/components/STRMDeleteDialog.tsx', import.meta.url), 'utf8')
const exports = {}
let state = [], cursor = 0, calls = [], errors = [], deleted = 0, finish
const mocks = {
  react: {
    useState(initial) {
      const index = cursor++
      if (!(index in state)) state[index] = initial
      return [state[index], (value) => { state[index] = value }]
    },
    useRef(initial) {
      const index = cursor++
      return state[index] ??= { current: initial }
    },
  },
  '../api/library': { mediaAPI: { deleteSTRMTarget: (...args) => {
    calls.push(args)
    return new Promise((resolve, reject) => { finish = { resolve, reject } })
  } } },
  'react-hot-toast': { default: { success() {}, error(message) { errors.push(message) } } },
  './ModalShell': { ModalShell: 'dialog' },
}
vm.runInNewContext(ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports, require: (id) => mocks[id] ?? require(id) })

function render(target) {
  cursor = 0
  return exports.STRMDeleteDialog({ mediaID: 'fixture-media', target, onClose() {}, onDeleted() { deleted++ } })
}
function nodes(node) {
  if (!node || typeof node !== 'object') return []
  return [node, ...[node.props?.children].flat().flatMap(nodes)]
}
const checkbox = (tree) => nodes(tree).find((node) => node.type === 'input')
const confirm = (tree) => nodes(tree).find((node) => node.type === 'button' && node.props.className.startsWith('btn-danger'))
const pathShown = (tree, path) => nodes(tree).some((node) => node.type === 'p' && node.props.children === path)
const target = { target_path: '/fixture/season/episode.mkv', parent_path: '/fixture/season' }

for (const parent of [false, true]) {
  state = []; calls = []; deleted = 0
  let tree = render(target)
  assert.equal(checkbox(tree).props.checked, false, 'parent deletion is opt-in on every open')
  assert.ok(pathShown(tree, target.target_path))
  checkbox(tree).props.onChange({ target: { checked: parent } })
  tree = render(target)
  assert.ok(pathShown(tree, parent ? target.parent_path : target.target_path))
  const pending = confirm(tree).props.onClick()
  await confirm(tree).props.onClick()
  assert.deepEqual(calls, [['fixture-media', parent]], 'duplicate clicks send only one request with the exact media ID')
  tree = render(target)
  assert.equal(tree.props.onClose, undefined, 'cannot dismiss while deleting')
  assert.equal(checkbox(tree).props.disabled, true)
  assert.equal(confirm(tree).props.disabled, true)
  finish.resolve({ removed: true })
  await pending
  assert.equal(deleted, 1)
}

state = []; errors = []; deleted = 0
let tree = render({ target_path: target.target_path })
assert.equal(checkbox(tree).props.disabled, true, 'protected parent cannot be selected')
const pending = confirm(tree).props.onClick()
finish.reject({ response: { data: { error: '目标不可删除' } } })
await pending
assert.deepEqual(errors, ['目标不可删除'])
assert.equal(deleted, 0, 'failure keeps the dialog open')
tree = render({ target_path: target.target_path })
assert.equal(confirm(tree).props.disabled, false, 'failure allows retry')
assert.ok(tree.props.onClose)
console.log('STRM deletion checks passed')
