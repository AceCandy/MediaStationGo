# 验证记录

- 隔离 PostgreSQL 17：service/handler 针对性测试通过。命令筛选为 `TestLibraryMetadataPagination|TestLibrarySeriesCards|TestListMediaVisibleGrouped|TestMediaSeriesDetail|TestListLibrarySeries|TestListMediaGroups|TestListMediaVersions`。
- 新回归断言：Web 每卡一个文件视图；Emby 整剧/季摘要零文件视图；分集按页读取版本；跨库、旧链接、缺图/中文标题、整剧 NSFW、空页、分段版本数及摘要后详情完整。
- handler 大剧回归覆盖 2001 集；修复相关测试缺失 probe/history 表的 fixture，历史选版 fixture 直接插入一条历史，不依赖未迁移的 Upsert 索引。
- Web `npm run lint`、`npm run build`、`check-series-detail.mjs`、`check-series-presentation.mjs` 通过。
- 独立静态复核后主线程点验：季 ID 的疑似父级错误不可达，Season 分支在构造 Season 摘要前已返回 Episode 分页；未据此改动。
- Emby API catalog 对照：路由、认证、参数和响应字段未变，内部分页下沉，无需修改 catalog。

## 已有失败

扩展 service 回归有 14 项失败，在基线 `1656980` 复现相同失败；没有为本次性能修复扩大生产改动范围：

`TestEmbyItemsFilterByPerson`、`TestEmbyLatestItemsOrderByReleaseDate`、`TestEmbyLatestSeriesFiltersEpisodesBeforeGrouping`、`TestEmbyItemsUseScalarStreamsWhileItemUsesCompleteProbeDocument`、`TestEmbyMovieLibraryGroupsEpisodicContentIntoSeries`、`TestEmbyMediaSourceUsesLocalSTRMTargetContainer`、`TestEmbyPlaybackInfoAsynchronouslyProbesLocalSTRMTarget`、`TestEmbyPlaybackInfoProbesAllLocalSTRMVersions`、`TestEmbyPlaybackInfoProbesMissingHTTPTrackMetadata`、`TestEmbyItemsExposeSeriesSeasonEpisodeHierarchy`、`TestEmbyMetadataVersionsShareUserStateAndKeepSourceIDs`、`TestEmbyMultipartKeepsVersionsAndConcretePartPlayback`、`TestMediaVisibilityFiltersNSFWAndLibraries`、`TestMediaVisibilityDoesNotApplyRetiredProviderRules`。

## 未验证与边界

- 未做真实播放器、浏览器端到端联调或线上规模 EXPLAIN/耗时比较；不承诺具体倍数。
- 数据库仍需聚合关联文件来计算总数和排序；本轮移除的是应用层全库文件加载、完整 payload 构造和内存分页。
- Web 加载媒体库信息后再请求列表的串行流程、显式选剧后的整剧文件读取保持现状。
- 用户已确认提交并归档；未部署、无数据库迁移，以上未验证项与已有失败保留供后续跟进。
