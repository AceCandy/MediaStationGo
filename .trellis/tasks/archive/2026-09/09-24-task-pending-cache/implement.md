# 实施与验证

1. 增加管理员待办数量缓存 handler 和路由，复用现有查询。
2. 更新 Web 类型、页面请求与刷新状态，移除关闭弹窗自动重算。
3. 后端 PostgreSQL 隔离 schema 验证缓存、刷新、错误保留与权限；前端脚本验证请求生命周期。
4. 运行相关 Go 测试、Web lint/build、任务页面回归脚本、git diff --check。
5. 独立复核本次 diff，并更新待办统计契约。运行中的服务和配置保持原样。

## 验证结果

- `go test -race ./internal/handler -run 'TestTaskPendingCountsCache|TestTasksHandlerReturnsStableDefinitions|TestTMDbRecheckListAccessAndValidation' -count=1` 通过，使用真实 PostgreSQL 隔离 schema。
- Web `npm run lint`、`npm run build` 通过。
- 收尾时对修改的 Web 源码/脚本执行定向 ESLint 通过，`git diff --check` 通过。
- `check-task-pending-counts.mjs`、`check-task-startup.mjs` 在隔离 Vite 页面通过；`check-recheck-dialog.mjs`、`check-task-log.mjs` 回归通过。
- 独立只读复核未发现明确功能缺陷。覆盖边界：未直接构造两个同时强制刷新或后端请求取消的集成用例；刷新期间缓存读取不阻塞及浏览器取消已验证。
- 更新 `background-task-execution.md` 的待办统计契约。

## 交付边界

- 未修改数据库连接池配置、未重启运行中的后端，改动尚未部署；真实线上页面性能改善未验证。
- 本次启动的 6239 端口隔离 Vite 服务已关闭。
- 首次统计与手动刷新仍执行原始查询；缓存允许过时，重启后首次加载重新计算。
- 任务历史慢查询与其他会话的红果查询优化不属于本次改动。
