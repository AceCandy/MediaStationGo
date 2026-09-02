# 调整 Resin 为统一反向代理池：技术设计

## 边界与数据流

```text
代理池面板
  -> GET/PUT /api/admin/api-proxy-pool/config
  -> ProxyPoolService
  -> 兼容复用 api_configs[douban] 的现有代理字段（Token 为 AES-GCM 密文）

豆瓣“使用代理”
  -> api_configs.use_proxy_pool
  -> DoubanProvider
  -> 全局代理池配置
     -> normal：现有 http.ProxyURL 客户端列表
     -> resin：将原目标 URL 改写为 Resin URL 反向代理地址
```

## 全局配置契约

- 保留现有 `/api/admin/api-proxy-pool` 条目接口，避免把数组响应改成对象而破坏兼容性。
- 新增 `GET/PUT /api/admin/api-proxy-pool/config`，字段为：
  - `proxy_pool_type`: `normal` 或 `resin`
  - `resin_proxy_url`: 无凭据的 HTTP(S) origin
  - `resin_proxy_token`: 仅 PUT 可选输入；留空时保留现值
  - `resin_account`: 可选粘性标识
  - `has_resin_proxy_token`: 仅公共响应
- 暂时复用上一版已创建的 `api_configs[douban]` 代理字段作为物理存储；通用 `settings` 会被现有管理接口完整列出，不得用于保存该密钥。
- `ProxyPoolService` 是全局配置的唯一运行时所有者，并维护独立 revision；配置保存后 revision 递增。
- Resin 模式要求地址和已有 Token，避免空 Token 形成可能被域名前置代理归一化的双斜杠路径。

## 旧版兼容

- 不删除 `api_configs` 中上一版增加的 Resin 列，也不做不可逆 schema 变更。
- 新接口通过 `ProxyPoolService` 读写旧字段；Token 输入留空时保留现有密文。
- 新 UI 不再通过豆瓣 API 接口读写代理类型和 Resin 字段；物理存储位置只是兼容实现细节。
- 等代理能力扩展到其他 Provider 时，再单独迁移到专用全局配置表；本任务不提前新增表和不可逆迁移。

## Resin 反向代理改写

- 原请求 URL 由一个集中 helper 改写，覆盖 Search、Discover、Detail 和 enrichment 的共同请求入口。
- 空粘性标识：`<origin>/<escaped-token>/./<scheme>/<host>/<escaped-path>?<query>`。
- 非空粘性标识：身份段为 `Default.<account>`，整段进行 path escaping。
- 使用目标 URL 的 `EscapedPath` 并原样保留 `RawQuery`，避免破坏已编码路径和查询参数。
- Resin route 使用普通 HTTP client 访问 Resin origin，不设置 `Transport.Proxy`；Resin 自己负责节点调度。
- 对 `url.Error` 复制并把 URL 替换为 `[redacted-url]`，既保留底层错误分类，也避免 Token 路径进入日志或任务错误。

## 路由行为

- `use_proxy_pool=false` 保持现有请求行为。
- `use_proxy_pool=true` 仍从直连开始，仅在精确 HTTP 400 或 `unexpected EOF` 时切换。
- 全局类型为 `normal` 时复用现有普通代理顺序与粘性路由。
- 全局类型为 `resin` 时只尝试一个 Resin 反向代理 route，不回退到普通代理列表。
- 全局配置 revision 或普通代理 generation 改变时，下次请求重新从直连开始。

## 前端

- 豆瓣编辑区只保留“使用代理”复选框。
- `ProxyPoolPanel` 加载全局配置并显示模式选择。
- 普通模式显示现有列表、保存、检测和清理操作。
- Resin 模式显示地址、Token 和粘性标识；隐藏但不删除普通条目。
- Token 输入留空不提交，公共响应仅用 `has_resin_proxy_token` 显示已配置状态。

## 回滚

- 回滚产品代码后，旧 `api_configs` 字段仍在，上一版正向代理实现仍可读取。
- 没有新增配置表或设置行；回滚无需转换数据。

## 部署风险

- Resin URL 反向代理协议把 Token 放在路径中。MediaStationGo 会脱敏自身错误，但 Resin、Nginx、CDN 等外部访问日志仍可能记录该路径；部署侧需关闭或脱敏对应 access log。
