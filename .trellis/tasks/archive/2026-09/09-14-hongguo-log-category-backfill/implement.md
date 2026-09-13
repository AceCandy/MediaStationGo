# 执行计划

1. 读取即将修改的红果 notice 回调和增量发现测试完整代码。
2. 先扩展现有增量发现回归测试，使其断言每个检查点通知只落盘一次，并确认修改前失败。
3. 删除 notice 更新中与 `Message` 相同的 `Details` 参数，保持指标和其他报告不变。
4. 运行最小 Go 测试：目标增量发现测试；再运行相关 task-log 测试。
5. 独立复核 diff：调用链、日志契约、无额外文件和无敏感信息。
6. 对当前配置数据库执行只读预检，确认合法分类、候选数、冲突数和剩余数。
7. 在单个 PostgreSQL 事务内完成空 work 分类回填与断言；成功后提交，失败则回滚。
8. 再次只读统计实际结果，并报告已更新、剩余和未覆盖范围。
9. 生成一次性临时 Go 命令，复用现有客户端完整扫描三个分类，仅记录空分类目标的唯一匹配。
10. 所有分类成功到尾页后，在单事务内条件更新空 work/discovery，并复核剩余数与冲突。
11. 删除临时命令，重新执行工作树与敏感文件检查。

## Validation

- `go test ./internal/service -run 'TestHongGuoDiscoveryIncrementalStopsAtSavedBoundary|TestTaskTracker'`
- PostgreSQL 事务前后聚合统计及冲突查询。
- `git diff --check`、`git status --short`。

## Rollback Points

- 数据回填安排在代码与测试验证之后；此前可直接撤销代码 diff。
- SQL 断言在 `COMMIT` 前执行，任何异常自动 `ROLLBACK`。
- 不创建永久迁移或临时导出文件。
