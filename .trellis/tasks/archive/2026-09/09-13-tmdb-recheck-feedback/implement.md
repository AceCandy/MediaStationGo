# Implementation

1. 读取 `CommitTMDbRecheck`、change 模型和现有并发测试的当前工作树内容，确认没有用户并行修改。
2. 先增加 PostgreSQL 回归：普通自身登记被消费、`expand=true` 保留、保存失败回滚、提交后外部变更保留。
3. 在 `CommitTMDbRecheck` 保存成功后，按保存前 revision 和 `expand=false` 条件清除当前目标 pending/cursor；不修改 revision 或父级 change。
4. 运行针对性的 repository 测试；如环境提供 `MEDIASTATION_TEST_POSTGRES_DSN`，确认真实 PostgreSQL 测试未 skipped。按影响再运行相关 service/database 定向测试，不执行无关全量构建。
5. 使用 `trellis-check` 做独立复核：检查规范、diff、锁顺序、失败回滚、外部并发事件和未跟踪产物。
6. 更新验证记录并汇报改动、已验证、未验证及剩余风险；不修改真实数据库、不重启服务、不提交代码。
