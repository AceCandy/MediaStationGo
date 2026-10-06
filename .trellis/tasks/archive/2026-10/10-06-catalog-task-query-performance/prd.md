# 资料体系任务页查询性能优化

## 目标

减少资料体系任务页待办统计和最近结果读取的无关数据扫描，保留既有统计口径、任务定义和缓存行为。

## 实现

- 待办统计仅执行未完成季/集复查计数与刮削问题计数两条 SQL。
- 复查统计先排除 done，继续复用媒体关联可见性条件；不读取变更统计。
- 刮削问题仅统计未删除媒体库中的 error/no_match 文件，不读取明细。
- 启动迁移补齐四个索引：idx_media_scrape_issues、idx_metadata_recheck_kind_id、idx_task_executions_kind_latest、idx_task_executions_kind_name_latest。
- 保留原明细列表、完整复查 summary、缓存刷新和错误处理。

## 验收结果

- [x] DBX 在同一快照下验证新旧待办总数均为 118704。
- [x] 新待办计数单次约 1.07 秒；该数据不代表整个页面耗时。
- [x] 隔离 PostgreSQL 测试覆盖媒体删除/恢复、done 排除、有效库口径、缓存行为及冷加载恰好两条 SQL。
- [x] 索引迁移首次创建、重复执行及缺表兼容测试通过。
- [x] 6 万条记录的索引计划测试通过，包含通用预编译任务查询和实际媒体库关联计数。
- [x] 独立复核及 git diff --check 通过。
- [x] 用户重启后，DBX 确认四个线上索引均 valid=true、ready=true。

## 验证范围

已运行 internal/database、internal/handler、internal/service、internal/repository 中与索引、待办缓存、刮削问题、复查列表及任务定义相关的针对性测试。隔离测试数据库已关闭并清理。

## 限制与风险

- 尚未进行重启后的登录页面完整请求计时，最终提速仍需复测。
- 数据分布及缓存可能影响 PostgreSQL 执行计划，不以隔离样本耗时保证线上耗时。
- 启动迁移使用常规建索引；新环境首次创建可能增加启动时间并暂时阻塞写入。
- DBX 的手动建索引操作曾被 SQL 权限设置拒绝；线上索引已由服务重启后的启动迁移补齐。
