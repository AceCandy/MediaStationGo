# 代理池与豆瓣请求代码路径

## 已确认事实

- `.codegraph/` 存在，定位阶段已优先使用 `codegraph explore`。
- `internal/service/proxy.go:20-49`：`NewExternalHTTPClient` 依次使用环境代理和系统代理；`NewExternalTransport` 集中定义外部连接池参数。
- `internal/service/proxy.go:52-55`：`NewInternalTransport` 将 `Transport.Proxy` 设为 `nil`，可证明真正直连的现有实现方式。
- `internal/service/proxy.go:58-114`：已有 `normalizeProxyURL`，可复用裸地址补 `http://` 的逻辑。
- `internal/service/douban.go:77-118`：Search 是 JSON GET，直接使用 `d.client`。
- `internal/service/douban_discover.go:16-72`：Discover 是 JSON GET，直接使用同一个 `d.client`。
- `internal/service/douban.go:178-259`：普通/补齐 Detail 最终进入 `requestDetailRawJSON`；HTTP `>=400` 时响应体被丢弃并生成 `douban detail: <status>`。
- `internal/service/douban.go:262-287`：400 不属于现有权限降级条件；403/JSON code 1000、404、429/5xx/网络错误各有既有分类，代理池不得改写这些语义。
- `internal/service/douban.go:490-505`：每个豆瓣 JSON 请求在发送前解析数据库配置并按条件设置 Cookie；重构后仍需保持实时解析。
- `internal/model/api_config_legacy.go:15-25`、`internal/service/api_config.go:66-212`：新管理端 APIConfig 的 APIKey 已加密并安全投影，但 `Extra` 会原样返回，不能承载代理凭据。
- `internal/model/model.go:47-78`：`AllModels` 同时注册 `APIConfig` 与历史 `ApiConfig`，两者映射同一表时共享列类型必须一致。
- `internal/database/schema_migration.go:452-466`：APIConfig 新字段使用启动兼容迁移幂等补列。
- `internal/handler/routes_admin.go:74-79`、`internal/handler/api_config.go:15-53`：现有管理员 API 配置路由和 handler。
- `web/src/api/api_configs.ts:3-35`、`web/src/components/APIConfigsPanel.tsx:9-322`：现有外部 API 类型、列表、编辑表单和豆瓣高级设置入口。

## 相关既有契约

- `.trellis/spec/backend/douban-cookie-config.md`：Cookie 只来自加密的 `api_configs.api_key`，公共响应只能掩码；Search、Detail、Discover 发送前解析当前配置。
- `.trellis/spec/backend/shared-media-metadata.md:1236-1389`：补齐 Detail 的 403/code 1000 降级、404 永久失败和其他临时失败语义必须保持。
- `.trellis/spec/frontend/discover-feed-loading.md`：Provider 失败不得破坏 Discover 的缓存和 fallback 顺序。

## 不采用的路径

- 不修改全局 `NewExternalHTTPClient`，否则会让所有 Provider 隐式接入。
- 不把完整代理 URL 放入 `APIConfig.Extra` 或通用 `settings`，因为现有列表 API 会原样返回这些值。
- 不直接复用 Telegram 的 fallback：它对所有网络错误和 HTTP `>=400` 都切换，不符合“仅精确 400”规则。
