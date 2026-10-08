# 完成记录

## 实现
普通收藏复用 favorites.created_at，软删恢复时刷新添加时间；红果与黄果 AI 的独立收藏表更新时间在重复写入相同状态时保持不变。NFO 在原状态表新增 nullable favorite_added_at，播放写入不修改它，由启动 AutoMigrate 创建字段，不回填历史数据。网页使用隐藏的 MediaView 收藏时间投影统一合并排序；Emby 缺省收藏排序在分页前应用。普通候选先按作品汇总收藏时间，避免重复行放大分页。

## 验证
- 临时 PostgreSQL 实测 TestFavoriteAddedOrderAcrossSources、TestNFOFavoriteAddedTimeSurvivesPlayback、TestOrdinarySeriesFavoriteAddedOrder。
- 收藏展示、类型拒绝、NFO 启动与状态隔离、红果合集、黄果 AI 层级及权限相关回归通过。
- TestHongGuoLibraryPagePlan 通过，包含新增默认收藏排序的大数据执行计划断言。
- 前端 npm run lint、npm run build 及 git diff --check 通过。
- 实现后经过独立只读审查；最终补充重复收藏记录回归并重新检查。
- 临时 PostgreSQL 已停止，临时目录已删除。

## 未验证与边界
未部署、未执行生产数据库迁移，未实测原生客户端和浏览器交互。客户端显式排序仍按原规则；新增 NFO 时间字段需随启动迁移生效。
