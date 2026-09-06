// Run from web: node scripts/check-task-log.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { URL } from 'node:url'
import vm from 'node:vm'
import console from 'node:console'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('../src/pages/TasksPage.tsx', import.meta.url), 'utf8')
const exports = {}
let response = { content: 'fresh\n', date: '2026-09-06', dates: [] }
const mocks = {
  '../components/ModalShell': { ModalShell: 'section' },
  '../api/tasks': { tasksAPI: { log: async () => response } },
}
let states = [], memos = [], stateIndex = 0, memoIndex = 0
const react = {
  ...require('react'),
  useState(initial) {
    const index = stateIndex++
    if (!(index in states)) states[index] = typeof initial === 'function' ? initial() : initial
    return [states[index], (value) => { states[index] = typeof value === 'function' ? value(states[index]) : value }]
  },
  useMemo(fn, deps) {
    const index = memoIndex++
    if (!memos[index] || deps.some((value, i) => !Object.is(value, memos[index].deps[i]))) memos[index] = { deps, value: fn() }
    return memos[index].value
  },
  useEffect() {},
  useRef: () => ({ current: 0 }),
}
vm.runInNewContext(ts.transpileModule(`${source}\nexport { TaskLogDialog, reverseLogLines };`, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports, require: (id) => id === 'react' ? react : mocks[id] ?? (id.startsWith('.') ? {} : require(id)) })

function nodes(node) {
  if (Array.isArray(node)) return node.flatMap(nodes)
  if (!node || typeof node !== 'object') return []
  return [node, ...nodes(node.props?.children)]
}
function render(content, page = 1) {
  states = [{ content, date: '2026-09-07', dates: [] }, page, new Date(), false, '']
  stateIndex = memoIndex = 0
  return nodes(exports.TaskLogDialog({ definition: { key: 'library_scan', name: '扫描' }, onClose() {} }))
}
function pre(tree) { return tree.find((node) => node.type === 'pre') }
function button(tree, label) { return tree.find((node) => node.props?.['aria-label'] === label) }

assert.equal(exports.reverseLogLines('').length, 0)
assert.equal(exports.reverseLogLines('old\n\nnew\n').join('|'), 'new||old')
assert.equal(exports.reverseLogLines('old\nnew').join('|'), 'new|old')
for (const count of [1, 200, 201, 400, 401, 5000]) {
  const content = Array.from({ length: count }, (_, i) => `line-${i}`).join('\n') + '\n'
  const pages = Math.ceil(count / 200)
  const collected = []
  for (let page = 1; page <= pages; page++) {
    const tree = render(content, page)
    const rows = pre(tree).props.children
    assert.ok(rows.length <= 200)
    collected.push(...rows.map((row) => row.props.children[0]))
    assert.strictEqual(pre(render(content, page)).props.children, rows, 'unchanged parent refresh reuses rendered rows')
    if (pages > 1) {
      assert.equal(button(tree, '日志上一页').props.disabled, page === 1)
      assert.equal(button(tree, '日志下一页').props.disabled, page === pages)
      if (page < pages) { button(tree, '日志下一页').props.onClick(); assert.equal(states[1], page + 1) }
    }
  }
  assert.deepEqual(collected, Array.from({ length: count }, (_, i) => `line-${count - i - 1}`))
}
assert.equal(pre(render('')).props.children, '该任务暂无日志。')
for (const date of ['2026-09-06', '2026-09-07']) {
  response = { ...response, date }
  button(render('old\n'.repeat(401), 3), '刷新日志').props.onClick()
  await Promise.resolve()
  assert.equal(states[1], 1, 'successful reload resets pagination')
  assert.equal(states[0].date, date)
}
for (const content of ['2026-09-07 ✅ done\n', '2026-09-07 [ERROR] failed\n']) {
  assert.ok(nodes(pre(render(content))).some((node) => node.props?.role === 'img'), 'log badges remain visible')
}
console.log('Task log pagination, ordering, badges and memo reuse checks passed')
