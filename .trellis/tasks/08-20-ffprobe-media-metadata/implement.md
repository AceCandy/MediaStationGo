# 实施计划

## 1. Schema 与权威写入口

- [x] 扩展 `MediaProbeMetadata` 类型化摘要字段并补 PostgreSQL schema/往返测试。
- [x] 让 probe 文档到摘要的投影保留毫秒时长、目标大小、容器、码率、主流宽高/编码。
- [x] 修改 probe repository Upsert 和 `MediaProbeService.persist`，在同一事务只更新 probe
      行，不再更新 `media` 技术列。
- [x] 将本地/STRM/HTTP/手动/异步探测的直接主表写入统一到共享 probe 持久化入口。

## 2. 现有数据回填

- [x] 增加幂等、分页的 typed summary 回填，只处理有效且版本过期的 probe 文档。
- [x] 在 HTTP 服务监听前执行回填；无效文档保持未知，不复制旧 `media` 值。
- [x] 覆盖有效、无效、重复运行和不创建伪 probe 行的测试。

## 3. 读取侧切换

- [x] MediaView 一对一 JOIN typed summary，并保持现有扁平 JSON 字段契约。
- [x] 统计、存储分组、管理统计、Telegram 统计改聚合 probe typed summary。
- [x] Organizer 分辨率、重复媒体排序及其余技术字段消费者改读 probe summary。
- [x] 移除 scanner upsert、TMDB 和其他非 ffprobe 流程对技术字段的写入。
- [x] 用 `rg` 审计旧主表技术列的 SQL 与运行时字段访问，逐项确认剩余引用仅属于兼容
      模型、迁移或测试。

## 4. PlaybackInfo 与进度兼容

- [x] PlaybackInfo 首次请求即从已有有效 probe 文档返回准确 `RunTimeTicks`、Size、
      Bitrate、Container/MediaStreams，不依赖异步回写主表。
- [x] Emby Item 详情改读相同摘要/文档。
- [x] Playing、Progress、Stopped 在 body 无 runtime 时读取 probe 时长；仍未知则 204
      no-op，不写历史/事件。
- [x] 更新 Emby API 管理目录中未知时长请求的状态说明；不改变 direct-only 能力声明。

## 5. 验证与独立复核

- [x] 添加回归：有效 probe + 旧主表时长 0，PlaybackInfo 返回正确 ticks。
- [x] 添加三类播放路由回归：无 body runtime 时使用 probe；无 probe 时成功 no-op。
- [x] 验证 API JSON 字段兼容、统计聚合、STRM 目标大小/容器和洗版分辨率。
- [x] 运行相关 Go 单元测试和 `go test ./internal/...`；有
      `MEDIASTATION_TEST_POSTGRES_DSN` 时运行 PostgreSQL 迁移测试，否则明确记录跳过。
- [x] 若 Emby catalog 有改动，运行 `cd web && npm run lint`、`npm run build`。
- [x] 运行 `git diff --check`，再由独立 Trellis check 复核跨层数据流、旧列残留读写和
      未知时长行为。

## 6. 删除未上线的主表技术列

- [x] 将扁平技术字段改为非持久化 API 投影，删除 Media Upsert 的影子清零逻辑。
- [x] 幂等删除 `media.duration_sec/size_bytes/container/width/height/video_codec/audio_codec`，
      并确保 AutoMigrate 不会重建。
- [x] PostgreSQL 回归断言旧列缺失、扫描指纹列保留、重复迁移安全。

## 回滚点

- 项目尚未上线，不保留旧程序 schema 回滚；完整 `probe_json` 和类型化摘要仍保留。
