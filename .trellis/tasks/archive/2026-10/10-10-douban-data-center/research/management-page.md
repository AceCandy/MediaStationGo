# 豆瓣资料中心页面探索

## 前端复用证据

- `web/src/appRoutes.tsx:231-236` 的 admin-root 已为子路由提供 adminOnly。新增 management scope 导航可由 `web/src/components/layoutNavigation.ts:30-45` 自动投影。
- `web/src/pages/PlayerRequestLogsPage.tsx:67-114` 和 `:126-149` 可参考 URL 查询参数、分页、错误/空状态和 selected 详情交互。
- `web/src/pages/useSearchPage.ts:85-128` 提供 AbortController 与请求序号模式；列表和详情必须取消旧请求并拒绝过期响应。
- `web/src/components/DoubanBindingDialog.tsx:25-56` 提供请求取消、pending 和重复提交保护参考，但其绑定行为不适用于本页。
- `web/src/components/ModalShell.tsx:24-83` 管理遮罩、Escape、Portal 和滚动锁；新详情弹窗直接复用。
- `web/scripts/check-douban-binding.mjs` 可参考轻量状态验证；实际页面导航和响应式检查应使用隔离 Web 服务及模拟 API。

## 后端复用证据

- `internal/handler/routes_admin.go:12-17` 的 admin 组使用 AuthRequired、activeUserRequired 和 AdminRequired，三条接口统一放入此组。
- `internal/handler/response.go:24-66` 定义 Success/Paginated 响应；Web 类型需匹配现有 interceptor 后的数据格式。
- `internal/model/douban.go:16-26` 保存共享资料和时间；无需新增表。
- `internal/repository/douban_repository.go:19` 有按远端 ID/类型 Find，尚缺按内部主键读取及管理分页。
- `internal/service/douban.go:649-691` 的 doubanMatchFromRawJSON 可解析 root/subject/data 中的标题、年份、简介等；列表标题筛选需在分页前查询 JSON 对应字段，不能分页后再过滤。
- `internal/service/douban.go:205-222` 和 `internal/service/douban_cache.go:23-25` 提供强制完整详情获取；不使用本地补齐服务。
- `internal/service/douban.go:251-258` 明确只支持 movie/series 远端详情；历史季/集行保留只读。
- `internal/repository/douban_repository.go:33-49` 保护完整质量和成功获取时间；手动刷新可能得到降级响应而保留旧完整行，必须读回共享表并正确显示本次结果。

## 实施边界和检查

- 页面只操作现存共享记录，不提供任意豆瓣 ID获取或 URL抓取。
- 查看页面不触发远端请求；单条点击更新才绕过 TTL，并复用限速、去重和错误保留。
- 用临时 PostgreSQL 验证查询/更新；前端遵守 admin manifest、URL规范化、请求取消及深浅主题。
- 基础资料中心改动已经在同一工作树，新增实现应保留这些改动及原有未跟踪 core 文件。
