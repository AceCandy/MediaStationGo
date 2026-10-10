# 技术设计

## 存储与边界

- `douban_subjects` 以豆瓣 ID 与实际 movie/series 类型唯一，保存原始 JSON、评分、质量、成功获取时间和失败退避。
- 搜索/榜单使用持久查询缓存，按规范化请求参数摘要定位，短有效期。
- 现有 MetadataIdentifier 保留本地身份及人工确认的 DoubanEntityKind；批量关联新表。
- 旧 provider 快照仅迁移/回滚用途；豆瓣专用读写改用新表，通用 TMDb 快照不变。
- 有效 JSON、合法评分与身份校验属于保存边界；完整资料不得被降级或旧响应覆盖。

## 获取与任务

- 在既有 DoubanProvider 中统一缓存读取和刷新，复用 Cookie/代理路由；不新增网络服务。
- 获取串行准入与请求前复查防止并发重复；等待支持取消，实际请求共享限速。
- 成功数据时间独立于字段/图片写入；失败不推进成功时间，强制人工重试绕过失败退避。
- 补齐先选本地缺项，再按中心新鲜度决定是否联网；图片失败复用已有详情。
- 新资料刷新任务分页处理到期已绑定电影/整剧；不补字段/图片，只更新资料与展示缓存。
- 展示继续本地批量读取，无逐条联网；成功刷新统一失效 Web/Emby 内容缓存。

## 兼容与迁移

- 启动 AutoMigrate 建新表后幂等导入旧豆瓣 JSON，按实际类型和质量选择，不覆盖新表已有资料。
- 保留旧表及旧行，不再继续写豆瓣副本，允许部署回滚。
- 保留原补齐任务 key、游标和历史名称；独立新任务默认关闭。

## 管理页面扩展

- 单一页面 `/admin/data-center`，管理空间导航名称“资料中心”，页面展示“豆瓣资料”。在 `appRoutes.tsx` 声明入口并继承 admin-root 权限，不另建导航清单。
- 列表固定每页 30 条，URL 保存 keyword/type/page；参数规范化、修改筛选回到第一页、请求可取消。使用既有 data-table、Select、ModalShell 和主题变量；窄屏使用信息卡片避免横向溢出。
- 列表只读取 `douban_subjects`：提取根或 subject/data 内的标题、年份，评分以独立 Rating 列为准。使用同一参数化查询计数和分页，以获取时间倒序（NULLS LAST）、稳定主键兜底；不返回大 JSON。
- 详情按共享行主键读取，返回共享身份、主要解析资料、完整原始 JSON 及时间。原始 JSON 默认折叠；没有有效 payload 的行仍可查看身份和退避时间。
- 计划新增管理员接口：`GET /api/admin/douban/subjects`（keyword/type/page/page_size），`GET /api/admin/douban/subjects/:id`，`POST /api/admin/douban/subjects/:id/refresh`。这里 id 是已入库共享行主键，不接受任意远端 URL或未入库 ID。
- 刷新先读取共享行身份，只允许 movie/series，调用 `GetEnrichmentMatchByID(WithDoubanFresh(ctx), DoubanID, EntityKind)`。成功后重新读取数据库，返回实际存储结果及本次是否降级，避免质量保护拒绝降级覆盖时误报旧完整数据已替换。
- 刷新复用 Provider 成功落库、质量/时间门控、失败退避及 Web/Emby 缓存失效。请求只影响共享资料，不调用 `EnrichFromDouban`，不下载海报，不改 canonical 元数据、绑定或子层级。
- 单条操作在页面持有 pending 状态阻止重复提交；请求失败保留界面旧资料，读回可展示更新后的重试时间。历史 season/episode 只读，禁用刷新并解释能力范围。
- 不修改数据库结构；回滚只需撤回管理路由、接口和页面，底层共享资料及此前实现继续保留。

## 证据

- `web/src/appRoutes.tsx:231` 定义 admin-root；页面导航由 manifest 投影。
- `web/src/pages/PlayerRequestLogsPage.tsx:67` 提供 URL 参数、管理列表和详情参考；`ModalShell` 负责弹窗交互。
- `internal/handler/routes_admin.go:12` 提供管理员认证组。
- `internal/service/douban.go:205`、`internal/service/douban_cache.go:23` 提供手动强制获取入口；`:251` 的现有远端详情仅支持 movie/series。
- 具体复用点和风险见 `research/management-page.md`。
