# Metadata 搜索实施计划

## 1. 建立 Metadata 搜索契约

- 将外部搜索接口从 Media ID 改为 Metadata ID，并增加 Web/Emby 字段范围、
  Metadata 种类、NSFW 和有效可见媒体库过滤。
- 过滤结构区分 unrestricted 与 restricted-empty；restricted-empty 直接空结果。
- 增加最小的 Metadata 搜索文档 DTO，只包含设计文档列出的字段。
- 保留现有关键词拆分和 PostgreSQL LIKE 转义 helper。

验证：接口编译通过；单元测试证明过滤对象不包含 Media ID、路径或扫描标题。

## 2. 改造 OpenSearch 索引

- 使用新的 metadata 专用索引名称和 mapping。
- 搜索返回 Metadata ID，Web 使用四个 Metadata 字段，Emby 使用标题/原名。
- `library_ids` 使用 keyword 数组和交集过滤；移除 Media 字段与 Media 文档写入。
- 增加删除 Metadata 文档、版本化 concrete index、schema 版本校验和 alias 原子
  切换；alias 不存在或不兼容时返回错误，由调用方回退 PostgreSQL。

验证：HTTP mock 覆盖 mapping、bulk 文档、Movie/Series 查询字段、权限交集、
文档删除、restricted-empty、强制 Movie/Series kind 及未就绪回退。

## 3. 实现 Metadata 投影查询与 PostgreSQL 回退

- Movie 通过直接 Media EXISTS 聚合 `library_ids`。
- Series 通过 Series -> Season -> Episode -> Media 聚合 `library_ids`。
- 只产出顶层 Movie/Series；无有效 Media 的 Metadata 不产出。
- PostgreSQL 搜索先按 Metadata 过滤、计数和分页，再按搜索顺序加载可见 Media。
- 同一 Metadata 多版本及跨库版本只计数一次。
- Movie 使用直接 Media；Series 使用后代 Episode Media，结果按 Metadata ID
  搜索顺序组装，不能复用只加载直接 `media.metadata_id` 的方法。

验证：PostgreSQL 集成测试覆盖 Movie 多版本、Series 多 Episode、无 Media、跨库
可见性、NSFW、准确总数和分页。

## 4. 接入 Web 搜索

- `/media` 搜索改用 Metadata ID 契约，保持响应 JSON 和卡片播放数据兼容。
- OpenSearch 失败或未就绪时使用相同 Metadata 粒度的 PostgreSQL 回退。
- 删除路径和扫描标题搜索断言，保留标题、原名、简介、类型范围。

验证：repository/service/handler 定向测试；Web lint、类型检查和 build。

## 5. 接入 Emby 顶层搜索

- `/Items` 与 `/SearchHints` 的非空 `SearchTerm` 统一进入顶层 Metadata 搜索。
- 只返回 Movie/Series；Season/Episode 搜索返回空，但无关键词层级浏览不变。
- 保持 `ParentId` 媒体库范围、IncludeItemTypes、用户权限、分页和响应结构。
- PersonIds、收藏、继续播放等复杂组合使用 PostgreSQL Metadata 路径。
- 删除当前 Episode/Season SearchTerm 搜索实现及冲突测试。
- 在 `EmbyService.Items` 的 Season/Series 识别和混合电影库分支之前处理非空
  `SearchTerm`，避免落回 Episode 内存过滤或 Media/扫描标题查询。

验证：Emby handler/service 测试覆盖 Movie、Series、多关键词、特殊字符、只请求
Episode、库范围、权限、逻辑分页和 SearchHints 投影。

## 6. 重做增量同步与全量回填

- 用“刷新受影响顶层 Metadata”替换按 Media ID 写索引。
- 覆盖 Media 新增、换绑、删除、按库清理，以及 Metadata 更新/删除。
- Series 投影要求 Season/Episode/Media 均未删除、父链 kind 正确，并去重非空
  `library_id`；换绑/删除必须在写入前收集旧顶层 ID，提交后刷新新旧两侧。
- 全量回填只扫描具备资格的 Movie/Series；增量变更记录 dirty Metadata IDs，
  回填后重放并原子切换 alias，完成前 OpenSearch 不接管查询。
- 更新搜索 compose 配置使用 metadata 专用索引；不自动删除旧索引。

验证：测试新增/删除最后一个 Media、Series 最后一集、换绑、跨库聚合、重复回填
幂等和首次回填前 PostgreSQL 回退。

## 7. 文档与最终质量门

- 同步 Emby API 目录与搜索配置说明。
- 更新 `embyApiCatalog.ts` 中 `/Items` 与 `/SearchHints` 的 `SearchTerm` 条目，明确
  只返回顶层 Movie/Series。
- 运行相关 Go 包测试、OpenSearch mock、PostgreSQL 集成测试、Web lint/build、
  `git diff --check`。
- 独立审查索引粒度、权限交集、分页顺序、全部 Media 变更入口及旧索引迁移。

## 主要代码落点

- `media_repository.go`：搜索接口从 Media ID 改为 Metadata ID。
- `media_search_repository.go`：Metadata 回退、回填与增量同步入口。
- `media_view_repository.go`：按顶层 Metadata 加载 Movie/Series 可见播放版本。
- `metadata_repository.go`：Metadata 更新后刷新自身或顶层 Series。
- `opensearch.go`：metadata mapping、查询、bulk、delete、alias 与 schema 校验。
- `service_builder.go`、`service_search_warmup.go`：后端就绪和首次回填切换。
- `media_search.go`：Web 顶层 Metadata 结果组装与相关度顺序。
- `emby_compat.go`、`emby_items_list.go`、`emby_movie_items.go`：Emby 顶层搜索
  前置路由，移除 Media/Season/Episode 搜索分支。
- `docker-compose.search.yml`：切换 metadata 专用索引 alias。
- `embyApiCatalog.ts`：同步 SearchTerm 支持范围。

## 回滚点

- OpenSearch 使用独立 metadata 索引，不覆盖或删除旧 Media 索引。
- PostgreSQL Metadata 回退独立可用；OpenSearch 后端可通过现有配置关闭。
- 不在实现或测试中自动执行不可恢复的索引删除。
