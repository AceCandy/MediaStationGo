# Emby 接口管理页技术设计

## Scope And Boundaries

本次仅修改 `web` 前端包。页面读取随前端构建发布的静态接口目录，不调用新后端接口，不改变 Emby 兼容层行为。

页面路由为 `/admin/emby/interfaces`，挂入 `appRoutes.tsx` 的 `admin-root` 子路由并使用 `navigation.scope = 'viewer'`，排序在“我的”之后。管理员权限和激活态仍来自同一份路由 manifest；桌面侧栏与移动端底栏都按继承的 `adminOnly` 过滤该入口。旧 `/admin/integrations/emby` 保留为管理员保护下的重定向。

## File Shape

- `web/src/appRoutes.tsx`：懒加载页面，将管理员路由投影到观看空间并保留旧地址重定向。
- `web/src/components/LayoutSections.tsx`：移动端观看空间入口按管理员身份过滤。
- `web/src/pages/AdminEmbyAPIsPage.tsx`：页面布局、筛选状态、展开状态和复制交互。
- `web/src/pages/embyApiCatalog.ts`：页面专用的接口类型、分类和静态接口目录；根 `.gitignore` 会忽略任意层级的 `data/` 目录，因此与页面同级存放。

首版不拆更多展示组件。只有页面内部结构确实影响可读性时，才提取同文件内的小组件。

## Catalog Contract

接口目录使用简单的只读 TypeScript 数据结构，核心字段如下：

```ts
type EmbyApiSupport = 'implemented' | 'compatibility'
type EmbyApiParameterLocation = 'path' | 'query' | 'header' | 'body'

interface EmbyApiEndpoint {
  id: string
  category: string
  name: string
  description: string
  method: 'GET' | 'POST' | 'DELETE' | 'HEAD'
  path: string
  aliases?: string[]
  auth: string
  support: EmbyApiSupport
  parameters: EmbyApiParameter[]
  requestExample?: string
  responses: EmbyApiResponse[]
  notes?: string[]
}
```

示例使用预格式化字符串，避免在 JSX 中混入大段对象。目录采用虚构 ID、Token 和 URL，不包含真实环境数据。多个 HTTP 方法行为相同但响应不同的场景可拆为独立条目；仅路径大小写或 `/emby` 前缀不同的场景通过页面总说明或 `aliases` 合并展示。

`support = implemented` 表示处理器会执行实际查询、播放或状态写入；`support = compatibility` 表示端点主要用于满足客户端探测并返回固定/空结构。目录只收录与播放器接入有关的兼容占位接口，不追求把所有后台管理兼容路由逐一列出。

## Content Coverage

目录按播放器接入链路组织：

1. 服务发现与认证：公开系统信息、公开用户、`AuthenticateByName`、Capabilities、Logout。
2. 用户与媒体库：当前用户、用户视图、媒体文件夹、Items/SearchHints、详情、季与集。
3. 图片与播放：图片 GET/HEAD、PlaybackInfo GET/POST、视频流 GET/HEAD、字幕、`/emby/api/stream/:id` 的 STRM/302 行为。
4. 播放状态：Playing、Progress、Stopped、FavoriteItems、PlayedItems。
5. 会话：Sessions 列表及播放器会话字段。

内容录入时逐项对照 `internal/handler/emby_routes*.go` 与对应 handler/service；响应字段仅描述当前实现实际返回的结构。

## Page Interaction

页面顶部展示标题、接口数量、统一前缀与鉴权提示。搜索框对名称、路径、描述做不区分大小写的客户端过滤；分类使用现有风格的紧凑按钮或 select，在移动端可换行。无结果时显示明确空状态。

每个接口使用单层可折叠条目：收起态展示方法、名称、路径、鉴权和支持状态；展开态按参数位置分组展示入参，并展示响应状态、字段说明和示例。代码区域保持稳定宽度并允许内部横向滚动。复制按钮使用 `lucide-react` 图标和可访问标签，成功后短暂反馈；Clipboard API 不可用或失败时只给出非阻塞提示。

首版使用页面本地 React state，不引入 store、请求层或新依赖。

## Compatibility And Rollback

新路由独立于现有“外部 API”页，原有 `/admin/integrations/apis` 和 integrations 默认跳转均保持不变。旧地址使用 `Navigate replace` 跳转到新地址。删除观看空间导航元数据、页面、目录与兼容重定向即可完整回滚，不涉及数据迁移。

## Validation

- 静态检查：`cd web && npm run lint`。
- 类型与生产构建：`cd web && npm run build`。
- 差异检查：`git diff --check`。
- 浏览器检查：管理员菜单、路由访问、搜索、分类、折叠、复制和无结果状态。
- 响应式检查：390x844、768x1024、1440x900；确认页面无整体横向滚动或控件遮挡。
- 内容抽查：以路由注册、鉴权中间件、PlaybackInfo、流媒体、进度和会话处理器为准核对核心条目。
