# 实施计划：豆瓣 Cookie 数据库唯一来源

1. 后端注入与请求头
   - 调整 `DoubanProvider` 构造器和服务构建器注入。
   - 将搜索、详情和发现调用统一改为带上下文的 `setHeaders`。
   - 动态 Resolve Douban 配置，并实现启用/空值/错误时的匿名行为。
   - 验证：聚焦 Go 测试断言请求头。

2. 删除旧配置入口
   - 删除 `SecretsConfig.DoubanCookie`。
   - 删除 `.env.example` 的旧环境变量示例。
   - 统一两个 API 配置模型的 `api_key` 类型，并增加 `varchar(512)` 到 `text` 的幂等兼容迁移。
   - 验证：`rg` 确认旧符号及环境变量零命中。

3. 外部 API 页面语义
   - Douban 编辑字段、占位文案和清除确认改为 Cookie 语义。
   - 保持现有密码输入、加密、遮蔽和 `api_key` 提交契约。
   - 验证：`npm run lint`、`npm run build`。

4. 独立质量复核
   - 检查所有豆瓣出站路径均经过动态配置。
   - 检查 Cookie 不进入日志、错误或公共响应。
   - 检查未触碰当前工作树的无关任务文件。
   - 验证：聚焦 Go 测试、前端 lint/build、`git diff --check`。

5. 补齐专用移动详情与错误停批
   - 保留普通详情/剧集数的摘要降级，为批量和单媒体补齐增加移动详情专用调用。
   - 将网络/超时、HTTP 403/429/5xx、空响应、非法 JSON 和 HTTP 200 错误对象转换为可恢复上游错误；将明确 404 保留为永久错误。
   - 批量循环遇到可恢复错误后在当前条目写游标前停止，且不执行尾部游标清空；明确 404 记录后继续。
   - 调整任务详情与指标，区分完整快照刷新、无字段/海报变化、永久失败和接口异常暂停。
   - 验证：provider 与 service 聚焦测试覆盖无降级、可恢复错误停批、同条重试、404 后继续和日志分类。

6. 单媒体立即补齐
   - 暴露复用现有补齐逻辑的最小服务入口，不读取或修改批量游标。
   - 新增管理员 `POST /api/media/:id/douban-enrichment`，复用详情 ID 到 canonical metadata ID 的解析语义，并映射 400/404/429/500。
   - 详情页管理员菜单增加电影豆瓣补齐操作、pending guard、成功刷新和上游暂时不可用提示。
   - 验证：handler 聚焦测试、前端 lint/build，人工检查非电影/无豆瓣 ID 不显示入口。

7. 扩展后的最终复核
   - 确认普通刮削和剧集数仍保留摘要降级，补齐路径没有降级调用。
   - 确认可恢复上游错误条目不写快照、不推进游标、不请求后续候选，并在下次执行重试同一条。
   - 确认日志、响应和测试失败信息不含 Cookie 或上游响应正文。
   - 验证：相关 Go 测试、Web lint/build、`git diff --check`，再做独立只读复核。

## Expected Files

- `internal/service/douban.go`
- `internal/service/douban_discover.go`
- `internal/service/service_builder.go`
- `internal/service/douban_snapshot_test.go`
- `internal/model/api_config.go`
- `internal/database/schema_migration.go`
- `internal/database/api_config_migration_test.go`
- `internal/config/types.go`
- `.env.example`
- `web/src/components/APIConfigsPanel.tsx`
- `internal/handler/routes_authenticated_core.go`
- `internal/handler/media.go`（或现有媒体操作 handler 文件）
- `internal/handler/*_test.go`（聚焦单媒体路由/错误映射）
- `web/src/api/library.ts`
- `web/src/pages/MediaDetailAdminPanel.tsx`
- `web/src/pages/MediaDetailPage.tsx`
- `web/src/pages/MediaDetailPageSections.tsx`
- `web/src/pages/useMediaDetailPageState.ts`

## Rollback Point

代码可整体回滚；`api_key` 扩宽为 `text` 后无需回滚列类型，旧版本仍可正常读取。数据库中已保存的 Douban Cookie 无需删除，但旧版本不会读取。
