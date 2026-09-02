# 接入 Resin 代理池

## Goal

管理员可以为豆瓣请求选择普通代理池或 Resin 代理池，无需把 Resin 管理的节点逐条维护到普通代理池。

## Background

- 当前普通代理池加密保存管理员维护的有序代理 URL；豆瓣启用代理池后从直连开始，仅在 HTTP 400 或 `unexpected EOF` 时按顺序切换代理。
- Resin 是标准 HTTP/SOCKS5 正向代理网关，不是供调用方拉取节点列表的 API；节点健康检查、调度和熔断由 Resin 自己负责。
- Resin 数据面使用 `RESIN_PROXY_TOKEN`，与管理后台使用的 `RESIN_ADMIN_TOKEN` 不同。
- 当前 Resin V1 的 HTTP 正向代理身份格式为 `Platform.Account:RESIN_PROXY_TOKEN`；不指定 Platform 时使用 `Default`，不指定 Account 时在平台内随机路由。

## Requirements

- R1：豆瓣代理配置支持在普通代理池和 Resin 代理池之间明确选择。
- R2：普通代理池现有的保存、去重、健康检测、清理和请求切换行为保持不变。
- R3：Resin 模式只配置 HTTP 接入地址、`RESIN_PROXY_TOKEN` 和可选粘性标识；豆瓣继续遵守现有直连起步、失败切换和 Cookie 解析规则。
- R4：Resin Token、带认证信息的完整代理 URL 和上游请求 URL 不得出现在公共 API、日志或错误信息中；粘性标识不得进入日志或错误信息。
- R5：切换代理类型或更新 Resin 配置后，后续豆瓣请求无需重启即可观察到新配置。
- R6：MediaStationGo 不调用 Resin 控制面 API，不读取或同步 Resin 内部节点列表。
- R7：粘性标识留空时不发送 Account；填写时作为 Resin Account 使用。首版固定使用 `Default` Platform。

## Acceptance Criteria

- [ ] AC1：管理界面可以选择普通代理池或 Resin 代理池，并在刷新后保持选择结果。
- [ ] AC2：普通代理池模式的现有测试与行为不回归。
- [ ] AC3：Resin 模式能使用 HTTP 正向代理和 `RESIN_PROXY_TOKEN` 访问豆瓣，无需维护 Resin 内部节点。
- [ ] AC4：Resin 配置或连接失败时返回不泄露凭据的明确错误，且不会误用普通代理池。
- [ ] AC5：切换代理类型或更新 Resin 配置后，下一次豆瓣请求按新配置重新从直连开始。
- [ ] AC6：粘性标识留空时使用 Default 平台随机路由；填写后按 `Default.<标识>` 粘性路由，且标识不进入日志或错误信息。
- [ ] AC7：后端聚焦测试、前端 lint/build 和 `git diff --check` 通过。

## Out of Scope

- 不把代理能力扩展到豆瓣以外的 Provider。
- 不改动普通代理池的健康检测与清理规则。
- 不调用 Resin 管理 API，不引入节点同步、历史健康评分或自动删除机制。
- 首版不提供 Resin Platform 配置，固定使用 `Default`。
