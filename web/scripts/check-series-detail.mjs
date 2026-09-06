// Run from web: node scripts/check-series-detail.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { URL, URLSearchParams } from 'node:url'
import console from 'node:console'
import ts from 'typescript'

const exports = {}
vm.runInNewContext(ts.transpileModule(readFileSync(new URL('../src/pages/seriesDetailModel.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports })
const { distinctEpisodes, resolveSeriesSelection, seriesResumeEpisode, episodeLabel } = exports
assert.equal(episodeLabel({ metadata_kind: 'series', episode_num: 0 }), '整剧关联文件')
assert.equal(episodeLabel({ metadata_kind: 'season', episode_num: 0 }), '季关联文件')
assert.equal(episodeLabel({ episode_num: 0 }), '未识别集号')
assert.equal(episodeLabel({ metadata_kind: 'episode', season_num: 0, episode_num: 1 }), '第 1 集')
const ep = (id, metadata_id, season_num, episode_num) => ({ id, metadata_id, season_num, episode_num })
const special = ep('special-file', 'special', 0, 1)
const first = ep('first-file', 'first', 1, 1)
const alternate = { ...first, id: 'first-alternate' }
const second = ep('second-file', 'second', 1, 2)
const items = [special, first, alternate, second]
assert.equal(distinctEpisodes(items).length, 3)
assert.equal(items.length, 4, 'whole-series actions retain all files')
const seasons = [{ season: 0, episodes: [special] }, { season: 1, episodes: [first, second] }]
assert.equal(resolveSeriesSelection(seasons, new URLSearchParams()).episode.id, first.id)
assert.equal(resolveSeriesSelection(seasons, new URLSearchParams('season=0')).episode.id, special.id)
assert.equal(resolveSeriesSelection(seasons, new URLSearchParams('season=-1&episode=missing')).episode.id, first.id)
assert.equal(resolveSeriesSelection(seasons, new URLSearchParams('season=1&episode=second')).episode.id, second.id)
assert.equal(resolveSeriesSelection([], new URLSearchParams()).episode, undefined)
assert.equal(seriesResumeEpisode(items, []).metadata_id, first.metadata_id)
assert.equal(seriesResumeEpisode(items, [{ metadata_id: 'first', media_id: alternate.id, completed: false }]).id, alternate.id)
assert.equal(seriesResumeEpisode(items, [{ metadata_id: 'first', completed: true }]).id, second.id)
assert.equal(seriesResumeEpisode(items, [{ metadata_id: 'hidden', completed: false }]).metadata_id, first.metadata_id)
assert.equal(seriesResumeEpisode([], []), undefined)
console.log('Series selection, specials, version grouping and resume checks passed')
