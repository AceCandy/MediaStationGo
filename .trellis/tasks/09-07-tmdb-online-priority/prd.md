# TMDB 在线信息优先

## 目标
- 电影和剧集匹配同一 TMDB 条目后，在线有效字段及关联编号优先。
- 在线缺失字段保留已有值，本地 NFO 可补充；请求失败不覆盖。
- 旧辅助来源编号不阻断同一 TMDB 剧集的关联和重试；不同 TMDB 编号仍拦截。

## 范围
共用刮削写入、已有元数据复用、剧集分组同步及对应回归测试。不修改生产数据、NFO、部署或 NFO-only 行为。

## 验证
PostgreSQL 隔离 schema 回归：电影/剧集在线覆盖、缺失保留、旧 NFO 重试、剧集辅助 ID 冲突恢复与主 ID 冲突拒绝。修改后独立检查 diff。

## 验证记录
- 核心 PostgreSQL 回归通过：TestTMDbOnlinePriorityMoviesAndSeries、TestSeriesInventoryBindsOwnEpisodesAndReusesSnapshot。
- 扩展 EnrichOne / NFOOnly 等回归存在旧失败：测试缺 media_probe_metadata 表、时间精度断言及旧 episode fixture。已在 HEAD 的独立临时副本对照，失败项一致。
- 未部署、未修改生产数据或本地 NFO；现有失败任务需要在更新运行版本后重试。
