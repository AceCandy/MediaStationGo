# 支持 Emby Jellyfin 多 Part 媒体

## Goal

让同一电影或剧集单集的多个物理 Part 被识别为一个有序播放版本，并通过现有 Emby/Jellyfin 兼容接口展示和播放；同时保持多版本 `MediaSources` 语义不变。

## Background

- 当前每个物理文件对应一条 `Media`，同一元数据下的多个文件全部作为多版本 `MediaSources` 暴露。
- 当前没有 Part 关系、`PartCount`、`AdditionalParts` 路由或跨 Part 处理。
- Jellyfin 将 Part 与 alternate version 分开：一个版本可包含主文件和 additional parts，客户端通过 `PartCount` 与 `/Videos/{id}/AdditionalParts` 获取各 Part。

## Requirements

1. 支持文件名尾部的 `cd`、`dvd`、`part`、`pt`、`disc`、`disk` 加数字或 `a-d`，大小写不敏感，并允许空格、点、横线、下划线分隔。
2. 仅当同一媒体库目录中存在至少两个基础名和 Part 类型相同、序号不重复的候选文件时建立 Part 组；单独的 `Part 1` 文件仍按普通媒体处理。
3. 每个物理文件继续使用独立 `Media` 行和路径，只持久化最小的 Part 组键与顺序；Part 数量从关系查询得出，不冗余存储。
4. Part 组与媒体版本正交：每个分辨率/来源版本可拥有自己的 Part；主条目的 `MediaSources` 只包含各版本的首 Part，不把后续 Part 当成版本。
5. 电影和剧集单集使用相同的 Part 关系；剧集原有季集识别不得回归。
6. 主条目在存在多个 Part 时输出 `PartCount`；`GET /Videos/{id}/AdditionalParts` 返回按序排列的后续 Part DTO，并兼容项目现有前缀与大小写路由。
7. 每个后续 Part 使用自己的媒体 ID、时长、媒体流、播放 URL 和播放进度；现有具体媒体流路由与进度算法保持不变。
8. 全量扫描、根扫描及文件删除后的调和必须重新计算受影响 Part 关系；现有数据通过重新扫描获得支持。

## Acceptance Criteria

- [ ] `Movie-part1.mkv` 与 `Movie-part2.mkv` 被识别为同一组，顺序为 1、2；大小写、数字和 `a-d` 规则有可运行测试。
- [ ] 单独存在的 `Movie Part 1.mkv` 不产生对外 Part 关系，也不被错误剥离为 multipart 标题。
- [ ] 不同目录、基础名、Part 类型或重复序号的文件不被错误合组。
- [ ] `S01E01-part1` 与 `S01E01-part2` 保持同一季集身份，并作为一个单集版本的两个 Part。
- [ ] 1080p/2160p 各两 Part 时，主条目只有两个 `MediaSources`，每个 source 对应各自 Part 组的首 Part。
- [ ] 主条目对当前版本输出 `PartCount=2`；AdditionalParts 接口只返回 Part 2，且其 ID 和播放 URL 指向 Part 2 的具体媒体行。
- [ ] `/emby/Videos/...`、`/Videos/...` 及现有小写兼容入口均能取得相同 Part 数据，鉴权 token 正确附加。
- [ ] Part 2 的 PlaybackInfo、视频流和进度记录使用 Part 2 的媒体 ID，不引入跨 Part 位置换算。
- [ ] 删除 Part 2 并重新扫描后，剩余文件恢复普通单文件语义；多版本、电影列表和剧集列表回归测试通过。

## Out of Scope

- 文件拼接、转码合并、虚拟总时长。
- 服务端强制播完一个 Part 后自动跳转下一个 Part。
- 跨 Part 累计播放位置或完成度换算。
- 手工合并/拆分 Part 的管理 UI。
- 非文件名规则的模糊猜测或新依赖。

## Technical Notes

- 解析规则参考 Jellyfin `Emby.Naming` 的 `VideoFileStackingRules`，但只实现本需求所需的尾部规则。
- 关键现有落点：`scanner_local_ingest.go`、`scanner_scan.go`、`library_media.go`、`emby_media_sources.go`、`emby_playback.go`、`emby_items_detail.go`、`emby_routes.go`。
- 无阻塞产品决策；标准 multipart 展示/选择已获用户确认，自动连播明确延期。
