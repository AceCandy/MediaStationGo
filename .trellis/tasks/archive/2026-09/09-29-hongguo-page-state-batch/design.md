# 设计

在 `hongGuoLibraryNodes` 复用现有 `page_works`，按页内 source_id 关联 `CompletedPlaybackStates`（纯 Series）或 `PlaybackStates`（含电影）。沿用候选代码已有的源作品 LATERAL 边界，将结果物化为 `page_states` 一次。可见文件先按作品和分集归并多个版本，物化为 `page_files`；两个页内集合按 source_id + COALESCE(episode number, 1) 左连接，再生成每作品 `file_stats`。只物化状态而直接连接原文件，实测仍会逐文件遍历状态，必须保留分集集合边界。

不改变文件可见性、COUNT DISTINCT、BOOL_AND、最早文件日期及末端卡片/收藏/封面聚合。状态函数和调用签名不变，无迁移、持久化计数或缓存。修改范围是当前共享红果库卡片详情函数，不扩展到其它查询。

风险：普通 JOIN UNION 可退化为扫描全历史；页内状态可能被重复物化/遍历。因此保留源作品参数化范围并检查真实 rows/loops。Media/episode 的必要读取仍保留，本轮不保证 HTTP 固定耗时。回滚只需还原本函数和测试/规范增量。
