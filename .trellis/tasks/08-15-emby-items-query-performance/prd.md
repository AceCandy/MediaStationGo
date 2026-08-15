# 优化 Emby 列表查询性能

## Goal

降低 Emby `/Users/{userId}/Items` 首次分页加载时的数据库查询量，消除随页面条目数线性增长的逐项查询，同时保持现有播放器可见响应和播放兼容性不变。

## Background

- 当前一条 `/Items` HTTP 请求会为每个媒体分别查询 `People`、`ProviderIds` 和同作品的 `MediaSources`；Series/Season 卡片还会逐项查询元数据、图片和用户状态。
- `payloadsForViews` 已批量加载收藏和播放历史，但上述关联数据仍是逐项查询。
- 列表返回的 `MediaSources` 及标量 `MediaStreams` 已被现有测试和播放器播放路径依赖，不能通过默认删字段来换取性能。
- `Fields` 参数当前未被解析，客户端即使只请求轻量列表字段，服务端仍会读取并返回完整关联数据。

## Requirements

- R1. 对同一页 Movie/Episode/Series/Season 的元数据、图片、用户状态、演职员、外部标识、可见媒体版本和剧集媒体库分类执行页级批量加载，列表条目循环内不得再发起这些数据库查询。
- R2. 未传 `Fields` 时保持现有 JSON 形状；显式传入时仅按需查询和返回 `People`、`ProviderIds`、`MediaSources`，其排序、映射、可见性和去重语义不变。
- R3. 列表继续使用标量流信息，不读取完整探测文档、不调度 lazy ffprobe；单条详情继续使用现有完整探测路径。
- R4. 保持现有关联数据错误降级语义：批量关联查询失败时返回相应空值或当前媒体源，不让整个列表请求新增失败。
- R5. 性能改造只修改消除 N+1 与支持 `Fields` 裁剪所需的查询和映射代码；不新增缓存层，不改变路由或鉴权。
- R6. Emby 播放器 API 请求在现有 HTTP 日志中记录脱敏后的 header 与 query 参数，便于确认客户端实际请求的字段；不得记录认证、Cookie、Token、设备标识或请求体明文。

## Out of Scope

- 删除默认响应中的 `People`、`ProviderIds` 或 `MediaSources`。
- 优化 `metadataPage` 的分组计数、聚合排序或深分页 `OFFSET`。
- 调整缓存键、缓存 TTL、数据库 schema 或索引。

## Acceptance Criteria

- [x] AC1. 对包含多条媒体的同一页，关联数据查询次数保持常数级，不随 Movie/Episode/Series/Season 条目数线性增加。
- [x] AC2. 现有列表标量流、媒体多版本、播放 URL、剧集层级与 People 隔离测试继续通过。
- [x] AC3. 新增可运行的 PostgreSQL 回归检查，证明多条媒体和 Series/Season 卡片的 payload 查询次数不随条目数增长。
- [x] AC4. 列表 `People`、`ProviderIds`、`MediaSources` 的代表性响应断言覆盖批量路径，且详情完整流行为不变。
- [x] AC5. 代码格式化、针对性 Go 测试和 `git diff --check` 通过；不要求全量编译或全仓测试。
- [x] AC6. `/emby` 与根路径兼容路由均带 `player_api=true`、脱敏 headers/query；普通 API 不附带请求详情，敏感测试值不出现在日志中。
- [x] AC7. `Fields` 被解析并进入缓存键；显式轻量字段不查询或返回 People、ProviderIds、MediaSources，省略 `Fields` 时保持原响应。

> 验证说明：使用 DBX 确认的 `mediastation_test` 数据库运行随机隔离 schema 测试，测试结束已自动清理。
