# 实施与验证

1. 修改 metadata 私有待办字段、目录交接与子项筛选；验证旧状态恢复、资料完成与图片完成分离。
2. 扩展现有 TMDb 图片执行器，添加首次快照下载、头像处理、持久化退避和生命周期唤醒；验证失败/取消/并发选择保护。
3. 更新任务显示名称、必要的状态展示及规范；保留任务 key 和旧执行名称。
4. 运行定向 Go/PostgreSQL 测试及相关任务展示检查；独立复核 diff、检查未跟踪产物、关闭测试服务。

只使用隔离测试 schema；不对用户业务数据库执行任务。数据库测试未实际执行时不得把 skip 计为通过。

## 验证结果

- 已完成资料/图片交接、持久化退避与重启恢复、合并待办转移、头像来源持久化和任务显示名兼容。
- 独立静态复核及主线程 diff 复核完成；Episode 演职员沿用 Season 所有权，不新增 Episode credits。
- 隔离 PostgreSQL 上 service/repository/database 定向回归通过，覆盖 Catalog、子项交接、TMDb 图片修复、人物持久化、任务定义和迁移。
- `go test -race ./internal/service -run 'TestCatalogArtwork|TestPersistCredits' -count=1` 通过（启用隔离 PostgreSQL）。
- `node web/scripts/check-task-log.mjs` 通过；gofmt 和 `git diff --check` 通过。
- 原有两条图片修复测试使用不可解码的 JPEG 头，已在原 HEAD 复现；仅替换为有效图片 fixture。
- 未运行全仓测试、浏览器或真实 TMDb 下载测速；未部署、未提交，发布依赖正常 AutoMigrate 新字段。
