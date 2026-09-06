# 媒体库按元数据分页

## Goal

Web 电影与电视剧列表按 metadata 分页，避免首屏耗时随全库文件数增长；确认 Emby 列表的同类问题及改造边界。

## Requirements

- Web 列表以电影或整剧 metadata 为分页单位；文件关联用于验证当前媒体库归属与访问权限。
- 筛选、总数和分页使用一致的范围；同作品多个版本不重复占据卡片。
- 列表仅读取当前页展示信息及必要统计，版本和分集详情按需加载。
- 保持详情、收藏、播放及管理员操作可用，明确 metadata ID 和文件 ID 的区别。
- 保持现有排序和集数语义；实现前核对具体查询映射。
- Emby 同时修复混合库全量加载、整剧列表展开全部分集及子级列表内存分页；保留普通电影已经正确的 metadata 分页。

## Acceptance Criteria

- [ ] Web 首屏和后续页均不加载全库文件详情，没有 50,000 条截断。
- [ ] 多版本、跨库、受限权限、缺海报与缺中文标题筛选后无重复、遗漏或越权。
- [ ] 电影详情及版本选择、电视剧详情及选集、收藏与管理员操作正常。
- [ ] PostgreSQL 回归实际执行，记录是否跳过；Web lint/build 通过。
- [ ] 独立复核 SQL 分页边界与列表响应字段，区分代码证据和运行时耗时证据。

## Confirmed Evidence

- Web 电影：`internal/service/media_listing.go:147` 读取最多 50,000 条分组源，再在内存分组、分页。
- Web 电视剧：`internal/service/media_series.go:57` 加载全库分集、聚合并补整剧展示，`internal/handler/series.go:78` 最后切页。
- 前端：`web/src/pages/useLibraryData.ts:66` 先获取媒体库信息，再请求 50 条列表。
- Emby 普通电影：`internal/service/emby_metadata_scope.go:15` 已在 SQL 中按 metadata 分页，然后读取当前页版本。
- Emby 普通电视剧：`internal/service/emby_metadata_scope.go:115` 先分页整剧 ID，随后读取当前页整剧的全部分集和版本。
- Emby 混合电影库：`internal/service/emby_movie_items.go:37` 对电影和整剧分页方法传入 limit=0，全部构造 payload 后才切页。
- Emby 分集：`internal/service/emby_series.go:37`、`:64` 读取目标整剧或季的全部文件，`internal/service/emby_items_list.go:83` 再分页。
- Emby 搜索：`internal/service/emby_search_items.go:109` 对当前页整剧补取全部分集；最近添加的整剧复用 seriesMetadataPage。

## Compatibility Findings

- `web/src/pages/LibrarySeriesDetailHeader.tsx` 与 `LibraryPageDialogs.tsx` 把卡片 rep.id 作为文件 ID 调用整剧查询及管理接口。
- `internal/repository/favorite_repository.go:17` 的 Toggle 仍先按文件 ID 解析 metadata，不能只把卡片 id 替换为 metadata ID。
- Web 原电影排序在版本代表选择后使用 CreatedAt；整剧排序来自分集的日期、年份和修改时间，不能假定现有排序已经采用整剧 metadata 日期。

## Out of Scope

- 扫描、刮削、媒体文件搬移及数据库内容迁移。
- 没有运行时测量前不承诺具体提速倍数，也不把缓存作为全库加载的替代修复。
