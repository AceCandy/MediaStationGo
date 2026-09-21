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
const presentation = exports.episodePresentation
assert.equal(presentation({ title: '电影', metadata_kind: 'movie' }).subtitle, '')
const sample = { title: '第 7 集', series_title: '测试剧', metadata_kind: 'episode', season_num: 1, episode_num: 7 }
assert.equal(presentation(sample).title, '测试剧')
assert.equal(presentation(sample).subtitle, '第 1 季 · 第 7 集')
assert.equal(presentation({ ...sample, title: '重逢' }).subtitle, '第 1 季 · 第 7 集 · 重逢')
assert.equal(presentation({ ...sample, title: '第七集', season_num: 0 }).subtitle, '特别篇 · 第 7 集')
assert.equal(presentation({ ...sample, series_title: '' }).title, '第 7 集')
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
assert.equal(seriesResumeEpisode(items, [{ metadata_id: 'first', media_id: alternate.id, completed: true, position_ms: 120000 }]).id, alternate.id)
assert.equal(seriesResumeEpisode(items, [{ metadata_id: 'hidden', completed: false }]).metadata_id, first.metadata_id)
assert.equal(seriesResumeEpisode([], []), undefined)
console.log('Series selection, specials, version grouping and resume checks passed')
