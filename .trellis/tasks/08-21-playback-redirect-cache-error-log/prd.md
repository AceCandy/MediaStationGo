# 优化播放重定向缓存与错误日志

## Goal

减少同一媒体被播放器多次 Range 请求时的重复媒体查询和上游 302 解析，并让播放器请求日志能够说明失败响应的真实原因。

## Background

- `mediaId` 在本项目中唯一绑定一条不会变化的 STRM 播放地址；STRM 地址不同即视为不同媒体 ID。
- 当前重定向解析缓存是单进程内存缓存，TTL 为 1 小时，缓存键为原始上游 URL 与 User-Agent。
- 当前播放器请求日志的 `body` 是请求正文；GET 播放失败时没有保存错误响应正文。
- `/emby/Videos/:id/stream.:container` 会对 `Item`、`PlayableMediaID` 或流服务返回的取消错误按服务端错误记录，可能产生误导性的 500/502。

## Requirements

1. 重定向解析缓存及其并发合并键改为 `mediaId + User-Agent`。
2. 原始上游 URL 只用于首次缓存未命中时解析 Location，不再参与缓存键。
3. 相同媒体 ID 与 User-Agent 在 TTL 内只解析一次上游重定向；不同媒体 ID 即使上游 URL 相同也不得共享缓存。
4. 保持现有 1 小时 TTL、失败不缓存、同键并发合并及不同 User-Agent 隔离行为。
5. 播放器请求日志仅对 HTTP 4xx/5xx 保存截断、脱敏后的错误响应正文；成功响应和媒体字节不得持久化。
6. 错误响应正文使用独立字段，不改变现有请求 `body` 的含义；字段长度与现有播放器请求正文相同，最多 64 KiB。
7. `/emby/Videos/:id/stream.:container` 的请求上下文取消统一记录为 499，不再记录为 500 或 502；真实服务端错误保持原有状态语义。
8. 播放器日志详情页面展示错误响应正文。
9. 若 Emby 播放流对外状态说明包含错误状态矩阵，同步更新管理端 Emby API 目录。
10. Web 播放器和当前 Emby 播放地址均使用具体 `media.id`；已完成可见性校验的媒体记录必须直接交给流服务，不得在 `ServeFile` 中再次查询。
11. Emby 流入口的路径 ID 必须是具体 `media.id`，不构造完整 `Item`，也不调用通用 `PlayableMediaID`。
12. Web 与 Emby 播放入口必须保留用户可见性、播放配置限制及外部播放 token 的 media scope 校验。
13. 上游 302 预解析返回 HTTP 500 时，若 `ffprobe.path_mappings` 能映射到现有普通文件，则改为本地 Range 播放。
14. 成功的 500 本地回退按同一 `mediaId + User-Agent` 缓存 1 小时；其他预解析错误仍不缓存并回退原地址 302。
15. Emby 播放进度请求同时提供作品 `ItemId` 与具体 `MediaSourceId` 时，必须直接使用已校验归属关系的 media 记录，不得为两个 ID 重复构建完整 `MediaView`。

## Acceptance Criteria

- [ ] 同一 `mediaId + UA` 连续及并发解析只产生一次上游请求，后续返回缓存目标。
- [ ] 不同 mediaId、相同 UA、相同源 URL 分别解析并缓存。
- [ ] 不同 UA 和 TTL 到期仍按现有规则重新解析，解析失败仍不会进入缓存。
- [ ] 4xx/5xx 播放器请求日志可看到脱敏后的错误响应正文；2xx、3xx 日志的响应正文字段为空。
- [ ] 超过 64 KiB 的错误响应正文被明确截断，敏感 JSON 字段不以原值落库或展示。
- [ ] `context.Canceled` 在 Emby 视频流入口的条目解析、媒体选择和流服务阶段均记录为 499。
- [ ] PostgreSQL 现有父分区表可幂等增加错误响应正文字段，新建环境也具备该字段。
- [ ] Web `/api/stream/:id` 对具体 media ID 只加载一次媒体记录，并保持可见性与 token scope 行为。
- [ ] Emby `/Videos/:id/stream` 直接按具体 media ID 播放，不调用完整 `Item` 或通用版本解析；metadata、season、series ID 返回 404。
- [ ] 已加载媒体记录进入流服务后不再执行 `Media.FindByID`，本地文件和远程 STRM 行为保持不变。
- [ ] 上游预解析 500 且本地映射文件存在时返回本地媒体内容，同键后续请求不再访问上游。
- [ ] 非 500、映射未命中或本地文件不存在时仍返回原地址 302，且失败不缓存。
- [ ] Emby 进度请求的有效 `ItemId + MediaSourceId` 快路只做轻量媒体可见性与归属校验，不查询完整 `MediaView`；无效或旧式输入保留现有兼容解析。
- [ ] 相关 Go 定向测试、前端类型检查或构建检查以及 `git diff --check` 通过；不要求全量后端测试。

## Out of Scope

- 不使用 Redis 或跨实例共享缓存。
- 不缓存整个 HTTP 302 响应，也不减少播放器自身发出的 Range 请求数量。
- 不调整一小时 TTL，不增加缓存管理接口或新的配置项。
- 不重构完整 Emby `Item` / `PlayableMediaID` 查询链本身；视频流入口不再调用该通用路径。
- 不保存成功响应正文、媒体数据、认证令牌或未脱敏隐私信息。

## Risks and Deferred Items

- `mediaId` 永久绑定同一 STRM 地址是本次缓存正确性的业务不变量；若未来允许原地修改 STRM 地址，必须同步增加缓存失效机制。
- 响应捕获必须保持 Gin `ResponseWriter` 的流式、Flush 与 Hijack 能力，且只缓冲失败正文，避免影响媒体播放。
- Emby 视频流路径 ID 以 PlaybackInfo 生成的 concrete `media.id` 为唯一契约，不保留其他 ID 的兼容解析。
