# 修复 TMDb 复查同剧锁竞争

## Goal

避免同一整剧下不同季的复查提交因争抢共享整剧行锁而反复重排，保留不同整剧最多三季并行复查的现有吞吐和真实并发变更保护。

## Background

- `runTMDbRecheckQueue` 最多并行处理三季；单季内部在 `processTMDbRecheckSeason` 中串行处理。
- `CommitTMDbRecheck` 保存每个季/集时以 `FOR UPDATE NOWAIT` 锁定目标、父季和整剧。
- 同一整剧的不同季可被不同 worker 同时领取，它们会争抢同一整剧行；PostgreSQL 返回 `55P03`，随后被归类为 `ErrTMDbRecheckChanged` 并显示“数据变更，等待重新核对”。
- 生产日志已出现同一批目标跨轮次重复延后；失败次数保持为零，证明这是内部锁竞争而非 TMDb 上游失败。

## Requirements

- 同一整剧下不同季的复查提交必须按整剧互斥，不能因本任务自身的并行 worker 产生 `55P03`。
- 不同整剧仍可并行处理，现有三季并发上限保持不变。
- 保留 `FOR UPDATE NOWAIT` 对外部业务写入、后代展开反向锁竞争及过期租约的退让行为。
- 不改变 TMDb 请求、复查身份、冷却、重试、任务日志文案或数据库表结构。
- 使用 PostgreSQL 事务级 advisory lock，以 `series_id` 为键，只覆盖 `CommitTMDbRecheck` 的短保存事务；不引入进程内锁或新表。

## Acceptance Criteria

- [ ] 两个数据库连接并发提交同一整剧下不同季的复查结果时均能完成，不产生 `ErrTMDbRecheckChanged`/`55P03`。
- [ ] 已有“外部事务持有 metadata/change/identifier 行锁时立即退让”的回归行为继续通过。
- [ ] 不同整剧的提交不共享 advisory lock，仍可并行。
- [ ] 相关 PostgreSQL repository 测试通过；未配置 `MEDIASTATION_TEST_POSTGRES_DSN` 时明确记录未验证项，不以跳过冒充通过。

## Out of Scope

- 不修改任务日志“数据变更，等待重新核对”的展示文案。
- 不调整 worker 数量、季租约模型、重试间隔或任务调度。
- 不清理历史日志或手工改写现有待办状态。

## Technical Notes

- 预计仅修改 `internal/repository/tmdb_recheck_queue.go` 和对应 PostgreSQL 回归测试。
- advisory lock 必须在设置短 `lock_timeout` 和获取 metadata 行锁之前取得，避免把内部串行等待误判成外部锁竞争。
- advisory lock 只协调使用相同提交路径的内部事务；外部业务事务仍通过现有 `NOWAIT` 检测。
