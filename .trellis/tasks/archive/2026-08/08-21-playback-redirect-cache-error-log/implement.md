# 实施计划

1. 调整重定向解析签名和缓存键。
   - `ServeFile` 两条重定向路径传入 mediaID。
   - 更新并发、TTL、UA、失败和不同 mediaID 隔离测试。
   - 上游 500 时复用 `ffprobe.path_mappings` 本地回退，并缓存成功的本地目标。
2. 捕获并持久化错误响应正文。
   - 增加仅捕获 4xx/5xx、限长且保持 Gin writer 能力的包装器。
   - 扩展模型、DTO、PostgreSQL 幂等迁移及中间件测试。
3. 修正 Emby 流取消状态。
   - 三个错误阶段识别 `context.Canceled` 并返回 499。
   - 增加 handler 定向回归测试。
4. 消除 Web 与 Emby 播放路径的重复查询。
   - 流服务增加复用已加载媒体记录的入口，`ServeFile` 保持兼容并委托共享实现。
   - Web 使用已经完成权限校验的媒体记录直接播放。
   - Emby 直接按 concrete media ID 使用同一入口，删除视频流 handler 中的通用 ID 解析。
   - 回归可见性、scoped token、本地文件、STRM，并确认非 concrete ID 返回 404。
5. 消除 Emby 播放进度的重复完整视图查询。
   - `MediaSourceId` 有效且属于 `ItemId` 时，直接使用轻量可见 media 查询结果。
   - 无效、不匹配或旧式输入继续走现有通用解析。
   - 定向测试确认快路不查询 `metadata_identifiers` 完整投影。
6. 同步前端播放器日志详情和 Emby API 目录状态说明。
7. 验证与独立复核。
   - 运行受影响 Go 包的定向测试。
   - 运行前端 lint/build 或项目现有最小等价检查。
   - 运行 `git diff --check`，检查敏感信息、响应流兼容及未相关改动。

## 预期修改文件

- `internal/service/stream_redirect_resolver.go`
- `internal/service/stream_file.go`
- `internal/service/stream_redirect_resolver_test.go`
- `internal/middleware/request_logger.go`
- `internal/middleware/request_logger_test.go`
- `internal/model/player_request_log.go`
- `internal/service/player_request_log.go`
- `internal/database/schema_migration.go`
- `internal/handler/emby_playback.go`
- `internal/handler/media.go`
- 相关 Emby handler 测试
- `web/src/api/admin.ts`
- `web/src/pages/PlayerRequestLogsPage.tsx`
- `web/src/pages/embyApiCatalog.ts`（仅状态说明确需同步时）
- `web/src/pages/settingsGroupPlayback.ts`

## 验证限制

- 不运行全量后端测试；仅运行受影响包和具体测试。
- 不启动额外服务；使用现有测试设施验证。
