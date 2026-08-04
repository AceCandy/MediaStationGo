# 完整媒体轨道探测与播放选择

## Goal

为每个可播放 `Media` 持久化完整、安全的容器与轨道探测结果，使 Emby 客户端能够展示视频技术信息、所有音轨和内封/外挂字幕，并在播放时选择对应轨道。

## Background

- 当前 `ffprobe -show_streams` 已读取完整流信息，但 `ProbeResult` 只保留时长、分辨率、首个视频/音频编码和容器。
- `media` 表只有单值 `video_codec` / `audio_codec` 等投影，Emby `MediaStreams` 固定最多生成一条视频和一条音频，内封字幕被忽略。
- Emby 播放请求类型声明了 `AudioStreamIndex` / `SubtitleStreamIndex`，当前播放与转码链路没有消费它们；HLS 固定映射 `0:v:0` 和 `0:a:0`。
- PlaybackInfo 的 GET query 和 POST body 当前均未解析轨道选择；HLS 请求 query 只被复制到分片 URL，不会影响 ffmpeg 选流。
- HLS 任务和输出目录当前只按 `media_id` 复用；若支持选轨，任务键必须包含影响输出的音频/字幕选择，否则不同请求会复用错误轨道。
- 已存在且文件未变化的本地媒体会在扫描时跳过，当前没有安全的全库轨道回填任务；已有单媒体同步重探入口和 PlaybackInfo 异步懒探测基础。
- 真实样本 `遮天剧场版：背棺战王腾 (2026).2160p.WEB-DL.HDR.HEVC.DDP 2.0.mkv` 包含一条 HEVC Main 10 HDR 视频和三条音频，项目当前只展示首条 AAC。

## Requirements

- 新增轻量一对一探测信息表；以 `media_id` 作为主键和外键，每个 `Media` 最多一条完整探测记录。
- 完整探测结果使用版本化 JSON 持久化，不放入 `media` 表，避免现有 `m.*` 列表查询读取大 JSON。
- JSON 保存播放与展示所需的格式、视频、音频、内封字幕、章节和轨道 disposition/tag 信息；不得保存远程签名 URL、请求 Header、Cookie、Authorization 或其他凭据。
- `media.duration_sec`、`width`、`height`、`video_codec`、`audio_codec`、`container` 继续作为兼容投影，由同一次探测同步更新。
- 本地文件、本地 STRM 目标及云媒体 HTTP 探测共用同一完整结果结构和持久化入口。
- Emby `MediaSources[].MediaStreams` 必须从持久化结果逐条映射视频、音频和内封字幕，并补齐可用的 profile、帧率、HDR、色彩、像素格式、声道、采样率、码率、语言、标题、默认/强制标记。
- 外挂字幕不持久化；详情或 PlaybackInfo 请求时实时扫描并临时合并为 Emby Subtitle stream。
- Direct Play 保留原始文件中的全部轨道，Emby 客户端必须在播放前看到完整轨道列表。
- HLS/转码必须消费客户端选择的音轨索引，不再固定映射第一条音轨。
- 内封字幕按需提取、外挂字幕实时发现，均作为 Emby 外部可选字幕流通过受控 `DeliveryUrl` 提供；Direct Play 与 HLS 由客户端叠加，不烧录视频、不生成 HLS subtitle rendition。
- 轨道索引统一使用经过持久化结果验证的 ffprobe 原始 stream index；不得混用 Emby 数组位置、音频相对序号和原始 stream index。
- HLS 任务键、输出目录和清理逻辑必须包含所有影响产物的已验证轨道选择，防止不同用户或请求互相复用错误轨道。
- 探测失败不得覆盖上一份有效完整结果；陈旧 STRM/云链接的异步结果不得写回当前媒体。
- SQLite 与 PostgreSQL 均通过现有 AutoMigrate 创建兼容结构。
- 升级后不得自动全库重探。详情或 PlaybackInfo 发现完整探测记录缺失/版本过期时异步懒补，首次响应继续使用现有标量字段安全降级。
- 管理员必须能够按媒体库手动启动批量回填；任务复用 ffprobe 并发限制，并报告总数、完成数、跳过数和失败数。

## Acceptance Criteria

- [ ] 多视频/音频/内封字幕 ffprobe JSON 能无丢轨解析、持久化并按 stream index 原序读回。
- [ ] 探测记录与 `media` 一对一；硬删除媒体后记录级联删除，普通列表查询不读取完整 JSON。
- [ ] 持久化 JSON 不含输入 filename、远程 URL、Header、Cookie、Authorization 或 token。
- [ ] 真实样本返回一条 3840x2160 HEVC Main 10、25fps、HDR10 视频和三条音频，不再折叠为首条 AAC。
- [ ] Emby 详情与 PlaybackInfo 对所有持久化轨道返回稳定且真实的 Index、Type、Codec 和默认/强制状态。
- [ ] 同目录新增或删除外挂字幕后，无需重新探测即可在下一次 PlaybackInfo 中体现。
- [ ] Direct Play 与 HLS 均能通过受控 DeliveryUrl 加载所选内封/外挂字幕；字幕可以关闭或切换，视频转码任务不因字幕选择重复生成。
- [ ] 客户端选择非首条音轨后，HLS ffmpeg 参数映射对应输入 stream；无效索引被拒绝或安全回退，行为有测试固定。
- [ ] 同一媒体同时请求不同音轨时，HLS 任务和输出目录互相隔离，不复用首个请求的轨道产物。
- [ ] 升级启动不会自动探测全库；首次访问缺失记录的媒体会异步生成完整探测记录，探测中/失败时接口仍能返回兼容标量信息。
- [ ] 管理员手动回填只处理缺失或版本过期的记录，遵守并发限制，并可观察完成、跳过和失败统计。
- [ ] 现有 STRM 多版本异步探测、源版本名、路径、容器和平均码率行为不回归。

## Out of Scope

- 不把外挂字幕写入数据库。
- 不把完整探测 JSON 放入 `media` 表。
- 不对探测 JSON 内容建立数据库级查询或索引。
- 不保存任何远程播放凭据或临时签名地址。

## Confirmed Decisions

- 使用独立一对一表存完整 JSON，保留 `media` 标量投影。
- 外挂字幕继续在播放/详情请求时实时扫描。
- 已有媒体采用访问时异步懒探测，并提供管理员手动批量回填；升级启动不自动探测全库。
- Direct Play 与 HLS 的字幕统一作为外部可选流交付；内封字幕按需提取，外挂字幕实时发现，不烧录、不生成 HLS subtitle rendition。
