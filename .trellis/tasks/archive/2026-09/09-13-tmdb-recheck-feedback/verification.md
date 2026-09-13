# Verification

## Result

- `CommitTMDbRecheck` 成功保存后仅条件消费当前目标在本事务中新登记的普通 change；revision 保留，`expand=true` 不消费。
- 保存错误随事务回滚；提交前外部登记由 revision/fingerprint 拒绝旧快照，提交后登记重新置 pending。
- 未调整 200 条事务边界、触发器、调度、冷却或数据库结构。

## Commands

- `MEDIASTATION_TEST_POSTGRES_DSN=... go test ./internal/repository -run '^TestTMDbRecheck' -count=1`：通过，真实临时 PostgreSQL。
- `MEDIASTATION_TEST_POSTGRES_DSN=... go test -race ./internal/repository -run '^(TestTMDbRecheckCommitConsumesOnlySelfRegistration|TestTMDbRecheckConcurrentConnections|TestTMDbRecheckQueueTransactionsAndClaims)$' -count=1`：通过。
- `MEDIASTATION_TEST_POSTGRES_DSN=... go test ./internal/service -run 'TMDbRecheck' -count=1`：通过。
- `go vet ./internal/repository ./internal/service ./internal/database`：通过。
- `git diff --check`：通过。

## Environment

- 使用无持久卷、仅绑定本机随机端口的临时 PostgreSQL 17 容器；验证后已停止并自动删除。
- 未连接或修改真实业务数据库，未启动或重启应用服务。

## Not Verified

- 未在真实媒体库运行完整任务，未测量修复后的生产归并次数和墙钟耗时。
- 未执行全仓测试或 Web 构建；本次没有前端或公共接口改动。

## Remaining Risk

- 每个成功提交增加一次命中主键的条件 UPDATE；预期成本远小于下一轮重复归并，但真实大库收益仍需部署后从任务指标确认。
