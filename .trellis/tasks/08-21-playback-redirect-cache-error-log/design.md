# 技术设计

## 边界

本次改动覆盖播放重定向解析、Emby 流错误状态、播放器请求日志持久化及详情展示。不重构通用 Emby 条目查询链，仅为播放进度中已明确的 concrete `MediaSourceId` 增加轻量快路。

### 已加载媒体播放

`StreamService` 保留按 ID 查询的 `ServeFile`，并增加接收已加载 `*model.Media` 的直接播放入口；两者复用同一内部播放实现。

- Web：`GetRawMedia` → 可见性与 scoped token 校验 → 直接播放已加载记录。
- Emby：按路径中的 concrete media ID `GetRawMedia`，完成可见性与 scoped token 校验后直接播放；不再调用 `Item` / `PlayableMediaID`。

快路径仅减少重复查找，不改变 STRM、本地文件、Range、重定向或 token 行为。

## 数据流

### 重定向缓存

`ServeFile(mediaID)` 将 `mediaID` 传给 `resolveConfiguredPlaybackRedirect`，后者再传给 `playbackRedirectResolver.Resolve`。缓存和 flight 使用 `mediaID + "\x00" + userAgent`，源 URL 仅传给首次上游 HTTP 请求。

当上游明确返回 HTTP 500 时，resolver 延迟读取既有 `ffprobe.path_mappings`，将远程 URL 映射为本地路径。只有目标是现有普通文件时才将本地回退视为成功结果并进入同一小时缓存；缓存项携带 URL/本地类型，后续 Range 直接本地播放。其他错误继续返回原 URL 且不缓存。

### 错误正文

播放器路由标记中间件在进入 handler 前替换 `gin.Context.Writer`，包装器保留原 `gin.ResponseWriter` 的全部能力。包装器在状态码达到 400 后才收集写入内容，最多保留 64 KiB；成功及重定向响应不收集。请求结束后复用现有 JSON 敏感字段脱敏逻辑，将结果写入 `response_body`。

### 取消请求

Emby 视频流入口的三个错误出口先判断请求上下文是否为 `context.Canceled`：取消返回 499；未取消则保持当前 500、404、502 行为。

### 播放进度媒体快路

`RecordProgress` 在 `MediaSourceId` 非空时先按具体 `media.id` 执行带用户可见性约束的轻量查询。当记录的 `metadata_id` 与 `ItemId` 匹配时直接生成进度目标；无法命中或归属不匹配时保留原有通用解析，避免破坏旧客户端兼容。

## 数据兼容

`player_request_logs` 父分区表增加 `response_body text NOT NULL DEFAULT ''`。模型、服务 DTO、管理 API 类型及详情展示同步增加同名字段。迁移使用 `ADD COLUMN IF NOT EXISTS`，保持重复启动安全。

## 回滚

代码可独立回滚；数据库新增文本列保留不会影响旧版本读取。无需删除列。

## 关键取舍

- 采用用户确认的业务不变量，以 mediaID 替代源 URL 作为缓存身份，不增加地址变更失效逻辑。
- 仅捕获失败响应，避免为正常 JSON、302 和媒体流分配 64 KiB 缓冲。
- 保存经过脱敏的错误正文而不是任意完整响应，兼顾诊断与隐私。
- 视频流入口严格使用 PlaybackInfo 已生成的 concrete media ID，删除无实际播放流量依据的通用 ID fallback。
- 复用现有远程 URL→本地路径映射，不新增第二套配置；仅 HTTP 500 触发播放回退，避免掩盖超时、无效 Location 等独立故障。
