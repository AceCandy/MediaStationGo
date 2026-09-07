// Run from web: node scripts/check-series-presentation.mjs
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { URL } from 'node:url'
import vm from 'node:vm'
import console from 'node:console'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import ts from 'typescript'

const require = createRequire(import.meta.url)
function load(name, mocks, exportName = name) {
  const exports = {}
  const source = readFileSync(new URL(`../src/pages/${name}.tsx`, import.meta.url), 'utf8')
  vm.runInNewContext(ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ES2022 },
  }).outputText, { exports, URL, window: { location: { origin: 'http://fixture.local' } }, require: (id) => mocks[id] ?? require(id) })
  return exports[exportName]
}

const Metadata = load('MediaDetailMetadata', { '../api/library': {}, '../components/STRMDeleteDialog': {} })
const AdminMenu = load('MediaDetailAdminPanel', {}, 'MediaDetailAdminMenu')
const menuProps = { tmdbRefreshPending: false, doubanEnrichmentPending: false, doubanDegraded: false, onMetadataEdit() {}, onProbe() {}, onSoftDelete() {} }
const seriesMenu = renderToStaticMarkup(createElement(AdminMenu, { ...menuProps, label: '整剧更多操作' }))
assert.doesNotMatch(seriesMenu, /整理入库|刷新tmdb信息|整剧智能刮削/, 'series menu omits scraping and organizing')
assert.match(seriesMenu, /编辑元数据/)
assert.match(seriesMenu, /强制探测媒体轨/)
assert.match(seriesMenu, /永久删除/)
const fileMenu = renderToStaticMarkup(createElement(AdminMenu, { ...menuProps, onTMDbRefresh() {} }))
assert.match(fileMenu, /刷新tmdb信息/, 'file refresh remains available')
assert.doesNotMatch(fileMenu, /整理入库/, 'movie and episode menus omit organizing')
const Tracks = load('MediaDetailTracks', { './libraryPageModel': { formatSize: String } })
const media = { id: 'file', title: 'Selected episode', overview: 'Synopsis sentinel', path: '/fixture/video.mkv', rating: 8, duration_sec: 1200, size_bytes: 1024, tracks: [
  { index: 0, type: 'audio', language: 'zh', codec: 'aac', is_default: true },
  { index: 1, type: 'audio', language: 'en', codec: 'aac' },
] }
for (const scope of ['series', 'episode']) {
  const html = renderToStaticMarkup(createElement(Metadata, { media, selectedMedia: scope === 'episode' ? media : undefined, scope, isAdmin: false, actions: createElement('button', {}, 'Play sentinel') }))
  assert.ok(html.includes('Synopsis sentinel'), 'synopsis is rendered')
  assert.ok(html.indexOf('Play sentinel') < html.indexOf('Synopsis sentinel'), 'playback precedes synopsis')
  assert.doesNotMatch(html, /<(details|summary)\b/, 'series and episode metadata need no disclosure')
  if (scope === 'episode') assert.ok(html.includes(media.path), 'selected file path is rendered')
}
const movie = renderToStaticMarkup(createElement(Metadata, { media, selectedMedia: media, isAdmin: false }))
assert.doesNotMatch(movie, /<details/, 'movie metadata stays expanded')
for (const kind of ['movie', 'series', 'season', 'episode']) {
  const html = renderToStaticMarkup(createElement(Metadata, {
    media: { ...media, metadata_kind: kind }, scope: kind === 'series' ? 'series' : undefined,
    isAdmin: false, onToggleFavourite() {},
  }))
  assert.equal(html.includes('aria-label="加入收藏"'), kind === 'movie' || kind === 'series', `${kind} favorite control`)
}
for (const scope of [undefined, 'series', 'episode']) {
  const html = renderToStaticMarkup(createElement(Metadata, { media: { ...media, douban_id: 'fixture-douban', tmdb_id: 1, series_tmdb_id: 2, metadata_kind: scope ?? 'movie', season_num: 1, episode_num: 1 }, scope, isAdmin: true }))
  if (scope === 'episode') {
    assert.doesNotMatch(html, /badge-gold|douban.svg|fixture-douban/, 'episode has neither rating nor Douban association')
    assert.match(html, /\/tv\/2\/season\/1\/episode\/1/, 'episode TMDb link remains')
  } else {
    assert.match(html, /badge-gold/, 'movie and series ratings remain')
    assert.match(html, /fixture-douban/, 'movie and series Douban association remains')
  }
}
const standaloneEpisode = renderToStaticMarkup(createElement(Metadata, { media: { ...media, metadata_kind: 'episode' }, isAdmin: true }))
assert.doesNotMatch(standaloneEpisode, /badge-gold|douban.svg/, 'standalone episode uses the same metadata rules')
const placeholder = { ...media, metadata_kind: 'episode', tmdb_id: 0, series_tmdb_id: 2, season_num: 1, episode_num: 20, tmdb_status: 'missing', tmdb_snapshot: false }
for (const scope of [undefined, 'episode']) {
  const missing = renderToStaticMarkup(createElement(Metadata, { media: placeholder, scope, isAdmin: false }))
  assert.match(missing, /border-red-500/, 'missing episode has a red provider border')
  assert.match(missing, /未获取到 TMDB 单集信息/)
  assert.match(missing, /可能尚未收录或分集编号不同/)
  assert.doesNotMatch(missing, /href="https:\/\/www.themoviedb.org/, 'placeholder does not invent a provider link')
  for (const status of ['partial', 'complete']) {
    const recovered = renderToStaticMarkup(createElement(Metadata, { media: { ...placeholder, tmdb_id: 120, tmdb_snapshot: true, tmdb_status: status }, scope, isAdmin: false }))
    assert.doesNotMatch(recovered, /border-red-500|未获取到 TMDB 单集信息/, 'successful recheck clears red warning even without artwork')
    assert.match(recovered, /\/tv\/2\/season\/1\/episode\/20/)
  }
}
assert.doesNotMatch(standaloneEpisode, /未获取到 TMDB 单集信息/, 'non-TMDb episodes are not mislabeled')
const props = { media, versions: [media], selectedVersionID: media.id, loading: false, probing: false, probeError: '', onVersionChange() {} }
const tracks = renderToStaticMarkup(createElement(Tracks, { ...props, readOnlyTracks: true }))
assert.match(tracks, /媒体信息/)
assert.equal((tracks.match(/relative flex min-h-12/g) ?? []).length, 4, 'version and all three track types share the movie row shell')
assert.doesNotMatch(tracks, /<(details|summary)\b/, 'single-version tracks need no disclosure')
assert.match(tracks, /实际音轨与字幕请在播放器中选择/)
assert.match(tracks, /zh/)
assert.match(tracks, /en/)
assert.doesNotMatch(tracks, /aria-label="音频选项"/, 'informational tracks are not playback selectors')
const multipleVersions = renderToStaticMarkup(createElement(Tracks, { ...props, versions: [media, { ...media, id: 'alternate-file' }], readOnlyTracks: true }))
assert.match(multipleVersions, /aria-label="版本选项"/, 'multiple versions remain selectable')
assert.equal((multipleVersions.match(/<details\b/g) ?? []).length, 1, 'only the version chooser uses disclosure')
assert.match(renderToStaticMarkup(createElement(Tracks, props)), /aria-label="音频选项"/, 'movie track presentation is preserved')
const loading = renderToStaticMarkup(createElement(Tracks, { ...props, media: null, loading: true, readOnlyTracks: true }))
const subtitles = renderToStaticMarkup(createElement(Tracks, { ...props, media: { ...media, tracks: [{ index: 2, type: 'subtitle', language: 'zh', is_default: true }, { index: 3, type: 'subtitle', language: 'ja' }] }, readOnlyTracks: true }))
assert.match(subtitles, /aria-label="字幕选项"/, 'subtitle information uses the compact movie chooser')
assert.equal((subtitles.match(/<details\b/g) ?? []).length, 1, 'only subtitles need a chooser with one version')
assert.match(loading, /正在加载媒体信息…/)
assert.doesNotMatch(loading, /暂无轨道/, 'loading is not an empty result')
const router = { useNavigate: () => () => {}, Link: ({ to, children, className, 'aria-label': label }) => createElement('a', { className, 'aria-label': label, href: to }, children) }
const model = { episodeIdentity: (item) => item.metadata_id || item.id, episodeLabel: (item) => `第 ${item.episode_num} 集` }
const client = { imageURL: (url) => url }
const Card = load('../components/MediaCard', { 'react-router-dom': router, '../api/client': client, '../utils/groupSeries': { mediaDetailLink: () => '/media/movie' } }, 'MediaCard')
for (const [fields, supported] of [[{}, true], [{ metadata_kind: 'movie' }, true], [{ series_id: 'series' }, false], [{ metadata_kind: 'episode' }, false], [{ metadata_kind: 'season' }, false]]) {
  const html = renderToStaticMarkup(createElement(Card, { media: { ...media, ...fields }, onToggleFavourite() {} }))
  assert.equal(html.includes('aria-label="加入收藏"'), supported, 'grouped movies retain favorites; series and episodes use no file-level favorite control')
}
let episodeMedia = { ...media, episode_num: 1, season_num: 1, backdrop_url: '/episode-still.jpg' }
const EpisodeDetail = load('LibrarySeriesEpisodeDetail', {
  'react-router-dom': router,
  '../api/client': client,
  '../components/ExternalPlayerButton': { ExternalPlayerButton: () => null },
  '../components/ModalShell': {},
  './MediaDetailAdminPanel': { MediaDetailAdminMenu: AdminMenu },
  './MediaDetailMetadata': { MediaDetailMetadata: Metadata },
  './MediaDetailTracks': { MediaDetailTracks: Tracks },
  './MediaDetailPageSections': { MediaDetailDialogs: () => null },
  './useMediaDetailPageState': { useMediaDetailPageState: () => ({ media: episodeMedia }) },
  './seriesDetailModel': model,
})
const episodeProps = { mediaID: media.id, episodeID: media.id, versions: [media], playbackFrom: '/library', isAdmin: false, onVersionChange() {}, onChanged() {} }
const episode = renderToStaticMarkup(createElement(EpisodeDetail, episodeProps))
const adminEpisode = renderToStaticMarkup(createElement(EpisodeDetail, { ...episodeProps, isAdmin: true }))
assert.match(adminEpisode, /当前单集 \/ 文件操作/)
assert.match(adminEpisode, /编辑元数据/)
assert.doesNotMatch(adminEpisode, /整理入库/, 'episode admin controls omit organizing')
assert.match(episode, /src="\/episode-still.jpg"/, 'episode uses its own landscape artwork')
assert.match(episode, /href="\/play\/file"/, 'playback retains the selected file')
assert.ok(episode.indexOf('Synopsis sentinel') < episode.indexOf('媒体信息'), 'mobile reading order puts synopsis before technical details')
assert.match(episode, /md:col-start-2/, 'episode metadata has a desktop right column')
episodeMedia = { ...episodeMedia, backdrop_url: '' }
assert.match(renderToStaticMarkup(createElement(EpisodeDetail, episodeProps)), /暂无剧照/, 'missing artwork has an explicit fallback')
episodeMedia = { ...episodeMedia, id: 'stale-file' }
assert.doesNotMatch(renderToStaticMarkup(createElement(EpisodeDetail, episodeProps)), /Synopsis sentinel/, 'stale file metadata stays hidden')
let seasonResponse = null
const Episodes = load('LibrarySeriesEpisodes', {
  react: { ...require('react'), useState: (initial) => [initial === null ? seasonResponse : initial, () => {}] },
  '../api/library': {},
  'react-router-dom': router,
  '../api/client': client,
  '../components/Select': { Select: ({ children, ...rest }) => createElement('select', rest, children) },
  '../utils/groupSeries': { seriesTitleFromPath: () => '' },
  './seriesDetailModel': model,
  './LibrarySeasonActions': { LibrarySeasonActions: () => createElement('button', {}, '整季更多操作') },
})
const firstEpisode = { ...media, episode_num: 1, season_num: 1 }
const seasons = [{ season: 1, episodes: [firstEpisode] }]
const seasonProps = { loading: false, selectedEpisodes: seasons, selectedSeason: 1, visibleEpisodes: [firstEpisode], selectedEpisodeID: media.id, history: [], playbackFrom: '/library', onSeasonChange() {}, onEpisodeSelect() {} }
const manyEpisodes = Array.from({ length: 17 }, (_, index) => ({ ...firstEpisode, id: `ep-${index + 1}`, episode_num: index + 1, title: `第 ${index + 1} 集` }))
manyEpisodes[15].title = 'Breaking up…'
for (const title of ['第 17 集', '第17集', '第十七集', 'Episode 17', '']) {
  manyEpisodes[16].title = title
  const html = renderToStaticMarkup(createElement(Episodes, { ...seasonProps, visibleEpisodes: manyEpisodes }))
  assert.match(html, /<option value="ep-17">第 17 集<\/option>/, 'generic episode title is not repeated')
  assert.match(html, /<option value="ep-16">第 16 集 · Breaking up…<\/option>/, 'real episode title is retained')
}
const season = renderToStaticMarkup(createElement(Episodes, seasonProps))
assert.ok(season.includes('lg:grid-cols-[minmax(0,28rem)_minmax(0,1fr)]'), 'wide screens cap the current-season column and give remaining space to the season list')
assert.match(season, /第 1 季/)
assert.match(season, /可播放 1 集/)
assert.doesNotMatch(season, /aria-label="选择季"/, 'a single season needs no redundant selector')
const specials = renderToStaticMarkup(createElement(Episodes, { ...seasonProps, selectedSeason: 0, selectedEpisodes: [...seasons, { season: 0, episodes: [firstEpisode] }] }))
assert.match(specials, /特别篇/)
assert.doesNotMatch(specials, /aria-label="选择季"/, 'season dropdown is removed')
assert.match(specials, /aria-label="完整季列表"/)
assert.ok(specials.indexOf('特别篇') < specials.indexOf('第 1 季'), 'selected season comes first')
const threeSeasons = [...seasons, { season: 2, episodes: [{ ...firstEpisode, id: 'second', season_num: 2 }] }, { season: 3, episodes: [{ ...firstEpisode, id: 'third', season_num: 3 }] }]
const thirdSelected = renderToStaticMarkup(createElement(Episodes, { ...seasonProps, selectedEpisodes: threeSeasons, selectedSeason: 3, visibleEpisodes: threeSeasons[2].episodes }))
assert.ok(thirdSelected.indexOf('第 3 季') < thirdSelected.indexOf('第 1 季'))
assert.ok(thirdSelected.indexOf('第 1 季') < thirdSelected.indexOf('第 2 季'), 'other seasons retain their order')
assert.deepEqual(threeSeasons.map(({ season }) => season), [1, 2, 3], 'render does not mutate season input')
const fullList = thirdSelected.slice(thirdSelected.indexOf('aria-label="完整季列表"'), thirdSelected.indexOf('aria-label="分集列表"'))
assert.ok(fullList.indexOf('第 1 季') < fullList.indexOf('第 2 季') && fullList.indexOf('第 2 季') < fullList.indexOf('第 3 季'), 'complete list includes selected season in season order')
assert.doesNotMatch(thirdSelected, /当前选中|点击切换|点击查看详情|\/play\//, 'selection strips contain neither hints nor playback actions')
assert.match(thirdSelected, /S3E1:/)
assert.match(thirdSelected, /motion-reduce:transition-none/, 'accordion respects reduced motion')
assert.match(fullList, /aria-label="第 3 季" aria-pressed="true"/, 'selected poster exposes its pressed state')
assert.doesNotMatch(fullList, /可播放|transition-\[width\]|hover:w-/, 'poster stack never expands into a text card')
assert.match(fullList, /-ml-12/, 'season posters overlap')
assert.match(fullList, />S1<\/span>/)
assert.match(fullList, />S2<\/span>/)
seasonResponse = { mediaID: media.id, season: { title: '季独立标题', season_num: 1, poster_url: '/season-poster.jpg' }, failed: false }
const withSeason = renderToStaticMarkup(createElement(Episodes, seasonProps))
assert.doesNotMatch(withSeason, /整季更多操作/, 'viewer cannot manage season metadata')
assert.match(renderToStaticMarkup(createElement(Episodes, { ...seasonProps, isAdmin: true })), /整季更多操作/, 'administrator can manage current season')
assert.match(withSeason, /第 1 季 · 季独立标题/)
assert.match(withSeason, /src="\/season-poster.jpg"/)
assert.doesNotMatch(renderToStaticMarkup(createElement(Episodes, { ...seasonProps, selectedSeason: 0, selectedEpisodes: [{ season: 0, episodes: [firstEpisode] }] })), /season-poster.jpg/, 'previous season poster never leaks across season changes')
for (const title of ['第 1 季', '第1季', 'Season 1']) {
  seasonResponse = { mediaID: media.id, season: { title, season_num: 1 }, failed: false }
  const rendered = renderToStaticMarkup(createElement(Episodes, seasonProps))
  assert.match(rendered, />第 1 季<\/h2>/)
  assert.doesNotMatch(rendered, /第 1 季 · (?:第|Season)/)
}
for (const [status, label] of [['missing', '本地未缓存'], ['partial', '本地数据不完整'], ['complete', '本地详情和图片完整']]) {
  seasonResponse = { mediaID: media.id, season: { title: '第 1 季', season_num: 1, overview: '季简介独立内容', rating: 8.5, tmdb_id: 123, series_tmdb_id: 456, tmdb_status: status }, failed: false }
  const html = renderToStaticMarkup(createElement(Episodes, seasonProps))
  assert.match(html, /季简介独立内容/)
  assert.match(html, /评分 8.5/)
  assert.ok(html.indexOf('评分 8.5') < html.indexOf('aria-label="季资料"'), 'rating belongs inside current-season summary')
  assert.ok(html.includes(label))
  assert.match(html, /https:\/\/www.themoviedb.org\/tv\/456\/season\/1/)
}
seasonResponse = { mediaID: media.id, season: { title: '番外故事', season_num: 0 }, failed: false }
assert.match(renderToStaticMarkup(createElement(Episodes, { ...seasonProps, selectedSeason: 0, selectedEpisodes: [{ season: 0, episodes: [firstEpisode] }] })), /特别篇 · 番外故事/)
console.log('Series playback order, expanded metadata and read-only tracks checks passed')
