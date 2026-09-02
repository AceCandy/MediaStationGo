# 接入 Resin 代理池：技术设计

## Architecture and Boundaries

- Resin 按 `Resinat/Resin` 当前 V1 契约作为一个标准 HTTP 正向代理网关接入，不调用其控制面 API。
- 豆瓣继续拥有代理路由策略；Resin 只替代启用代理后的代理客户端来源，不影响图片、AI 或其他 Provider。
- 保留 `use_proxy_pool` 作为总开关，新增 `proxy_pool_type`，取值仅为 `normal` 或 `resin`。历史数据缺省为 `normal`，因此升级后行为不变。
- 普通代理池的表、管理 API、健康检测和清理逻辑保持不变。

## Persistence and API Contract

在现有 Douban `api_configs` 行增加：

- `proxy_pool_type`：`normal` 或 `resin`，默认 `normal`。
- `resin_proxy_url`：仅允许无认证信息、无路径、无查询和无片段的 HTTP(S) origin。
- `resin_proxy_token`：使用现有 `CryptoService` AES-GCM 加密保存，公共 API 永不返回明文。
- `resin_account`：可选粘性标识；trim 后保存，首版固定 Resin Platform 为 `Default`。

公共投影返回代理类型、Resin 地址、Account 和 `has_resin_proxy_token`；更新接口接受对应字段。Token 留空时前端不提交以保留旧值，显式清除沿用现有 `<clear>` 约定。

兼容迁移沿用 `ensureAPIConfigColumns` 补列。`api_key` 继续只保存 Douban Cookie，不能复用为 Resin Token；`Extra` 继续保持原用途，避免凭据泄漏和无类型 JSON。

## Runtime Data Flow

```text
Douban request
  -> resolve current api_configs row
  -> use_proxy_pool = false: existing client
  -> type = normal: existing ProxyPoolService snapshot
  -> type = resin: one cached HTTP client using Resin as Proxy
  -> existing direct-first / HTTP 400 / unexpected EOF route logic
```

Resin 客户端的代理认证按 V1 生成：

- Account 为空：Basic 用户名为空，密码为 `RESIN_PROXY_TOKEN`。
- Account 非空：Basic 用户名为 `Default.<account>`，密码为 `RESIN_PROXY_TOKEN`。

使用 Go 标准库 `http.Transport.Proxy = http.ProxyURL(...)` 生成 `Proxy-Authorization`，不手工拼接认证头。Resin 客户端按 Douban 配置 revision 缓存；配置更新时关闭旧空闲连接并重建。生成的 snapshot 只含一个 Resin 客户端，因此 Resin 内部节点调度仍完全由 Resin 负责。

## Validation and Error Handling

- `proxy_pool_type` 非法时拒绝更新。
- Resin 地址非法，或选择 Resin 且地址为空时拒绝更新；错误只描述字段原因，不回显地址或 Token。
- Resin 运行时配置解密/构造失败时返回安全的配置错误，不回退到普通代理池。
- Resin Token、完整认证 URL、Douban Cookie 和上游请求 URL 不进入日志或公共响应；Account 可以返回管理界面，但不进入日志或错误信息。

## Compatibility and Rollback

- 老库的 `use_proxy_pool=true` 在新增类型缺省时继续使用普通代理池。
- 回滚代码时新增列会被忽略，普通代理池数据不受影响；数据库列无需破坏性删除。
- Resin 模式关闭或切回普通模式后，下一个请求因配置 revision 变化重新从直连开始。

## Trade-offs

- 不新增 Resin 专用表：配置只属于 Douban 单行，独立表会增加无收益的 CRUD 和同步复杂度。
- 不把 Resin 编码进普通代理列表：否则无法保留两套配置并独立切换。
- 不开放 Platform：当前需求只需要 Default，Account 已覆盖可选粘性路由。

## Risks

- 目标 Resin 实例需使用当前 V1 正向代理认证格式，并允许 MediaStationGo 所在网络访问其接入端口。
- Resin 网关自身不可达时按现有非 `unexpected EOF` 网络错误语义返回，不自动误切普通代理池。
