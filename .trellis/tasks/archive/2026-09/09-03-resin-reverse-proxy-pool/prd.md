# 调整 Resin 为统一反向代理池

## Goal

管理员在统一代理池区域选择并配置普通代理池或 Resin 代理池；豆瓣只决定是否使用代理池，启用 Resin 时通过 Resin URL 反向代理访问原豆瓣地址。

## Background

- 当前已提交的实现把代理类型、Resin 地址、Token 和粘性标识放在豆瓣 API 配置中，并使用 HTTP 正向代理。
- Resin 官方同时支持正向代理和 URL 反向代理；反向代理格式为 `/<token>/<Platform.Account>/<protocol>/<host>/<path>`。
- 用户要求 Resin 配置属于统一代理池，而不是豆瓣 Provider；普通代理池与 Resin 代理池是同一全局代理能力的两种模式。

## Requirements

- R1：统一代理池区域可以选择“普通代理池”或“Resin 代理池”。
- R2：普通模式保留现有代理列表、去重、检测和清理能力；Resin 模式要求实例地址和加密 Token，可选粘性标识，不再显示普通代理列表操作。
- R3：切换到 Resin 模式不得删除已保存的普通代理；切回普通模式后原列表仍可使用。
- R4：豆瓣 API 配置只保留“使用代理”开关，不再展示或提交代理类型及 Resin 字段。
- R5：豆瓣启用代理且全局模式为 Resin 时，将原始目标 URL 改写为 Resin URL 反向代理格式；粘性标识为空时使用默认平台随机调度，填写时使用 `Default.<标识>`。
- R6：保留现有豆瓣直连起步行为，仅在 HTTP 400 或 `unexpected EOF` 时切换到当前全局代理模式。
- R7：Token 必须加密保存，不得出现在 MediaStationGo 的公共 API、日志或错误信息中；实例地址只允许无用户信息、路径、查询和片段的 HTTP(S) origin。
- R8：已经通过上一版界面保存的 Resin 地址、Token、粘性标识和模式必须自动保留，用户无需重新输入。
- R9：更新全局代理模式或 Resin 配置后，后续豆瓣请求无需重启即可使用新配置。

## Acceptance Criteria

- [x] AC1：豆瓣编辑区只有“使用代理”开关，代理模式和 Resin 配置只出现在统一代理池区域。
- [x] AC2：统一代理池切换模式并刷新页面后，选择与配置保持不变；Resin 模式不显示普通代理列表操作。
- [x] AC3：Resin 模式能将豆瓣 HTTPS 请求改写为合法的 Resin 反向代理 URL，并保留原路径与查询参数。
- [x] AC4：空粘性标识生成默认平台随机路由路径，非空标识生成 `Default.<标识>` 路径。
- [x] AC5：普通代理池现有保存、去重、检测、清理和运行时切换测试不回归，模式切换不删除普通代理。
- [x] AC6：旧版已保存的 Resin 配置自动迁移或兼容读取，Token 始终加密且公共响应不含明文。
- [x] AC7：配置 revision 变化后 Resin 反向代理立即生效，并重新从直连路由开始。
- [x] AC8：后端聚焦测试、前端 lint/build 和 `git diff --check` 通过。

## Out of Scope

- 不扩展到豆瓣以外的 Provider。
- 不调用 Resin 管理 API，也不同步 Resin 内部节点。
- 不删除上一版数据库列，避免不可逆迁移。
- 首版仍固定使用 Resin 的 `Default` Platform。
- Resin 和域名前置反向代理的访问日志策略不由 MediaStationGo 管理；部署方需避免记录含 Token 的反向代理路径。
