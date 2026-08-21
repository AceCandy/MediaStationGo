# 统一 ffprobe 技术元数据与播放信息

## Goal

将媒体技术信息统一由 ffprobe 探测结果负责，消除 `media` 主表与
`media_probe_metadata` 内容分裂导致的播放时长为 0、PlaybackInfo 信息不一致和
播放器进度接口 400。项目尚未上线，本任务直接删除旧技术列，使主表只保留媒体身份、
扫描和关联信息，不保留双写或回滚影子。

## Background

- 当前 `media_probe_metadata.probe_json` 已保存目标媒体的完整 ffprobe 文档，包含
  时长、大小、真实容器、码率、宽高、编码、轨道和章节。
- `media` 主表重复保存 `duration_sec`、`size_bytes`、`container`、`width`、
  `height`、`video_codec`、`audio_codec`，且 scanner、TMDB、异步 probe 等多个流程
  都可能写入，已经出现完整 probe 文档正确但主表时长被清零的分裂状态。
- PlaybackInfo 已在 `MediaSources[]` 返回 `RunTimeTicks`，但当前只读取
  `media.duration_sec`，未读取已有 probe 文档，因此当前 HTTP STRM 返回 0 并使
  Playing/Progress/Stopped 在客户端未上报 `RunTimeTicks` 时返回 400。

## Requirements

- `media_probe_metadata` 是目标媒体技术信息的唯一事实来源。
- 时长、目标媒体大小、真实容器、宽高、视频编码、音频编码和码率迁入
  `media_probe_metadata` 的类型化字段；完整 `probe_json` 继续保留轨道、章节和
  未单独投影的 ffprobe 信息。
- 删除 `media` 主表的上述重复技术列；扫描文件自身的大小、mtime、指纹和 STRM 地址仍
  属于主表扫描信息。扁平 API 字段仅作为 probe 查询投影，不映射回主表列。
- PlaybackInfo、Emby Item 详情、播放进度校验、统计、存储分析、洗版比较和前端
  媒体展示切换到新的技术元数据来源。
- PlaybackInfo 的时长继续放在标准位置 `MediaSources[].RunTimeTicks`；不增加非标准
  顶层 `RunTimeTicks`。direct-only 行为保持不变，不新增转码字段。
- 从已有有效 `probe_json` 回填类型化技术摘要，优先以 ffprobe 文档为准；迁移不能破坏
  尚未探测媒体的列表和播放错误处理。
- 没有有效 ffprobe 文档且重新探测失败的媒体，技术信息按未知处理；不得把 TMDB、
  scanner 或旧主表值写入 probe 表伪装成 ffprobe 结果。
- 时长未知时，播放器进度接口不得因 `duration must be positive` 返回 400；可安全忽略
  无法计算完成度的历史更新，但不得写入伪造时长。
- scanner、TMDB 和其他非 ffprobe 流程不得再写入目标媒体技术字段。
- 不修改播放器请求 Body 日志任务的实现，不清理工作树中的无关改动。

## Acceptance Criteria

- [ ] 已有有效 `probe_json.format.duration`、但旧主表时长为 0 的媒体，PlaybackInfo
      返回正确的 `MediaSources[].RunTimeTicks`。
- [ ] 同一媒体的 Playing、Progress、Stopped 在请求未携带 `RunTimeTicks` 时，能用
      ffprobe 时长完成记录，不再因 `duration must be positive` 返回 400。
- [ ] 媒体技术摘要与完整 ffprobe 文档存储在 `media_probe_metadata`，新探测只通过
      ffprobe 持久化流程写入这些字段。
- [ ] 所有运行时读写已切换到 `media_probe_metadata`；旧主表技术列已删除且不会被
      AutoMigrate 重建，扫描字段语义不变。
- [ ] 统计、存储分析、洗版比较、Emby API 和前端展示在迁移后保持现有可观察行为；
      未探测媒体以未知值安全返回，不伪造时长或轨道。
- [ ] 无有效 probe 文档且重新探测失败的媒体不继承旧主表技术值；PlaybackInfo 返回
      未知技术信息，进度接口兼容接受请求且不生成错误的完成度记录。
- [ ] 数据库迁移、服务层回归测试、播放器三类进度路由测试和相关前端检查通过。

## Out of Scope

- 新增转码能力或 `TranscodingUrl`。
- 为缺少 ffprobe 结果的媒体伪造时长。
- 重构与技术元数据迁移无关的扫描、刮削、播放器日志或前端页面。
