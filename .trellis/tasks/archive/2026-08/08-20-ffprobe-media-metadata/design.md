# 统一 ffprobe 技术元数据与播放信息：技术设计

## 1. 边界与原则

- `media` 负责媒体身份、路径、库关联、扫描指纹和 STRM 地址。
- `media_probe_metadata` 负责目标媒体的全部技术事实。完整 `probe_json` 是事实文档，
  类型化摘要是同一文档的查询投影，二者必须由同一持久化入口原子写入。
- scanner、TMDB、organizer 和播放器不得创建第二套技术事实。
- 项目尚未上线，直接删除旧列；不保留双写、影子字段或旧程序回滚能力。

## 2. 数据模型

在现有 `MediaProbeMetadata` 一对一记录上增加类型化摘要：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `duration_ms` | bigint | ffprobe `format.duration` 转为毫秒，0 表示未知 |
| `size_bytes` | bigint | 被探测目标媒体大小，0 表示未知 |
| `container` | varchar(128) | ffprobe format/container 事实 |
| `bit_rate` | bigint | ffprobe format 总码率，0 表示未知 |
| `width` / `height` | integer | 主视频流尺寸，0 表示未知 |
| `video_codec` / `audio_codec` | varchar(32) | 主音视频流编码 |
| `summary_version` | integer | 类型化摘要投影版本，0 表示尚未成功投影 |

不创建额外索引；`media_id` 主键已覆盖一对一 JOIN。`duration_ms` 避免继续使用整数秒
截断，API 仍按既有字段需要转换为秒或 ticks。

## 3. 写入与回填

```text
ffprobe
  → 校验 ProbeDocument 与源身份
  → 由同一个 projector 生成类型化摘要
  → 同一事务 Upsert probe_json + typed summary
```

- `MediaProbeService.persist` 是唯一写入口，不再更新 `media` 技术列。
- 本地、HTTP、STRM、手动探测和后台探测继续复用该入口；旧的直接 `Updates(media)`
  fallback 必须移除或改为调用共享入口。
- AutoMigrate 新增 probe 摘要列，并幂等删除 `media` 的旧技术列；持久化模型忽略同名
  API 投影字段，确保后续 AutoMigrate 不会重建旧列。
- 服务开始监听前运行幂等的摘要回填：按主键分页读取 `summary_version` 过期的 probe 行，
  使用现有 `UnmarshalProbeDocument` 完整校验后投影。有效文档更新摘要；无效文档保持
  未知并由现有懒修复/后台回填重新探测。
- 回填绝不读取旧 `media` 技术列，也不创建没有有效 probe 文档的 probe 行。

## 4. 读取投影与兼容

- `MediaViewRepository` 对列表/详情的一对一查询 LEFT JOIN probe 表，只选择类型化摘要，
  不加载 `probe_json`。`MediaView.Normalize` 将摘要映射到现有扁平 JSON 字段名，保持前端
  `duration_sec`、`size_bytes`、`container`、宽高和 codec 契约不变。
- 直接 SQL 聚合改为 JOIN probe 表并聚合类型化列；没有有效摘要的媒体按 0/未知处理。
- Organizer 分辨率比较、重复媒体排序以及其他技术字段消费者改读 probe 摘要。
- 读取完整轨道的详情/PlaybackInfo 继续批量加载并校验 `probe_json`，不在列表加载完整
  文档。
- 旧 `media` 技术列不再存在；扁平 JSON 技术字段仅由 probe 摘要投影填充。

## 5. PlaybackInfo 与播放进度

- `MediaSources[].RunTimeTicks` 从有效 probe 时长计算；不增加非标准顶层
  `RunTimeTicks`。
- `Size`、`Bitrate`、轨道继续来自 probe；真实技术容器优先来自 probe，直放 URL 后缀
  仍由可播放路径/STRM 目标确定，避免把 ffprobe format name 当作文件扩展名。
- Emby Item 详情使用同一 probe 摘要，不再读取旧主表技术列。
- Playing、Progress、Stopped 未携带 `RunTimeTicks` 时读取 probe 时长；若仍未知，返回
  成功 no-op，不写历史、事件或完成度，也不返回 400。Web/API 显式提交非法非正时长的
  原有校验保持不变。
- direct-only 行为不变；不新增转码字段、默认轨道索引或非标准顶层时长。

## 6. API 契约与发布

- 对外媒体 JSON 字段名与 PlaybackInfo 结构保持不变；仅未知时长的 Emby 进度请求从
  400 改为成功 no-op，需要同步管理员 Emby API 目录说明。
- 项目尚未上线，不为旧程序保留数据库兼容。开发数据库升级会永久删除旧技术列。

## 7. 风险控制

- 避免 N+1：列表通过一对一 JOIN 类型化摘要；PlaybackInfo 使用现有批量加载。
- 回填只解析数据库内已有安全文档，不执行 ffprobe，不阻塞在网络或媒体文件 I/O。
- 回填失败不得用旧主表或 TMDB 值兜底；保留未知并记录不含路径/URL/凭据的错误摘要。
- 工作树已有无关修改，实施和提交必须按确切文件白名单处理。
