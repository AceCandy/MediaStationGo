# 优化 NFO 浏览与常规库存在性查询

## 目标

减少 Web/Emby NFO 库浏览的无用汇总与最新媒体总数统计，保持结果、权限和状态兼容。

## 要求与事实

- R1：Emby NFO 作品层候选先分页，再补当前页详情。NFONodes（internal/repository/nfo_media_view.go:84）目前展开层级聚合，hongGuoHierarchyItems 才分页。
- R2：指定 NFO 库 Latest 不统计总数。LatestItems（internal/service/emby_hongguo.go:125）目前复用层级查询却只使用 Items。播放筛选仍在分页前。
- R3：Web nfoLibraryPage（internal/repository/nfo_media_view.go:104）仅对当前页统计集数、版本数、代表文件，保留现有 MAX 日期排序和精确总数。
- R4：movieLibraryHasEpisodicContent（internal/service/emby_movie_items.go:16）用 EXISTS 替代 Limit(1).Count()，保留原条件与合并库范围。

## 验收标准

- [x] 身份、排序、状态、分页、越界空页总数与原结果一致。
- [x] 文件、条目、祖先 NSFW、隐藏/允许库与 locked-empty 在分页前生效。
- [x] 标题候选不汇总无用日期和状态；当前页字段仍完整。
- [x] 指定 NFO 库 Latest 无总数查询；Web 页内详情查询范围受限。
- [x] 电影库存在性检测对空库、有/无剧集与现有库范围结果一致（当前 mergedLibraryIDs 不展开合并库）。
- [x] 隔离 PostgreSQL 回归、实际查询计划、独立复核通过，并清理测试环境。

## 不在范围内与风险

保留 NFO Web MAX、Emby 库内 MIN 的现有排序差异，不顺带统一产品行为。日期与状态筛选仍有必要读取候选文件，不保证恒定耗时。
不改全局混合查询、常规剧库时间汇总、搜索索引、缓存、schema；不部署、重启、提交或归档。保留此前红果未提交改动。
