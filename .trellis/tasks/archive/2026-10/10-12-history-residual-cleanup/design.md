# 现行契约作为唯一基线

目录仓库删除 hasLibraryRootsTable 分支并直接操作当前表；服务移除 ensureLibraryRoots 及其两个调用，不修改 Library.Path 的主目录投影。测试夹具必须建立当前 schema，不能借助缺表回退。

扫描、增量摄取、监听、文件管理器和图片路径提供器只消费真实 roots；无 root 时不再从 Path 构造内存目录或授权范围。路径规范化只更新真实 root 及首目录投影，删除无 root 分支。流程测试显式写入 root；无 root 负例保留独立创建库的方式，不修改生产 Create 的含义。

旧 Vue system/config、settings/schema、system/events/ticket 以及替代调度入口撤销路由和对应 handler；SSEHub 保持订阅与广播，只删除无消费者的票据字段/方法。当前 /admin/settings、/events、/admin/scheduler 与任务定义接口保持。

剧集解析只处理当前 canonical/source 身份，删除通过 LibrarySeriesMetadataIDs 枚举作品并匹配旧哈希的分支与专用仓库查询。内部尚有用途的哈希分组不等同于旧 URL 兼容，必须核验全部链接生产者后再决定涉及的调整。非法旧输入按既有空结果行为处理。

红果历史整理工具已只读确认无目标行，删除 CLI、专有仓库实现、专有测试和现行规范中的操作承诺；保留匿名 App 目录扫描与当前下载队列/作品摘要。

仅删除相关兼容链，不调整资料中心数据归属、来源隔离、播放/权限逻辑或数据库业务内容。变更可通过回退代码恢复，生产数据库不做写入。
