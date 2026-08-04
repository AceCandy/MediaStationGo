# 修复 STRM 多版本媒体信息

## Goal

同一媒体条目包含多个本地 STRM 版本时，所有缺少轨道信息的版本都能可靠进入异步探测，并让 Emby 客户端最终显示真实容器、路径和平均码率，而不是混用原始 `.strm` 信息或长期显示 `0Kbps`。

## Background

- `PlaybackInfo` 当前只对入口版本调用 `ensureTrackMetadata`，随后生成兄弟版本的 `MediaSources` 时不再触发探测（`internal/service/emby_playback.go:17`、`internal/service/emby_media_sources.go:25`）。
- 播放补探测达到并发上限时直接拒绝新任务，而底层 `FFprobeService` 已有实际并发限制，导致兄弟版本可能没有后续重试（`internal/service/emby_playback.go:109`、`internal/service/ffprobe.go:140`）。
- 条目级 `Container` 和 `Path` 直接使用原始 Media 字段，媒体源级字段则已按本地 STRM 目标修正，客户端可能同时看到 `mkv/mp4` 与 `strm`（`internal/service/emby_items_detail.go:204`、`internal/service/emby_playback.go:225`）。
- `MediaSource` 未返回 `Bitrate`，`ProbeResult` 也没有码率字段；客户端因此可能显示 `0Kbps`（`internal/service/emby_playback.go:255`、`internal/service/ffprobe.go:57`）。
- 扫描探测队列满时当前会静默拒绝任务（`internal/service/scanner_local_probe_queue.go:72`）。

## Requirements

- `PlaybackInfo` 必须为当前条目的每个缺少轨道信息的可见媒体版本安排异步探测，而不只处理入口版本。
- 播放补探测必须按媒体 ID 去重，并复用现有 FFprobe 并发限制；达到并发限制时等待执行，不得永久丢弃兄弟版本。
- 扫描阶段已确认有效的本地媒体探测任务不得因队列暂满而静默丢失。
- Emby 条目级 `Container` 与 `Path` 必须和当前媒体源的修正结果一致，不暴露本地 `.strm` sidecar 信息。
- 当媒体大小和时长均有效时，`MediaSource.Bitrate` 必须返回 `SizeBytes * 8 / DurationSec` 得到的平均总码率；信息不足时保持零值。
- `MediaSource.Name` 必须来自真实源文件名，并移除标题、年份、季集标记和扩展名；保留分辨率、片源、编码、音轨、字幕组等版本技术信息。本地 STRM 使用其真实目标文件名，不使用 sidecar 文件名。
- 探测继续异步执行，不能让详情或 PlaybackInfo 请求同步等待 ffprobe。

## Acceptance Criteria

- [x] 同一 metadata 下存在两个缺少轨道信息的本地 STRM 版本，且 `ffprobe.max_concurrent=1` 时，请求任一版本的 PlaybackInfo 后两个版本最终都完成探测并持久化。
- [x] 重复请求不会为同一媒体 ID 并发启动重复探测。
- [x] 扫描探测队列暂满时，新任务会等待可用位置并最终执行，而不是返回失败后丢失。
- [x] 本地 STRM 条目的条目级与媒体源级 `Container` 均为目标文件容器，`Path` 均不包含 backing `.strm` 路径。
- [x] 已知大小和时长的媒体源返回正确的平均 `Bitrate`；缺失任一输入时不返回错误码率。
- [x] `Dune.Part.Two.2024.2160p.WEB-DL.mkv` 显示为 `2160p.WEB-DL`。
- [x] `紫川.2024.S02E24.第24集.2160p.WEB-DL.H.265-ColorTV.mkv` 显示为 `2160p.WEB-DL.H.265-ColorTV`。
- [x] 标题与源文件语言不一致时，只要文件名包含可识别的版本技术标记，也能从首个技术标记开始显示，不残留标题、年份或季集。
- [x] 现有本地 STRM 单版本、云媒体和普通本地媒体行为不回归。

## Out of Scope

- 不新增数据库字段或迁移，不持久化精确流码率。
- 不把异步探测改为同步等待，不保证首次响应已经包含刚探测出的轨道数据。
- 不修改客户端 UI，也不改变视频版本排序规则。

## Key Decisions

- 复用现有 FFprobe limiter 负责实际并发控制；播放层的 in-flight map 只负责媒体 ID 去重。
- `Bitrate` 使用文件平均总码率，避免扩展 ffprobe 解析和数据库模型。
- 任务按轻量修复处理，仅维护本 PRD，不新增设计文档和实施文档。

## Risks and Deferred Items

- 首次进入详情时仍可能短暂看到旧信息，需要探测完成后的后续请求或客户端刷新才能更新。
- 平均总码率包含视频、音频及容器开销，不等同于视频流的精确编码码率。
