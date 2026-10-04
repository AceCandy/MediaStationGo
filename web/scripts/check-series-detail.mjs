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
const render = (type, props) => ({ type, props })
const header = {}
let headerData = null
let stateIndex = 0
let refreshedData = null
let refreshPending = false
let refreshRequest
let reloadSeries = async () => { throw new Error('unexpected series reload') }
const refreshToasts = []
vm.runInNewContext(ts.transpileModule(readFileSync(new URL('../src/pages/LibrarySeriesDetailHeader.tsx', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText, {
  exports: header,
  URL,
  window: { location: { origin: 'http://localhost' } },
  require: id => ({
    react: { useState: value => {
      const index = stateIndex++
      return [index === 0 ? headerData : value, next => {
        if (index === 0) refreshedData = next
        if (index === 3) refreshPending = next
      }]
    }, useEffect: () => {}, useRef: value => ({ current: value }) },
    'react/jsx-runtime': { jsx: render, jsxs: render },
    'react-router-dom': { Link: 'Link' },
    'lucide-react': { ArrowLeft: 'Icon', Play: 'Icon' },
    'react-hot-toast': { default: { loading: () => 'refresh-toast', dismiss() {}, success: message => refreshToasts.push(message), error: message => refreshToasts.push(message) } },
    '../api/client': { api: { post: (...args) => refreshRequest(...args) }, LONG_REQUEST_TIMEOUT: 120000 },
    '../api/library': { mediaAPI: { series: (...args) => reloadSeries(...args) } },
    '../api/playback': { playbackAPI: {} },
    '../components/ModalShell': { ModalShell: 'Modal' },
    '../utils/groupSeries': { seriesTitle: () => '测试剧' },
    './MediaDetailArtwork': { MediaDetailBackdrop: 'Backdrop', MediaDetailPoster: 'Poster' },
    './MediaDetailMetadata': { MediaDetailMetadata: 'Metadata' },
    './MediaDetailAdminPanel': { MediaDetailAdminMenu: 'AdminMenu' },
    './seriesDetailModel': exports,
  })[id],
})
const next = ep('s2e1-file', 's2e1', 2, 1)
const tree = header.LibrarySeriesDetailHeader({ series: { rep: first, count: 3 }, allEpisodes: [first, second], history: [], resume: { media: next, is_next: true, position_ms: 0 }, playbackFrom: '/library/test?season=1', isAdmin: false })
const links = []
function collect(node) {
  if (!node || typeof node !== 'object') return
  if (Array.isArray(node)) return node.forEach(collect)
  if (node.type === 'Link') links.push(node)
  collect(node.props?.children)
}
collect(tree)
assert.equal(links[0].props.to, '/play/s2e1-file', 'completed season continues at the next season')
assert.match(links[0].props.state.from, /season=2&episode=s2e1&version=s2e1-file/)
assert.match(JSON.stringify(links[0].props.children), /S2 E1/)
const headerProps = { series: { rep: first, count: 3 }, allEpisodes: [first], history: [], resume: null, playbackFrom: '/library/test', isAdmin: true, seriesToolBusy: '' }
function adminMenu(overrides = {}) {
  stateIndex = 0
  const tree = header.LibrarySeriesDetailHeader({ ...headerProps, ...overrides })
  const nodes = []
  function visit(node) {
    if (!node || typeof node !== 'object') return
    if (Array.isArray(node)) return node.forEach(visit)
    if (node.type === 'AdminMenu') nodes.push(node)
    visit(node.props?.children)
    if (node.type === 'Metadata') visit(node.props.actions)
  }
  visit(tree)
  return nodes[0]?.props
}
assert.equal(adminMenu().onTMDbRefresh, undefined, 'do not refresh before canonical series is loaded')
headerData = { series: { metadata_id: 'series-metadata', title: 'Original title' }, favourite: true }
assert.equal(adminMenu({ isAdmin: false }), undefined, 'viewer has no management menu')
for (const catalog_source of ['hongguo', 'nfo']) {
  assert.equal(adminMenu({ series: { rep: { ...first, catalog_source }, count: 3 } }).onTMDbRefresh, undefined, 'independent catalogs do not refresh shared TMDb metadata')
}
let finishRefresh
const posts = []
refreshRequest = (...args) => { posts.push(args); return new Promise(resolve => { finishRefresh = resolve }) }
const localized = { series: { metadata_id: 'series-metadata', title: '中文剧名', overview: '中文简介' }, favourite: true }
const reloadIDs = []
reloadSeries = async id => { reloadIDs.push(id); return localized }
const refresh = adminMenu().onTMDbRefresh
assert.equal(typeof refresh, 'function', 'whole-series menu exposes TMDb refresh')
const pendingRefresh = refresh()
assert.equal(refreshPending, true, 'refresh displays pending state')
await refresh()
assert.equal(posts.length, 1, 'duplicate refresh clicks send one request')
assert.equal(posts[0][0], '/metadata/series-metadata/tmdb/refresh', 'refresh targets canonical Series rather than Episode/file')
assert.equal(posts[0][2].timeout, 120000)
finishRefresh()
await pendingRefresh
assert.deepEqual(reloadIDs, [first.id], 'reload uses visible file ID')
assert.equal(refreshedData, localized, 'localized title and synopsis replace header data')
assert.equal(refreshPending, false)
assert.match(refreshToasts.at(-1), /已刷新/)
refreshedData = null
refreshRequest = async () => { throw { response: { data: { error: 'TMDB 暂时不可用' } } } }
await adminMenu().onTMDbRefresh()
assert.equal(refreshedData, null, 'failed refresh preserves displayed data')
assert.equal(refreshPending, false, 'failed refresh allows retry')
assert.equal(refreshToasts.at(-1), 'TMDB 暂时不可用')
console.log('Series selection, specials, version grouping and resume checks passed')
