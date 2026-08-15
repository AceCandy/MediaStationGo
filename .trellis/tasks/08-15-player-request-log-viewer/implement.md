# 实施计划

1. 数据库模型与分区迁移
   - 新增不嵌入 `Base` 的播放器请求日志模型和专用 DTO。
   - 增加幂等父表、复合主键、索引、当前月/下月分区创建逻辑。
   - 验证：PostgreSQL 隔离 schema 中重复迁移成功，并发确保分区不冲突，UTC 跨月数据进入对应分区。

2. 持久化与查询
   - 新增仓储的写入、按月分页和路径/方法/状态码过滤。
   - 在写入前通过单一入口处理路由模板、Path 参数、Header、Query 的脱敏与大小限制。
   - 验证：仓储集成测试覆盖排序、分页、过滤、月份裁剪；单元测试覆盖全部敏感键、4096 字符单值及 64 KiB 对象截断标记。

3. 播放器请求接入与实时通知
   - 扩展现有全局 `RequestLogger`，仅对已标记的 Emby 双前缀请求在响应完成后写库。
   - 成功后广播无详情的 `player_request_log_changed` SSE 事件；失败仅告警。
   - 验证：中间件测试覆盖 `/emby` 与根路径双前缀、普通 API 边界、最终状态/耗时、原始 URL 不入库、写入失败不改变响应、SSE payload 不含任何记录字段。

4. 管理员查询 API
   - 在现有管理员路由组注册分页查询端点并校验参数。
   - 验证：handler 测试覆盖管理员成功、普通用户拒绝、无效月份/分页/方法/状态码、响应 DTO 不泄密。

5. 观看空间页面
   - 在路由清单增加管理员专属观看空间入口和页面。
   - 实现 URL 驱动的月份/路径/方法/状态筛选、分页、响应式列表和详情弹窗。
   - 为 `useSSE` 增加可选 `onOpen` 回调，收到更新或重连后查询数据库。
   - 验证：前端 lint/build；路由守卫与筛选状态；390x844、768x1024、1440x900 下暗色/亮色、键盘操作、无横向溢出和无控制台错误。

6. 独立复核
   - 核对数据库→API→类型→页面→SSE 的字段和权限链路。
   - 运行相关 Go 测试、前端 lint/build、`git diff --check`。
   - 检查没有临时文件、敏感日志样本或无关改动。

## Risky Files and Rollback Points

- `internal/database/schema_migration.go`: 分区 DDL 必须幂等，失败应阻止有缺陷的 schema 启动。
- `internal/middleware/request_logger.go` / Emby 路由注册：不得改变既有播放器响应或重复记录普通 API。
- `internal/service/sse_hub.go`: 只增加事件类型，不扩大 SSE payload 权限面。
- `web/src/appRoutes.tsx`: `adminOnly` 必须同时约束导航和直接 URL。

回滚时优先撤销应用接入和 UI；数据库表及分区保留，避免不可逆删除诊断数据。
