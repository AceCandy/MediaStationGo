# Emby 人物与媒体并列搜索：实施计划

## 前置隔离

1. 复核当前 4 个未提交文件只属于单字符 `%` 兼容修复，并在用户授权后独立提交；不得丢弃或混入本任务提交。

## 实施步骤

1. 更新搜索类型分流
   - 仅在非空 `SearchTerm` 且没有更高优先级的 IDs 请求时启用混合搜索语义。
   - 分别识别 Person 与 Movie/Series；忽略 MusicAlbum 等未支持类型。
   - 验证：纯 Person、Person+Movie、Person+MusicAlbum、仅 MusicAlbum、无搜索词均命中预期分支。

2. 召回并统一候选
   - 媒体以 `offset=0, limit=100` 走现有 SearchMetadataIDs。
   - 人物以 `offset=0, limit=100` 走 PersonRepository.List。
   - 用最小类型化候选投影复用现有匹配层级和比较逻辑，不复制整套排序实现。
   - 合并排序后截断 100 条并内存分页。
   - 验证：完全匹配、包含、其它匹配、自然数字、年份和稳定 tie-breaker 测试。

3. 组装异构 payload
   - 媒体复用现有 Movie/Series payload。
   - 人物复用 `personPayload`，不查询或展开 credits。
   - 按统一分页后的候选顺序输出，返回合并后的 TotalRecordCount。
   - 验证：同页同时包含 Person 与 Movie/Series，顺序和总数正确。

4. 同步玩家接口目录与规范
   - 更新 `/Items` 的 IncludeItemTypes/SearchTerm 行为说明和示例。
   - 仅在实现产生新的稳定契约时更新 shared-media-metadata 规范。

## 验证命令

- `gofmt` 处理本任务修改的 Go 文件。
- `go test ./internal/repository ./internal/service`
- 有 `MEDIASTATION_TEST_POSTGRES_DSN` 时运行相关 PostgreSQL 集成测试；没有时明确记录跳过。
- `cd web && npm run lint`
- `cd web && npm run build`
- `git diff --check`
- 独立复核：检查 PRD/设计一致性、类型 OR 语义、无搜索词回归、OpenSearch 回退、100 条上限与分页。

## 重点文件与回滚点

- `internal/service/emby_compat.go`：Items 分流，必须保持无搜索词和 IDs 优先级。
- `internal/service/emby_search_items.go`：异构召回、排序、分页和 payload 汇合点。
- `internal/repository/media_search_ranking.go`：只做支持异构候选所需的最小扩展，避免复制排序。
- `internal/service/emby_movie_library_test.go` 或同层搜索测试：覆盖请求级行为。
- `internal/repository/media_search_ranking_test.go`：覆盖统一排序。
- `web/src/pages/embyApiCatalog.ts`：同步玩家可见契约。

任一步验证失败时只回退本任务新增的混合搜索逻辑，不回退前置单字符 `%` 修复。
