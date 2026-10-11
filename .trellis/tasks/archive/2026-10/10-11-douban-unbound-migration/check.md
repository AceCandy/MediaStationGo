# 验证记录

- 先复现：旧迁移无法迁入无关联、改绑/类型变化资料，两个新增回归失败。
- 最终保护也先复现：快照自带 ID 但缺类型，当前绑定 ID 不同时，旧候选逻辑错误导入；增加同规范化 ID 的类型兜底限制后通过。
- 隔离 PostgreSQL 16、独立 schema，迁移三项回归通过；真实数据库不作为测试目标。
- `go test -race ./internal/database ./internal/model ./internal/repository ./internal/service ./internal/handler -run Douban -count=1 -timeout=3m` 全部通过，使用隔离 PostgreSQL 测试 DSN。
- 同五包 `go vet`、`git diff --check` 通过。
- 三次只读独立复核覆盖迁移身份、排序和游标、断言有效性及最终同 ID 保护；最终无必须修复问题。
- 质量优先级由原 complete/degraded 回归及新增 degraded/partial 回归共同覆盖；1001 条前置冲突候选验证整页跳过后仍可继续找到有效资料。
- 真实数据库以最终迁移 CTE 做 SELECT 核对：29 条候选、29 个缺失身份；未执行 INSERT/UPDATE/DELETE。此前未覆盖的 33 条中，1 条真实资料已存在，29 条可补迁，3 条仅含 r 字段无法识别。
- 启动 AutoMigrate 每次调用迁移，下一次启动更新后的服务会补迁；这次生产补迁、界面实际新增行以及旧数据删除尚未验证或执行。
- 本轮测试容器已关闭并自动删除；没有启动或关闭用户服务，既有未跟踪 core 保留。

## 根因与预防

前次验收只核对了当前有效关联对应的快照，把资料身份与媒体绑定混为一谈。规范已补充无关联、改绑/跨类型历史资料和无效分页候选的验收要求；资料中心的入库身份须由资料自身确定，绑定只用于缺字段兼容。
