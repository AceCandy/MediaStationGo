# 实施计划

1. 在 `fetchTMDbMetadataRecheck` 的 Season 分支移除旧季 ID 与响应 ID 的硬比较，保留季号校验和通用响应校验。
2. 增加回归测试：旧/非法季 ID 可被响应 ID 替换，错误季号仍失败，唯一冲突仍回滚。
3. 运行 `gofmt`、`git diff --check`、目标 Go 测试和相关 package 检查。
4. 独立复核手动刷新与 Episode 路径未改变，并同步后台任务 code-spec。

## 风险点

- 不得绕过 `ReplaceIdentifierWithSnapshot`，否则 identifier 与 snapshot 可能部分更新。
- 不得删除唯一约束或在冲突时抢占另一 metadata 的 identifier。
- PostgreSQL 回归测试必须区分真实执行与因缺少 DSN 跳过。
