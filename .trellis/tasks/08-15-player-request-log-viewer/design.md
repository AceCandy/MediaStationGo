# 技术设计

## Architecture

```text
播放器请求
  → Emby 路由标记中间件
  → 业务处理器
  → 全局 RequestLogger（仅 player_api 标记时脱敏并写 PostgreSQL 月分区）
  → 写入成功后广播无内容的 SSE 更新事件
  → 管理员页面收到事件后重查分页 API
```

数据库是历史和断线恢复的唯一事实来源；SSE 只负责提示页面刷新，不承载日志详情。

## Storage Contract

逻辑表 `player_request_logs` 使用 `requested_at` 按 UTC 月范围分区。字段：

- `id`: UUID 字符串。
- `requested_at`: 请求开始时间及分区键。
- `method`, `route`, `status`, `duration_ms`, `ip`；`route` 使用 Gin 路由模板，不存原始 URL。
- `path_params`, `headers`, `query`: 脱敏后的 JSONB 对象。

主键为 `(id, requested_at)`，满足 PostgreSQL 分区唯一约束。模型不嵌入通用 `Base`，避免其单列主键与分区规则冲突。

分区表不能通过通用 `Base`/`AllModels` 自动迁移；`ensurePlayerRequestLogSchema` 由现有 PostgreSQL migration 流程使用幂等 DDL 创建父表、复合主键和查询索引。仓储在写入前按 `requested_at` 幂等确保当月及下月分区存在，分区名仅由 UTC 年月生成，并串行化进程内首次建分区操作。这样服务长期运行跨月时无需额外调度器，首个新月请求也不会因缺少分区失败。

查询必须携带月份范围，使 PostgreSQL 可以分区裁剪；排序为 `requested_at DESC, id DESC`，分页返回 `items/total/page/page_size`。

## Write Path

扩展现有全局 `RequestLogger`，仅当 Emby 双前缀路由已设置 `player_api` 标记时调用播放器日志服务。中间件在 `c.Next()` 后获取最终状态和耗时，将 `c.FullPath()` 作为路由模板，并把 Gin path params、headers、query 统一交给一个脱敏/限长入口。每个值最多 4096 个 Unicode 字符，每个 JSON 对象最多 64 KiB，超限时写入 `_truncated` 标记。随后同步执行一次数据库写入：

- 写入成功：广播 `player_request_log_changed` 空通知。
- 写入失败：Zap `Warn`，不改写已生成的播放器响应。

不增加内存队列或逐请求 goroutine，避免进程退出丢日志、队列溢出和无界并发。代价是请求结束阶段增加一次数据库写入延迟；这是保证诊断日志可靠落库的最小方案。

## API and Access

新增管理员接口：

```text
GET /api/admin/player-request-logs
  month=YYYY-MM
  page=1
  page_size=50
  path=<optional route substring>
  method=<optional HTTP method>
  status=<optional integer>
```

接口位于现有 `AuthRequired + AdminRequired` 路由组。月份、分页、方法和状态码在 handler 边界校验；路径作为参数化 `ILIKE` 条件。响应使用专用 DTO，不直接暴露数据库模型。

SSE 端点目前对所有登录用户开放，因此事件 payload 为空；普通用户最多得知“日志发生变化”，不能获得路径、IP、Header、Query 或记录 ID。

## Frontend

在 `appRoutes.tsx` 添加顶层观看空间目的页 `/player-logs`，声明 `adminOnly: true` 和 viewer navigation 元数据。路由清单同时控制导航可见性和直接访问守卫，不在 `layoutNavigation.ts` 重复权限。

页面状态：

- 月份、路由关键字、方法、状态码和页码由 URL query 管理，并规范化无效值。
- 默认当前月、第一页、每页 50 条。
- 桌面使用响应式数据表，移动端使用记录卡片。
- 详情使用现有 `ModalShell`，以只读格式展示 Path 参数、Header 和 Query。
- `useSSE` 增加可选的 `onOpen` 回调；页面收到 `player_request_log_changed` 后去抖刷新第一页，重新连接成功时也再次查询数据库。

前端定义单一 `PlayerRequestLog`/分页响应类型；页面和 SSE 处理不自行重复解析字段。

## Compatibility and Rollback

- 不修改播放器 API 路径、鉴权或响应契约。
- 保留现有 Zap `player_api=true` 日志行为；数据库日志是新增诊断能力。
- 回滚应用代码后，分区表和数据保留，不执行破坏性自动删除；后续可单独迁移清理。

## Risks

- Path 参数、Header、Query 体积可能异常：统一脱敏入口同时执行固定限长并保留 `_truncated` 标记，避免单请求放大数据库。
- SSE 可能丢通知：页面首次加载、重连和筛选变化均从数据库查询，实时事件不作为事实来源。
- 分区 DDL 失败：该条日志记录失败并告警，但播放器响应保持不变；数据库迁移测试覆盖幂等建表和跨月写入。
