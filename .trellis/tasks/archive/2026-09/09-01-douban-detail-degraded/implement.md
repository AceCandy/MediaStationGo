# 实施计划

1. 扩展豆瓣详情请求结果
   - 在现有 enrichment provider 路径按 movie/series 选择 `/movie` 或 `/tv`。
   - 仅将 HTTP 403、JSON `code=1000` 分类为权限受限并请求 `/subject`。
   - 用聚焦 provider 测试验证 movie/tv URL、权限降级、404 和临时异常边界。

2. 持久化显式降级状态
   - 为 `MetadataProviderSnapshot` 增加默认 false 的 `Degraded` 字段。
   - 保持现有完整快照写入口，并增加显式降级写入；冲突更新同步状态。
   - 用 repository 测试验证降级写入、完整覆盖清除和历史默认值。

3. 复用补齐流程支持 movie/series
   - 将现有电影补齐核心收窄重构为 movie/series 共用逻辑，season/episode 仍拒绝。
   - 降级响应只填空字段、不清空或覆盖现有非空值，并在最后保存对应状态快照。
   - 保持定时电影入口名称和指标兼容，增加降级成功指标/详情并继续处理本批。

4. 跳过定时降级候选
   - 在 `ListDoubanMovieEnrichmentAfter` 候选 SQL 排除 `degraded=true` 快照。
   - 覆盖无快照、完整快照、历史 partial、显式 degraded 和管理员手动绕过场景。

5. 扩展详情 API 与管理员手动恢复
   - 详情投影优先返回 `douban_status=degraded`。
   - 复用现有管理员路由支持 movie/series，并返回 `complete | degraded` 结果。
   - handler 测试验证普通用户仍被拒绝、movie/series 可重试、其他 kind 不可用。

6. 展示降级状态
   - 更新前端 `Media` 类型和现有 provider 徽章文案。
   - 降级时复用当前管理员操作，显示“重试完整豆瓣信息”，并按响应显示正确提示后刷新详情。
   - 不新增组件、页面或弹窗。

7. 验证与独立复核
   - 运行受影响的 Go provider/repository/service/handler 聚焦测试。
   - 运行 Web 相关测试（如现有）、`npm run lint`、`npm run build` 和 `git diff --check`。
   - 使用 `trellis-check` 独立核对错误分类、数据保护、权限、跨层枚举和定时跳过。

8. 更新项目契约
   - 在质量检查通过后更新共享元数据规范中的豆瓣降级状态、movie/tv 路由和恢复规则。
   - 不把实现过程或临时调试信息写入规范。

## 预计修改范围

- `internal/model/metadata.go`
- `internal/model/media_view.go`
- `internal/repository/catalog_repository.go`
- `internal/repository/metadata_repository.go`
- `internal/service/douban.go`
- `internal/service/douban_enrichment.go`
- `internal/service/media_listing.go`
- `internal/handler/media.go`
- 对应 Go 测试
- `web/src/types/media.ts`
- `web/src/api/library.ts`
- `web/src/pages/MediaDetailMetadata.tsx`
- `web/src/pages/MediaDetailAdminPanel.tsx`
- `web/src/pages/MediaDetailPageSections.tsx`
- `web/src/pages/useMediaDetailPageState.ts`

## 回滚点

- Provider 错误分类和 subject fallback 可独立回滚，不删除已保存快照。
- 降级列为增量 schema，代码回滚时保留该列。
- UI 枚举与提示可独立回滚；后端 `douban_snapshot` 兼容字段保持不变。
