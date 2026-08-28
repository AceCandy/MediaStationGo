# 技术设计

## 边界

本任务扩展单媒体详情响应和现有 `douban_movie_enrichment` 任务。列表、搜索、收藏、Emby 接口和其他 provider 补齐任务保持不变；不新增数据库表、依赖或任务定义。

## 详情页 provider 链接

`MediaView` 已从 canonical metadata identifier 投影 `metadata_kind`、`tmdb_id` 和 `douban_id`。单媒体 `MediaService.GetMedia` 在现有详情加载路径中额外附加：

- `tmdb_snapshot`：当前 metadata 是否存在 `provider=tmdb` 的详情快照。
- `douban_snapshot`：当前 metadata 是否存在 `provider=douban` 的详情快照。
- `series_tmdb_id`：仅 Season/Episode 生成 TMDb 深链时需要，来自 `SeriesID` 对应 canonical Series 的 TMDb identifier。

这些字段使用 `gorm:"-"`，只在单媒体详情服务中填充；共享列表查询不增加快照 JOIN 或 payload 读取。

前端在现有标题下方的元数据徽章行增加外链：

- 豆瓣 Movie：`https://movie.douban.com/subject/{douban_id}/`
- TMDb Movie：`https://www.themoviedb.org/movie/{tmdb_id}`
- TMDb Series：`https://www.themoviedb.org/tv/{tmdb_id}`
- TMDb Season/Episode：使用 `series_tmdb_id` 和季集号生成对应 TV 深链。

链接仅在所需 ID 完整时出现，使用新窗口并设置安全 `rel`；`✅` 只由服务端快照布尔值决定。

## 豆瓣缺失补齐

仓储候选查询保持 metadata-ID keyset 分页和唯一豆瓣 Movie 标识约束，候选分为：

1. 无豆瓣快照：立即进入现有首次补齐。
2. 有豆瓣快照：仅当 `fetched_at < now-24h`，且 canonical 缺有效 poster、简介为空或标题不含汉字时进入刷新。

刷新候选必须调用豆瓣详情接口，不能复用旧快照。完整 JSONB 快照及 `fetched_at` 在字段和图片持久化均成功后更新，作为成功路径的最后一步；请求、解析、字段、图片或快照保存失败均不推进冷却。成功保存新快照即开始下一轮 24 小时冷却，即使豆瓣仍没有提供缺失内容。

字段和图片仍使用现有安全写入语义：canonical 字段 fill-only；豆瓣海报保存为本地候选；已有选择不覆盖，仅在选择为空时提升。

## 任务指标

保留 `scanned`、`failed`、`ambiguous_skipped` 及细分字段/图片指标，并将正常结果分为：

- `updated`：实际填充字段、保存候选或提升选择。
- `unchanged`：成功刷新快照但没有用户可见数据变化。

任务摘要不再把零变化结果笼统计作“补齐”。

## 兼容与回滚

- 新详情字段为可选字段，旧客户端忽略即可。
- 不迁移历史数据；已有 `fetched_at` 直接参与冷却。
- 回滚详情投影/UI 与豆瓣候选刷新可独立进行，不影响现有快照和图片资产。

## 风险

- 豆瓣可能长期不提供某项核心数据；24 小时冷却将请求频率限制为每作品每天一次。
- Episode 的 TMDb ID 不是 Series 网站路径 ID，必须使用 canonical Series TMDb ID 生成深链，不能把 Episode ID 当作 `/tv/{id}`。
