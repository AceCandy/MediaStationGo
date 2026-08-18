# 当前播放历史与统计架构

## 已确认入口

- Web 历史写入：`internal/handler/playback.go` 的 `POST /history`，使用鉴权上下文账户并调用 `PlaybackService.RecordProgress`。
- Web 另一进度入口：`internal/handler/playback_extra.go` 的 `POST /playback/:id/progress`。
- Emby 播放状态：`internal/handler/emby_playstate_handlers.go` 的 `/Sessions/Playing`、`/Sessions/Playing/Progress`、`/Sessions/Playing/Stopped`，由 `internal/handler/emby_routes.go` 与小写兼容路由注册。
- Emby 进度模型已有 `PlaySessionId`，PlaybackInfo 也返回该字段，但当前 handler 没有把它传入历史或统计服务。
- Web 播放器没有会话 ID，可使用浏览器原生 `crypto.randomUUID()`，每次打开一个播放实例生成一次。

## 历史持久化

- `internal/model/playback_collection.go` 的 `PlaybackHistory` 按 `user_id + metadata_id` 表达作品级最新状态，`media_id` 只保留具体播放版本。
- `internal/repository/history_repository.go` 的 `Upsert` 当前先查询再 Create/Save，不是原子写入。
- `internal/database/schema_migration.go` 已有部分唯一索引 `uniq_playback_histories_user_metadata_active (user_id, metadata_id) WHERE deleted_at IS NULL`；PostgreSQL 冲突目标需要携带相同谓词。
- `internal/service/playback.go` 当前用固定 `duration - 30s` 判断完成，且 `RecordProgressEvent` 仍接受客户端 `completed`。
- Emby 服务在 `internal/service/emby_user_data.go` 内独立换算并写历史，完成规则与原生入口没有统一所有权。

## Resume 与权限

- 原生继续观看已经通过播放服务加载历史和媒体；Emby Resume 在 `internal/service/emby_items_detail.go` 中另行查询，门槛仍是 `position_ms > 0`。
- 账户身份来自认证中间件；普通 API 使用当前账户，Emby 兼容路径可能显式带 `userId`。
- `/api/admin` 已统一使用 `AuthRequired + AdminRequired`，适合作为播放统计查询入口。
- 媒体可见性由现有媒体查询层负责，播放写入和 Resume 应复用该过滤，而不是只按裸媒体 ID 查询。
- `can_view_history` 仍存在于实体、API 类型和 Web 权限面板。

## 播放事件与统计

- `PlaybackHistory` 会覆盖同一账户/作品的旧状态，不能用来统计每次播放。
- `SessionTrackerService` 仅维护进程内设备状态，重启丢失，不适合做持久去重。
- 播放事件应是独立追加记录，并用数据库唯一约束保证同一账户、会话、作品只计一次。
- 为保留播放发生时的筛选语义，事件至少记录 `user_id`、`session_id`、`metadata_id`、`media_id`、`library_id` 与发生时间；`metadata_id` 是内容统计主维度，`library_id` 是播放时版本所属库的快照。

## Web 复用点

- 路由和导航权威清单是 `web/src/appRoutes.tsx`；`navigation.scope = 'viewer'` 会自动进入“观看空间”。
- `adminOnly` 同时控制导航过滤与直接路由的 `RequireAdmin` 防护。
- “我的”与“播放器日志”已有稳定顺序，统计页可用中间 order 插入，无需修改 Layout。
- 现有管理页使用原生 input/select/table 与 Tailwind/CSS 条形展示；项目没有图表依赖，也不需要新增。
- 用户和媒体库筛选可复用现有管理员用户列表与媒体库列表 API。

## 验证入口

- 后端聚焦：`go test ./internal/service ./internal/handler -run 'Playback|playback|Resume|History'`。
- PostgreSQL 迁移与并发约束：设置 `MEDIASTATION_TEST_POSTGRES_DSN` 后运行相关 database/repository 测试；缺少 DSN 时测试会明确 skip。
- 前端：`cd web && npm run lint && npm run build`。
- 收尾：`go vet ./...`、`go test ./...`、`git diff --check`；是否执行全量命令按实施阶段风险和用户批准决定。
