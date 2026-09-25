# 修复 Emby 红果库类型与浏览性能

## 目标

使 Emby 正确识别并能打开红果剧集库，且该库的最新媒体能及时返回；保持权限、合集身份、季集层级及播放状态过滤正确。

## 已确认事实

- `internal/service/emby_system.go:145-161` 的 `libraryAsView` 漏掉红果类型，默认对外报告 movies；实际客户端因此请求 Movie。
- `internal/service/emby_hongguo.go:45-92` 从文件绑定展开作品、季、集并聚合；`:263-265` 浏览分页前无条件 Count。实际库约 60.5 万绑定，日志中此 Count 超过 12 秒，浏览和 Latest 均出现 context canceled。
- `internal/service/emby_hongguo.go:94-123` 中存在 NFO 时，库内 Latest 走 hierarchy，DateCreated 使用 MIN 文件入库时间；无 NFO 时库内 Latest 使用 MAX 文件入库时间。全局合并使用 MAX（`internal/service/emby_hongguo_browse.go:57-68`）。
- `internal/service/emby_hongguo.go:267-280` 未识别 DateLastContentAdded，客户端请求该字段时会落到标题排序。
- `internal/repository/hongguo_series.go:43-104` 已有可见作品候选先分页、页内补充文件信息的模式，但不能直接替换 Emby 的播放状态过滤与时间排序。

## 要求与范围

- R1：红果库 CollectionType 返回 tvshows；不强制改写既有作品种类或节点 ID。
- R2：优化指定红果库的作品层浏览与 Latest，避免对全库文件展开全部层级后再分页。权限、作品类型、播放状态等过滤必须在分页前完成；特殊条件如需回退，保持结果正确。
- R3：Latest 不做不需要的总数统计。普通浏览保留准确总数。
- R4：指定红果库 Latest 及 DateLastContentAdded 按作品最近可见文件入库时间（MAX）排序，补新集的旧剧可以提前；DateCreated 保留首次入库时间（MIN）。不能随其它 NFO 库存在与否变化。用户已确认此行为。
- R5：同步 Emby API catalog；增加最小回归验证。
- 不在范围内：搜索/OpenSearch 改动、普通库或 NFO 库行为重构、全局浏览重构、新缓存、数据库迁移、部署、重启、提交归档。

## 验收标准

- [x] R1：共享库映射返回 tvshows，Series 请求能返回红果作品。
- [x] R2：根目录、合集、季、集及分页身份保持一致；隐藏库和不可见文件不泄漏，播放状态筛选不产生分页漏项。
- [x] R2/R3：查询结构只对当前页候选补全节点详情，Latest 无作品总数 Count；使用隔离测试数据验证，不以尚未测量的耗时作提速承诺。
- [x] R4：有无 NFO 库时，指定红果库 Latest 顺序一致；DateCreated 与 DateLastContentAdded 分别有明确回归覆盖。
- [x] R5：相关 Go 回归测试通过；文档同步检查完成，未执行的真实客户端检查见 validation.md。
