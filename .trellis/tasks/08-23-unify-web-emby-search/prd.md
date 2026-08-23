# 统一 Web 与 Emby 的 Metadata 搜索

## Goal

将搜索的结果与分页粒度统一为逻辑 Metadata：OpenSearch 和 PostgreSQL
回退均返回顶层 Movie/Series Metadata，不再以底层 Media 文件作为搜索文档；
Web 与 Emby 共享候选作品搜索，同时保留不同的字段范围和响应格式。

## Background

- 当前 OpenSearch 每条文档对应一个 Media ID，虽然标题、简介等取自
  MediaView 中的 Metadata 投影；多个播放版本会成为多条搜索文档。
- Web 搜索的 PostgreSQL 回退路径会清理并拆分关键词、大小写去重、转义
  `\\`、`%`、`_`，并要求每个关键词都命中。
- 任务开始时，Emby 的媒体、系列和内存 Episode 搜索分别直接匹配完整原始
  字符串，SQL 路径还会把 `%`、`_` 当成通配符。
- Web 搜索字段范围有意更宽；Emby 搜索必须继续遵守 `ParentId`、
  `IncludeItemTypes`、用户可见性和 `Series -> Season -> Episode` 层级。
- Emby 搜索结果和总数必须继续按逻辑 Metadata 项计算，不能让多个媒体版本
  重复占用分页或放大 `TotalRecordCount`。

## Requirements

- R1：OpenSearch 每条文档对应一个顶层 Metadata，只索引 `movie` 和
  `series`，文档 ID 为 Metadata ID。
- R2：OpenSearch 不保存 Media ID、路径、扫描标题、媒体流或播放版本字段；
  Metadata 搜索内容只来自 Metadata 自身字段。
- R3：Movie 仅在自己关联至少一个未删除 Media 时具备搜索资格；Series 仅在
  至少一个后代 Episode 关联未删除 Media 时具备搜索资格。无可播放内容的
  Metadata 不进入索引，失去最后一个可播放内容时删除索引文档。
- R4：Metadata 文档允许保存派生 `library_ids`：Movie 从直接关联 Media
  聚合，Series 从可播放后代聚合；搜索权限使用可见媒体库交集过滤。
- R5：OpenSearch 和 PostgreSQL 回退均以 Metadata ID 和逻辑作品总数作为
  结果，不允许播放版本放大 `TotalRecordCount` 或占用分页。PostgreSQL 总数
  实时准确；OpenSearch 总数按 Metadata 文档计数，允许在同步窗口内短暂滞后。
- R6：Web 与 Emby 复用同一份关键词清理、拆词、去重和 LIKE 转义逻辑，
  不新增通用搜索框架或第三方依赖。
- R7：Web 搜索 Metadata 的标题、原名、简介和类型；不再搜索 Media 路径或
  扫描标题。Emby 只搜索顶层 Movie/Series 的标题和原名；Season/Episode
  继续支持正常浏览，但不参与任何 `SearchTerm` 搜索。
- R8：Emby 的路由、参数名、鉴权、响应 JSON、缓存边界、用户可见性、
  `ParentId`、媒体类型和层级聚合行为保持不变。
- R9：更新 Emby API 目录中的 `SearchTerm` 说明，使多关键词及字面量匹配
  行为与后端一致，不改 endpoint 支持级别。
- R10：Media 新增、删除、换绑以及 Metadata 更新时重新计算受影响 Movie/
  Series 的索引资格与 `library_ids`；搜索结果返回前由 PostgreSQL 再次确认
  Metadata 仍有当前用户可见的可播放内容。
- R11：权限限制已启用但计算出的有效可见媒体库为空时必须返回空结果，不能把
  空列表解释为“不限制媒体库”。

## Acceptance Criteria

- [ ] AC1：OpenSearch 中同一 Metadata 的多个 Media 版本只产生一条文档，
  搜索结果、总数与分页均按 Metadata 计算；同步窗口内总数可短暂滞后，但不
  得因播放版本数量而放大。
- [ ] AC2：只有具有有效 Media 的 Movie、具有可播放 Episode 后代的 Series
  能被搜索；Season、Episode 和无可播放内容的 Metadata 不进入索引。
- [ ] AC3：用户只能搜索到 `library_ids` 与其有效可见媒体库有交集的作品；
  同一 Metadata 同时存在于可见和隐藏库时仍可通过可见库命中；受限且有效
  可见库为空时返回空结果。
- [ ] AC4：Emby `/Items` 或 `/SearchHints` 使用由空白、标点或符号分隔的
  多个关键词时，只要所有关键词分别命中标题相关字段，就返回目标 Movie 或
  Series；Season/Episode 不出现在搜索结果中。
- [ ] AC5：Emby 搜索词中的 `\\`、`%`、`_` 不再作为 SQL 通配符扩大结果集。
- [ ] AC6：Web 的 OpenSearch 与 PostgreSQL 回退都按 Metadata 的标题、原名、
  简介和类型搜索，不读取或匹配 Media 路径与扫描标题。
- [ ] AC7：OpenSearch 短暂残留已失去 Media 的 Metadata 时，数据库复核会将其
  从当前页排除；索引同步后对应文档被删除。该窗口内 `TotalRecordCount` 允许
  短暂包含陈旧 Metadata。
- [ ] AC8：Emby API 目录准确描述新的 `SearchTerm` 语义，且无路由、参数名、
  响应顶层结构或 endpoint 支持级别变化。
- [ ] AC9：定向 Go 测试、相关 Web 校验和 `git diff --check` 通过；若本地缺少
  PostgreSQL 测试 DSN，数据库测试明确跳过并在交付中说明。

## Out of Scope

- 不让 Web 调用 Emby service，也不让 Emby 复用 Web 的响应、分页或分组层。
- 不扩大 Emby 到简介或类型搜索。
- 不索引 Season、Episode、未绑定 Metadata 的 Media 或任何 Media 播放字段。
- 不改变无 `SearchTerm` 时 Series -> Season -> Episode 的正常浏览与播放流程。
- 不处理截图之外尚未证实的 Yamby 客户端 UI 问题。

## Technical Notes

- 搜索后端的公共契约应返回 Metadata ID 与逻辑作品总数；Web 和 Emby 分别
  加载可见播放版本并组装自己的响应。
- OpenSearch 文档存在本身代表 Metadata 具备全局可播放内容；`library_ids`
  是唯一允许从 Media 关系派生的权限投影，不保存 Media 明细。
- 这是跨索引、同步、查询、权限和分页的复杂任务，最终规划需要 `design.md`
  与 `implement.md`。

## Risks and Deferred Items

- 现有 OpenSearch 索引需要全量重建；切换过程必须避免新旧文档粒度混用。
- OpenSearch 是最终一致投影，Media/Metadata 变更后的短窗口内总数可能滞后；
  数据库复核必须保证不会返回无播放内容或无权限的作品。
- Web 将不再通过 Media 路径或扫描标题命中作品，这是明确的范围收窄。
- Emby 多词搜索会从“完整字符串连续匹配”改为“拆词后全部命中”；这是本次
  明确修正，但可能扩大含空格/标点标题的命中范围。
- `%`、`_`、`\\` 的旧通配符行为将被移除；它们按字面量匹配，其他纯分隔符
  搜索返回空结果，避免意外全库匹配。
