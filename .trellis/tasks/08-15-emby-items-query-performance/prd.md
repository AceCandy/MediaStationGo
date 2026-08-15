# 优化 Emby 列表查询性能

## Goal

降低 Emby `/Users/{userId}/Items` 首次分页加载时的数据库查询量，消除随页面条目数线性增长的逐项查询，同时保持现有播放器可见响应和播放兼容性不变。

## Background

- 当前一条 `/Items` HTTP 请求会在 `itemPayload` 中为每个媒体分别查询 `People`、`ProviderIds` 和同作品的 `MediaSources`；剧集类型判断还会重复查询媒体库。
- `payloadsForViews` 已批量加载收藏和播放历史，但上述关联数据仍是逐项查询。
- 列表返回的 `MediaSources` 及标量 `MediaStreams` 已被现有测试和播放器播放路径依赖，不能通过默认删字段来换取性能。
- `Fields` 参数当前未被解析。本任务不改变这一对外行为。

## Requirements

- R1. 对同一页媒体的演职员、外部标识、可见媒体版本和剧集媒体库分类执行页级批量加载，列表条目循环内不得再发起这些数据库查询。
- R2. 保持 `People` 的类型顺序与字段、`ProviderIds` 的 provider 映射、`MediaSources` 的可见性/去重/首选版本顺序及现有 JSON 形状不变。
- R3. 列表继续使用标量流信息，不读取完整探测文档、不调度 lazy ffprobe；单条详情继续使用现有完整探测路径。
- R4. 保持现有关联数据错误降级语义：批量关联查询失败时返回相应空值或当前媒体源，不让整个列表请求新增失败。
- R5. 只修改消除 N+1 所需的后端查询与映射代码；不新增缓存层，不改变路由、鉴权、参数或播放器可见字段。

## Out of Scope

- 实现或启用 Emby `Fields` 字段裁剪。
- 删除默认响应中的 `People`、`ProviderIds` 或 `MediaSources`。
- 优化 `metadataPage` 的分组计数、聚合排序或深分页 `OFFSET`。
- 调整缓存键、缓存 TTL、数据库 schema 或索引。

## Acceptance Criteria

- [ ] AC1. 对包含多条媒体的同一页，关联数据查询次数保持常数级，不随条目数按 `People + ProviderIds + MediaSources + library` 线性增加。
- [ ] AC2. 现有列表标量流、媒体多版本、播放 URL、剧集层级与 People 隔离测试继续通过。
- [ ] AC3. 新增一个可运行的回归检查，在修复前能暴露列表 N+1，修复后能证明多条列表的查询次数不随条目数增长。
- [ ] AC4. 列表 `People`、`ProviderIds`、`MediaSources` 的代表性响应断言覆盖批量路径，且详情完整流行为不变。
- [ ] AC5. 代码格式化、针对性 Go 测试和 `git diff --check` 通过；不要求全量编译或全仓测试。
