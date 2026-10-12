import assert from 'node:assert/strict'
import { groupSeries, seriesCardLink } from '../src/utils/groupSeries.ts'
import { mediaLibraryBackTarget } from '../src/pages/MediaDetailPageModel.ts'

const item = { library_id: 'hongguo', catalog_source: 'hongguo', title: '同名作品', scrape_status: 'matched', path: '/media/短剧/同名作品/Season 01/S01E01.mkv' }
const cards = groupSeries([
  { ...item, id: 'group-file', lookup_catalog_id: '101', series_id: 'hg-group-1000' },
  { ...item, id: 'series-file', lookup_catalog_id: '102', series_id: 'hg-work-standalone' },
  { ...item, id: 'movie-file', lookup_catalog_id: '103' },
  { ...item, id: 'another-movie-file', lookup_catalog_id: '104' },
])
assert.equal(cards.length, 4, '不同红果作品不能因目录或同名合并')
assert.deepEqual(cards.map(seriesCardLink), [
  '/library/hongguo?series_id=hg-group-1000',
  '/library/hongguo?series_id=hg-work-standalone',
  '/media/movie-file',
  '/media/another-movie-file',
])
assert.equal(groupSeries([cards[0].rep, { ...cards[0].rep, id: 'version', path: '/另一位置/作品.mkv' }]).length, 1)
assert.equal(groupSeries([{ ...item, catalog_source: '', id: 'ordinary' }, { ...item, catalog_source: 'nfo', id: 'nfo', library_id: 'nfo' }, ...cards.map(card => card.rep)]).length, 6)

const episode = { id: 'episode-file', library_id: 'ordinary', catalog_source: '', season_num: 1, episode_num: 1, path: '/media/电视剧/作品/Season 01/S01E01.mkv', title: '作品' }
assert.equal(seriesCardLink(groupSeries([episode])[0]), '/media/episode-file', '没有作品身份的卡片进入媒体详情，不生成哈希深链')
assert.equal(mediaLibraryBackTarget(episode), '/library/ordinary')
for (const source of ['', 'nfo', 'hongguo', 'huangguoai']) {
  const linked = { ...episode, catalog_source: source, series_id: 'work-id', display_library_id: 'display-library' }
  assert.equal(seriesCardLink(groupSeries([linked])[0]), '/library/display-library?series_id=work-id')
  assert.equal(mediaLibraryBackTarget(linked), '/library/display-library?series_id=work-id')
}
assert.equal(seriesCardLink({ key: 'metadata:work-id', rep: episode, linkMedia: episode, count: 2 }), '/library/ordinary?series=metadata%3Awork-id')
assert.equal(mediaLibraryBackTarget({ ...episode, library_id: '' }), '')
