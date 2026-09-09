# Implementation

追加收藏/季复查：先用 dbx 比对两个查询改写，再修改对应仓库；扩展收藏直接季/整剧文件、同时间 ID 排序、库可见性与季无文件场景；隔离 PostgreSQL 执行回归及 go vet，独立复核 diff，结束停止临时容器。

1. 阅读数据库、后台任务和共享元数据规范，记录起始 diff。
2. 修改 person_repository.go、people_translation_worker.go 及对应测试，实现过滤、分页和组边界保护。
3. 修改 metadata_repository.go 及对应测试，合并快照关联并验证结果等价。
4. 隔离 PostgreSQL 配置 MEDIASTATION_TEST_POSTGRES_DSN，运行定向 repository/service 测试；skipped 不计通过。
5. dbx 实际库仅只读对比 EXPLAIN (ANALYZE, BUFFERS) 和结果，不执行 DDL/DML。
6. 独立复核任务 diff，主代理处理问题，执行 gofmt、git diff --check 和回归测试。
7. 必要时补充 database-guidelines.md 查询契约，清理本次临时服务和产物，报告验证边界，不提交。

用户已批准实施及后续角色索引迁移。增加 schema_migration.go 的一个索引定义；扩展 database_test.go 验证重复迁移、候选结果与强制 generic plan 的索引命中。
