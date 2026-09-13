# 修复 TMDb 季集复查重复登记

## Goal

让季集复查队列在成功保存后收敛，避免复查自身写入的普通资料、快照或图片变更无意义地进入下一轮，同时不丢失需要展开后代或来自其他事务的真实业务变更。

## Background

- `runTMDbRecheckQueue` 在网络阶段前归并全部 `tm_db_recheck_changes.pending=true` 记录，每次成功调用计一次处理登记。
- `CommitTMDbRecheck` 在锁定季租约、元数据层级、变更行和目标租约并验证状态指纹后执行保存回调。
- 保存回调更新受 PostgreSQL 触发器监控的元数据、TMDb 标识或图片选择时，会递增目标 change revision 并重新设置 `pending=true`；该登记只能留到下一次任务归并。
- 历史运行曾有约 7.8 万到期待办；本次日志显示每轮本地归并超过 10 万次并耗时约 15 分钟，因此重复反馈具有可见运行成本。

## Requirements

- 成功的 `CommitTMDbRecheck` 必须消费同一事务内由该目标保存产生的普通自身登记。
- `expand=true` 的登记必须保留，确保整剧或季标识变化仍能有界展开直接子项。
- 保存失败、租约失效、锁竞争或快照变化时不得消费登记。
- 提交前已经发生的业务变更必须继续通过现有指纹/版本校验拒绝旧结果。
- 提交期间被 change/metadata 锁串行化到提交后的外部业务变更必须继续登记为 pending。
- 保留现有每事务最多 200 个直接子项、固定 pass cutoff、72 小时冷却、失败退避和最多三个季 worker。
- 不修改任务调度、前端、数据库结构或真实业务数据库。

## Acceptance Criteria

- [x] PostgreSQL 回归证明：成功保存触发普通目标变更后，目标 change revision 保留递增但 `pending=false`，下一轮不再为该自身写入重复归并。
- [x] PostgreSQL 回归证明：成功保存产生 `expand=true` 时仍保留 `pending=true` 和后代展开语义。
- [x] PostgreSQL 并发回归证明：提交后的外部变更仍递增 revision 并保持 `pending=true`；现有提交前锁竞争和陈旧快照测试继续通过。
- [x] 保存回调返回错误时事务回滚，原登记状态不被额外消费。
- [x] 相关 repository/service/database 定向测试通过；未配置 PostgreSQL 时明确报告未验证项，不把 skipped 当作通过。
- [x] 独立复核确认只修改 TMDb 复查提交、对应测试、任务文档及必要规范，没有覆盖工作树中的其它改动。

## Out of Scope

- 不扩大 200 条事务边界，不把登记归并改成新的批量框架。
- 不隐藏或降低五秒进度日志频率。
- 不新增迁移、配置、依赖、调度器或常驻 worker。
- 不处理与本问题无关的现有待办积压或真实数据库清理。
