// Run from web: node scripts/check-series-loading.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { URL, URLSearchParams } from 'node:url'
import console from 'node:console'
import vm from 'node:vm'
import ts from 'typescript'

// Exercise the hook's effects with independently controlled API responses.
const slots = []
let cursor = 0
let dirty = true
let effects = []
let params = new URLSearchParams('series=metadata:one')
let result
const requests = []
const request = (kind, args) => new Promise((resolve, reject) => requests.push({ kind, args, resolve, reject }))
const react = {
  useState(initial) {
    const index = cursor++
    if (!(index in slots)) slots[index] = initial
    return [slots[index], value => {
      const next = typeof value === 'function' ? value(slots[index]) : value
      if (!Object.is(next, slots[index])) { slots[index] = next; dirty = true }
    }]
  },
  useRef(initial) {
    const index = cursor++
    return slots[index] ??= { current: initial }
  },
  useMemo(callback, deps) {
    const index = cursor++
    if (!slots[index] || deps.some((value, i) => !Object.is(value, slots[index].deps[i]))) {
      slots[index] = { deps, value: callback() }
    }
    return slots[index].value
  },
  useCallback(callback, deps) { return react.useMemo(() => callback, deps) },
  useEffect(callback, deps) {
    const index = cursor++
    const previous = slots[index]
    if (!previous || deps.some((value, i) => !Object.is(value, previous.deps[i]))) {
      effects.push(() => {
        previous?.cleanup?.()
        slots[index] = { deps, cleanup: callback() }
      })
    }
  },
}
const mocks = {
  react,
  'react-hot-toast': { default: { error() {} } },
  'react-router-dom': { useSearchParams: () => [params] },
  '../stores/auth': { useAuthStore: select => select({ user: { id: 'viewer' } }) },
  '../stores/playProfile': { usePlayProfileStore: select => select({ activeProfileId: 'profile' }) },
  '../api/library': { libraryAPI: Object.fromEntries(['get', 'listSeries', 'listMedia', 'listSeriesEpisodes'].map(kind => [kind, (...args) => request(kind, args)])) },
  '../utils/groupSeries': { groupSeries: () => [], isEpisodeLike: () => false },
  './librariesPageModel': { isSeriesLibraryType: type => type === 'tv' },
}
const exports = {}
vm.runInNewContext(ts.transpileModule(readFileSync(new URL('../src/pages/useLibraryData.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports, require: id => { assert.ok(id in mocks, id); return mocks[id] } })
async function flush() {
  for (let i = 0; i < 12; i++) {
    if (dirty) {
      dirty = false
      cursor = 0
      effects = []
      result = exports.useLibraryData('library', {})
      effects.forEach(effect => effect())
    }
    await Promise.resolve()
  }
  assert.equal(dirty, false, 'hook settles')
}
function navigate(query) { params = new URLSearchParams(query); dirty = true }
const count = kind => requests.filter(row => row.kind === kind).length
await flush()
assert.equal(requests.length, 1)
requests[0].resolve({ id: 'library', type: 'tv' })
await flush()
assert.equal(count('listSeries'), 1, 'detail skips the library catalogue')
assert.equal(requests[1].args[2], 1, 'only the linked card is requested')
assert.equal(count('listSeriesEpisodes'), 1, 'episodes start before the card resolves')
assert.equal(requests[2].args[1], 'metadata:one')
requests[2].resolve({ items: [{ id: 'episode' }], history: [] })
await flush()
assert.equal(result.seriesEpisodeItems[0].id, 'episode')
requests[1].resolve({ items: [{ key: 'metadata:one', rep: {} }] })
await flush()
assert.equal(result.loading, false)
assert.equal(count('listSeriesEpisodes'), 1, 'card completion does not refetch episodes')
navigate('series=metadata:one&season=2&episode=other&version=file')
await flush()
assert.equal(count('listSeriesEpisodes'), 1, 'season and version changes reuse the series')
result.reloadCurrentLibrary()
await flush()
assert.equal(count('listSeriesEpisodes'), 2, 'explicit retry refreshes episodes')
const staleEpisodes = requests.at(-1)
navigate('series_id=two')
await flush()
assert.equal(requests.at(-1).args[1], 'metadata:two', 'series_id uses canonical episode key')
staleEpisodes.resolve({ items: [{ id: 'stale' }] })
await flush()
assert.equal(result.seriesEpisodeItems.length, 0, 'old series response is ignored')
requests.at(-1).reject(new Error('unavailable'))
await flush()
assert.equal(result.seriesEpisodesError, true)
navigate('')
await flush()
assert.equal(requests.at(-1).kind, 'listSeries')
assert.equal(requests.at(-1).args[2], 50, 'returning to the library restores pagination')
requests.at(-1).resolve({ items: [], total: 0 })
await flush()
assert.equal(result.loading, false)
console.log('Series detail parallel loading, navigation, retry and stale-response checks passed')
