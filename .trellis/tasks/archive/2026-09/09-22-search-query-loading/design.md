# 设计

复用现有文件权限查询与资料投影。普通搜索仅增加候选 metadata 限制；NFO 使用候选层级展开、批量代表 ID 和资料加载，不改变代表排序。NFO 候选的可见性使用候选关联 EXISTS，关键词仍由现有过滤器处理。

网页保留 URL 驱动与序号防竞态，增加 AbortController，显式翻页成功后才推进页码。Emby 提示沿用 Items 的 BasicSyncInfo 字段选择。不引入依赖、缓存或新搜索索引。

影响 repository 搜索与 NFO 查询、SearchHints、网页搜索 API/hooks/results 及对应测试。MediaCard 和其他脏文件不在范围。
