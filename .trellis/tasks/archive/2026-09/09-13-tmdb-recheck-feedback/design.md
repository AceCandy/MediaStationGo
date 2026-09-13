# Design

## Behavior Boundary

问题位于 `MetadataRepository.CommitTMDbRecheck`：保存前已经锁住目标及其父层级的 metadata/change 行，并完成租约和指纹校验；保存回调触发的 change 更新发生在同一事务内。当前事务成功后没有消费这些已被本次结果覆盖的普通目标登记，因此它们进入下一轮。

修复仅在保存回调成功后处理当前 `job.MetadataID` 的 change：如果 revision 相对保存前增加且 `expand=false`，将 `pending` 设为 false 并清空 cursor。revision 不回退，用于保留变更历史和后续指纹边界。

## Concurrency Contract

- 保存前锁定 change 行；若外部事务先持锁，现有 NOWAIT 路径返回 `ErrTMDbRecheckChanged`，不执行保存。
- 保存前已提交的外部变更会改变 revision/fingerprint，旧结果被现有校验拒绝。
- 保存期间到来的外部变更会等待当前事务释放 change 或 metadata 行；当前事务提交后，它再递增 revision 并设置 pending，因此不会被本次条件消费吞掉。
- `expand=true` 表示该变化必须传播到直接子项，不消费；仍由现有持久游标按每事务最多 200 个子项展开。
- 保存失败使整个事务回滚，条件消费也不得生效。

## Files

- `internal/repository/tmdb_recheck_queue.go`：在成功保存回调后条件消费当前目标的普通自身登记。
- `internal/repository/tmdb_recheck_queue_test.go`：增加真实 PostgreSQL 的自身登记、expand 保留、失败回滚和提交后外部变更回归。
- `.trellis/spec/backend/background-task-execution.md`：记录成功结果消费普通自身登记、保留展开和并发事件的队列契约。

不改触发器本身：触发器无法可靠识别写入来源，增加 session flag 会扩大影响并可能漏掉后代事件。也不批量清理父层级，因为目标保存不等于已经复查全部受父级事件影响的兄弟目标。

## Rollback

代码只增加提交事务末尾的一次条件 UPDATE；回滚该语句和对应测试/规范即可恢复原行为，无数据库迁移或数据转换。
