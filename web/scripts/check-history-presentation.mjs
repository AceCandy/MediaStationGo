// Run from web: node scripts/check-history-presentation.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import vm from 'node:vm'
import { URL } from 'node:url'
import console from 'node:console'
import * as React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import ts from 'typescript'

const require = createRequire(import.meta.url)
function load(file, mocks = {}) {
  const exports = {}
  vm.runInNewContext(ts.transpileModule(readFileSync(new URL(`../src/pages/${file}`, import.meta.url), 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
  }).outputText, { exports, require: id => mocks[id] ?? require(id) })
  return exports
}
const media = { id: 'file', title: '第 7 集', series_title: '测试剧名', metadata_kind: 'episode', season_num: 1, episode_num: 7, poster_url: '/api/artwork/series-poster', year: 2026, rating: 0 }
const history = [{ id: 'history', media, position_ms: 30_000, duration_ms: 120_000, watched_at: '2026-09-15T00:00:00Z' }]
const mocks = {
  '../api/client': { imageURL: url => url },
  '../api/history': {},
  '../components/confirmAction': {},
  '../components/MediaCard': { MediaCard: () => null },
  '../utils/groupSeries': { mediaDetailLink: item => `/media/${item.id}` },
  './seriesDetailModel': load('seriesDetailModel.ts'),
}
const home = load('HomePageSections.tsx', mocks)
const watch = load('WatchHistoryPage.tsx', { ...mocks, react: { ...React, useState: initial => [Array.isArray(initial) ? history : initial, () => {}] } })
for (const [Component, props] of [
  [home.ContinueWatchingSection, { history }],
  [home.HomeFeaturedSection, { featuredItem: media, featuredPoster: media.poster_url, featuredVisual: '', showDiscover: false }],
  [watch.WatchHistoryPage, { embedded: true }],
]) {
  const html = renderToStaticMarkup(React.createElement(MemoryRouter, {}, React.createElement(Component, props)))
  assert.match(html, /测试剧名/)
  assert.match(html, /第 1 季 · 第 7 集/)
  assert.match(html, /src="\/api\/artwork\/series-poster"/)
  assert.match(html, /href="\/media\/file"/)
}
const nextHome = load('HomePageSections.tsx', { ...mocks, '../utils/groupSeries': { mediaDetailLink: () => '/library/library?series_id=hg-group-100' } })
const next = renderToStaticMarkup(React.createElement(MemoryRouter, {}, React.createElement(nextHome.ContinueWatchingSection, {
  history: [{ ...history[0], media: { ...media, series_id: 'hg-group-100', library_id: 'library', catalog_source: 'hongguo' }, is_next: true, position_ms: 0, duration_ms: 0 }],
})))
assert.match(next, /接着看下一集/)
assert.doesNotMatch(next, /已观看到/)
assert.match(next, /href="\/media\/file"/)
history[0] = { ...history[0], completed: true, position_ms: 0 }
const completed = renderToStaticMarkup(React.createElement(MemoryRouter, {}, React.createElement(watch.WatchHistoryPage, { embedded: true })))
assert.match(completed, /width:100%/)
assert.match(completed, /已看完/)
history[0] = { ...history[0], position_ms: 30000 }
const replay = renderToStaticMarkup(React.createElement(MemoryRouter, {}, React.createElement(watch.WatchHistoryPage, { embedded: true })))
assert.match(replay, /width:25%/)
assert.match(replay, /已看完/)
console.log('History, continue and featured presentation checks passed')
