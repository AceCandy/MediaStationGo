# Emby 人物与媒体并列搜索

## Goal

让 Emby `/Items` 搜索把 `IncludeItemTypes` 中已支持的类型按 OR 语义处理，使人物与电影/剧集可以在同一结果集中展示，同时保持现有统一相关度排序和内存分页行为。

用户价值：播放器搜索“周星驰”时，`Person,Movie` 能返回人物“周星驰”；不会因为类型混合而丢失人物，也不会把人物扩展成其参演作品。

## Confirmed Facts

- 当前仅当 `IncludeItemTypes` 全部为 `Person` 时才走人物搜索（`internal/service/emby_compat.go:133`）；`Person,Movie` 会忽略 `Person`。
- 媒体搜索只解析 `Movie`、`Series`（`internal/service/emby_search_items.go:120`）。
- 人物可复用 PostgreSQL `PersonRepository.List`，按 `name` / `original_name` 子串召回（`internal/repository/person_repository.go:191`）。
- 现有媒体搜索已实现最多 100 个候选、统一业务排序、再内存分页（`internal/repository/media_search_repository.go:145`、`internal/repository/media_search_ranking.go:161`）。

## Requirements

- R1. 非空 `SearchTerm` 下，`IncludeItemTypes` 中的 `Person`、`Movie`、`Series` 按并列 OR 语义召回并合并结果。
- R2. 人物候选仅从 PostgreSQL 召回；媒体候选继续使用当前 OpenSearch 优先、PostgreSQL 回退路径。
- R3. 人物匹配只返回人物条目，不根据人物关系展开参演电影或剧集。
- R4. 未支持类型（例如 `MusicAlbum`）在混合请求中忽略；只要至少包含一个已支持类型，就返回这些已支持类型的结果；若全部类型均不支持，则返回空结果。
- R5. 人物和媒体合并后沿用现有排序契约：完全匹配优先，其次完整包含，最后其它匹配。完全匹配同名时按年份倒序、稳定 ID 排序；完整包含和其它匹配再按相关度指标、标题自然数字倒序、年份倒序、稳定 ID 排序。人物没有年份和标题数字时按 `0` 处理。
- R6. 两类候选按“类型 + ID”去重并统一排序后最多保留 100 条，再按 `StartIndex` / `Limit` 在内存分页；`TotalRecordCount` 表示截断后的可分页候选数。每个来源最多召回 100 条供合并排序。
- R7. 纯 `Person` 搜索继续返回 Person payload；混合搜索中的人物 payload 与现有 `/Persons` 列表字段兼容。
- R8. 保留现有单字符加 `%` 的 Yamby/Emby 特殊处理。
- R9. OR 语义仅应用于非空 `SearchTerm`；无搜索词的普通 `/Items` 浏览、`IDs` 优先级和现有层级浏览行为保持不变。

## Acceptance Criteria

- [ ] AC1. 搜索“周星驰”且类型为 `Person` 或 `Person,Movie` 时，能返回 `Type=Person` 的“周星驰”；不存在匹配标题的电影时不返回其参演作品。
- [ ] AC2. 类型为 `Person,Movie` 且人物名、电影名均匹配时，两种类型出现在同一结果集中，并按统一排序契约排列。
- [ ] AC3. `Person,MusicAlbum` 仍返回匹配人物；仅 `MusicAlbum` 返回空结果。
- [ ] AC4. `Movie,MusicAlbum` 的媒体搜索结果与仅 `Movie` 一致，未支持类型不拖累已支持类型。
- [ ] AC5. 混合候选超过请求页大小时，先在不超过 100 条的统一候选集上排序，再内存分页；翻页顺序稳定且 `TotalRecordCount` 一致。
- [ ] AC6. 人物精确匹配优先于人物或媒体的包含/其它匹配；媒体原有数字别名、自然数字倒序、年份倒序规则不回归。
- [ ] AC7. OpenSearch 可用与回退 PostgreSQL 两条媒体路径均能与 PostgreSQL 人物结果合并。
- [ ] AC8. 现有纯媒体、纯人物、单字符 `%` 搜索测试继续通过。
- [ ] AC9. 无 `SearchTerm` 的 `Person,Movie` 等请求不进入人物与媒体合并搜索，原有浏览结果和分页不变。

## Out of Scope

- 为人物建立 OpenSearch 索引。
- 根据人物返回其参演作品。
- 新增 `MusicAlbum` 等媒体类型支持。
- 改变现有搜索上限、排序权重或数字 0–100 的别名范围。
- 改变无搜索词的普通 `/Items` 浏览语义。

## Operational Note

- 工作树中已有上一任务的单字符 `%` 兼容修复，实施本任务前需先独立提交，避免两个任务混入同一提交。
