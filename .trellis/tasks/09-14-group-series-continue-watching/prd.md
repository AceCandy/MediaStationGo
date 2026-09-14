# 统一继续观看的剧集聚合

## Goal

保留完整的逐集播放历史，同时让 Web 与 Emby 客户端的“继续观看”对同一剧集只展示最新播放的未完成集。

## Background

- `playback_histories` 当前按 `(user_id, metadata_id)` 保存单集进度，这是完整播放历史和单集状态的正确数据来源。
- 原生 `GET /watch-history/continue`、Emby `GET /Items/Resume` 与 `Filters=IsResumable` 当前都可能返回同一剧集的多集记录。
- 本地 Episode 可通过 `MediaView.SeriesID` 获得元数据父链派生的稳定剧集身份；红果使用独立用户状态与源作品身份，不能按标题与本地媒体跨源合并。

## Requirements

- R1：播放进度写入、逐集完成状态及完整播放历史列表保持不变。
- R2：所有“继续观看”读路径在分页前按逻辑剧集聚合；同一剧集仅保留 `watched_at` 最新的未完成 Episode。
- R3：Movie 和无法归属剧集的可播放项继续按自身逻辑作品身份展示，不互相合并。
- R4：原生 `GET /watch-history/continue`、Emby `GET /Items/Resume` 和 Emby `Filters=IsResumable` 使用一致语义。
- R5：本地目录与红果目录使用各自稳定、带来源边界的剧集身份，不按标题跨目录合并。
- R6：继续沿用现有完成状态、20 秒门槛、可见性、用户隔离、最近播放排序和版本选择规则。
- R7：聚合后再应用 `limit` / 分页，避免同剧多集占满候选窗口导致返回数量不足。

## Acceptance Criteria

- [ ] 同一用户对同一剧集第 6、7 集均有未完成进度时，三个继续观看入口都只返回第 7 集（其 `watched_at` 更新）。
- [ ] 不同剧集各自返回最新未完成集，结果按最新播放时间倒序并在聚合后分页。
- [ ] 已完成记录和低于 20 秒门槛的记录不进入继续观看。
- [ ] Movie、无剧集归属项、不同用户、隐藏或不可见媒体保持原有隔离行为。
- [ ] `GET /watch-history` 仍返回第 6、7 集两条逐集历史，底层历史行不被删除或覆盖。
- [ ] Emby Resume 与 `IsResumable` 的响应结构、Item ID、进度和版本选择兼容现有客户端。
- [ ] 红果继续观看按源作品聚合最新未完成集，不与本地同名剧集合并。
- [ ] 新增针对性回归测试通过，相关现有 Go 测试通过，`git diff --check` 通过。

## Out of Scope

- 不迁移或压缩现有 `playback_histories` 数据。
- 不改变完整播放历史页面的逐集展示。
- 不实现“当前集完成后自动推荐下一集”的 Next Up 功能。
- 不按标题推断或跨本地/红果来源合并剧集。
