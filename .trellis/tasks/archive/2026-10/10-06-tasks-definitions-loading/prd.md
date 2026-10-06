# 任务页加载性能优化

## 目标

打开任务体系子页时，仅加载固定任务定义及动态状态，避免页面不使用的历史分页和总数查询。

## 要求

- 任务页刷新和轮询显式请求 definitions_only=1。
- 服务端跳过 ListSystem 和无体系时的全局 Snapshot，继续获取定义、当前状态和最近结果。
- 未传开关的任务接口保留原有分页行为。
- 不修改数据库，不扩大到具体任务历史查询优化。

## 验收结果

- [x] 页面两个 snapshot 请求均启用开关。
- [x] 后端针对性测试通过，确认当前与最近状态保留，普通接口仍返回历史列表。
- [x] 前端 lint、类型检查及生产构建通过。
- [x] 独立复核与 git diff --check 通过。

## 验证命令

- go test ./internal/handler -run '^TestTasksHandler' -count=1
- web: npm run lint && npm run build
- git diff --check

## 限制

未部署到运行中的服务，未测量改后在线耗时；此前 DBX 测到的约 7.2 秒历史分页查询在新页面请求路径中被跳过。

## 提交

fafd711
