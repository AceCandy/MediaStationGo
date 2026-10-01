// Run from web: node scripts/check-next-episode-probe.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { URL, URLSearchParams } from 'node:url'
import console from 'node:console'
import vm from 'node:vm'
import ts from 'typescript'

const require = createRequire(import.meta.url)
function load(path, mocks = {}) {
  const exports = {}
  vm.runInNewContext(ts.transpileModule(readFileSync(new URL(path, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
  }).outputText, { exports, URLSearchParams, require: id => mocks[id] ?? require(id) })
  return exports
}
const model = load('../src/pages/seriesDetailModel.ts')
const current = { id: 's1e1', season_num: 1, episode_num: 1 }
const next = { id: 's1e2', season_num: 1, episode_num: 2 }
async function check({ episodes = [current, next], fields = {}, tracks = [], cancel = false, failure = '', playable = true } = {}) {
  const effects = []
  const reads = []
  const probes = []
  let resolveRead, rejectRead
  const read = new Promise((resolve, reject) => { resolveRead = resolve; rejectRead = reject })
  const { LibrarySeriesDetailSection } = load('../src/pages/LibrarySeriesDetailSection.tsx', {
    react: {
      useState: initial => [initial === null ? { episodeID: current.id, items: playable ? [current] : [] } : initial, () => {}],
      useEffect: effect => effects.push(effect),
    },
    'react-router-dom': { useSearchParams: () => [new URLSearchParams(`season=1&episode=${current.id}`), () => {}] },
    '../api/library': { mediaAPI: {
      listVersions: async () => [current],
      get: id => { reads.push(id); return read },
      ensureProbe: async id => { probes.push(id); if (failure === 'probe') throw new Error('unavailable') },
    } },
    './seriesDetailModel': model,
    ...Object.fromEntries(['LibrarySeriesDetailHeader', 'LibrarySeriesEpisodes', 'LibrarySeriesEpisodeDetail', 'MediaDetailCast'].map(name => [`./${name}`, { [name]: () => null }])),
  })
  const view = LibrarySeriesDetailSection({
    selectedSeries: { key: 'series', rep: current }, selectedEpisodes: [{ season: 1, episodes }],
    allEpisodes: episodes, history: [], loadingEpisodes: false, episodesError: false, ...fields,
  })
  const cleanups = effects.map(effect => effect())
  if (cancel) cleanups.forEach(cleanup => cleanup?.())
  if (failure === 'read') rejectRead(new Error('unavailable'))
  else resolveRead({ ...next, tracks })
  for (let i = 0; i < 8; i++) await Promise.resolve()
  cleanups.forEach(cleanup => cleanup?.())
  return { reads, probes, view }
}
const missing = await check()
assert.deepEqual(missing.reads, [next.id])
assert.deepEqual(missing.probes, [next.id], 'only the same-season episode +1 is probed')
assert.deepEqual((await check({ tracks: null })).probes, [next.id])
assert.deepEqual((await check({ tracks: [{ type: 'video', index: 0 }] })).probes, [], 'fresh tracks skip probing')
for (const episodes of [[current], [current, { ...next, episode_num: 3 }], [current, { ...next, season_num: 2 }], [{ ...current, episode_num: 0 }, next]]) {
  assert.deepEqual((await check({ episodes })).reads, [], 'no season crossing, gap skipping or guessing an unknown episode')
}
for (const fields of [{ selectedSeries: null }, { loadingEpisodes: true }, { episodesError: true }]) {
  assert.deepEqual((await check({ fields })).reads, [], 'only a loaded series detail starts pre-probing')
}
assert.deepEqual((await check({ playable: false })).reads, [], 'no current playable detail means no pre-probing')
assert.deepEqual((await check({ cancel: true })).probes, [], 'unmount before read completion stops probing')
assert.deepEqual((await check({ failure: 'read' })).probes, [], 'read failure does not start probing')
assert.ok((await check({ failure: 'probe' })).view, 'probe failure leaves the current detail available')
console.log('Same-season next episode probe checks passed')
