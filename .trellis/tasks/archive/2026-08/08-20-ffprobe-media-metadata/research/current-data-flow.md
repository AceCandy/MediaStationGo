# 当前技术元数据数据流

## 已确认事实

- `media_probe_metadata` 当前是一对一表，只包含 `media_id`、完整 `probe_json`、
  `schema_version` 和 `probed_at`；`probe_json` 是 `text`，不适合让列表和统计反复做
  JSON 提取。
- `media` 重复保存 `size_bytes`、`duration_sec`、`width`、`height`、
  `video_codec`、`audio_codec`、`container`。这些字段同时被 scanner upsert、异步本地
  probe、TMDB 详情和 `MediaProbeService.persist` 写入，因此没有单一所有者。
- `MediaProbeService.persist` 已能从一次 ffprobe 结果同时得到完整文档和上述技术摘要，
  适合作为迁移后的唯一写入口。
- 当前 backfill 遇到可成功反序列化的 `probe_json` 会直接跳过，因此新增类型化列后必须
  从已有文档重新投影，不能只依赖重新探测。
- PlaybackInfo 的 `MediaStreams`、`Size`、`Bitrate` 已部分读取 probe 文档，
  `RunTimeTicks` 和 `Container` 仍读取主表/扩展名；Emby Item 详情也直接读取主表摘要。
- 存储统计、全局统计和部分管理接口直接对 `media.size_bytes`、
  `media.duration_sec` 聚合；存储分类按 `media.container` 分组。
- Organizer 洗版比较直接读取 `media.width`、`media.height`；前端列表和详情依赖 Media
  JSON 暴露的旧字段名。

## 迁移约束

- 目标表需要类型化摘要列，避免把所有消费者改成解析 `probe_json`。
- 对外 JSON 字段名可以保持不变，由查询投影或非持久化字段承载，避免前端被迫同步改版。
- 必须先新增列并回填、切换读写，再删除旧列；不能在同一步先删主表字段。
- 项目使用启动时 `AutoMigrate` 加显式 PostgreSQL 迁移，没有对应的自动向下迁移；物理
  删列后旧二进制仍会查询旧列，因此不能直接回滚到旧版本。
- 有效 probe 文档优先级高于旧主表字段。没有有效文档且无法重新探测的媒体如何处理，
  属于需要用户确认的迁移风险决策。

## 关键位置

- `internal/model/library_media.go:40-50`
- `internal/model/media_probe_metadata.go:5-20`
- `internal/service/media_probe.go:180-225,373-440`
- `internal/repository/media_repository_upsert.go:186-197`
- `internal/service/emby_playback.go:279-315`
- `internal/service/emby_items_detail.go:228-273`
- `internal/service/stats.go:107-115`
- `internal/service/storage.go:76-80,113-118`
- `internal/service/organizer_directory_versions.go:210-228`
