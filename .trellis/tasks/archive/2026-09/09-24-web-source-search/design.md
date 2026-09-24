# 设计

MediaService 的分组、分页和非分页搜索收敛到同一个内部搜索页函数。先用现有 HongGuoRepository 搜索可见红果候选；有命中时与普通/NFO 候选统一排序，最终页才加载代表文件。无红果命中或空关键词保留原仓储查询。

复用已有排序实现，为网页显式保留简介和类型字段；Emby 继续使用标题排序入口。普通/NFO 候选字段批量查询增加这两个已有字段。

FindMetadataSearchRepresentatives 扩展红果分流。红果按当页逻辑身份批量选可见代表文件，复用 hongGuoPresentations 得到首季资料，保留真实文件 ID、来源 ID 与库信息。无共享 metadata 写入，无新索引、依赖或前端结果类型。

改动边界为 media_search.go、media_search_candidates.go、media_search_ranking.go、media_search_repository.go、hongguo_series.go、web/src/utils/groupSeries.ts 及对应回归测试。红果卡片用官方合集/来源身份去重；电影即使放在短剧目录也链接文件详情，避免新接入的搜索结果被目录启发式合并或跳错页面。通过相同输入的 Emby 回归验证排序默认行为不变。撤销这些代码即可回滚，无数据迁移。
