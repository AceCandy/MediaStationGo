# 验证记录

## 实现与边界

- Emby 指定 NFO 库先分页再加载节点；标题无播放筛选用 EXISTS。
- 日期/播放筛选一次分组汇总；Latest 不计作品总数。
- Web 仅在页内统计代表文件、集数和版本；常规电影库检测改 SELECT EXISTS。
- 保留身份、权限、状态、MIN/MAX 排序差异和特殊/全局回退，无 schema/API 变更。
- 已对比 Emby API catalog：请求与响应契约未变，本任务不修改前端 catalog。
- `mergedLibraryIDs` 当前只返回传入库 ID，无实际合并展开；回归覆盖目标库和其他库隔离，不虚构合并库用例。

## 已执行

- 最终定向 service 回归通过（71.160 秒）：`TestNFOLibrary.*`、NFO 启动/层级/快照、
  `TestMovieLibraryEpisodicExists`、`TestEmbyMovieLibrary.*`、`TestEmbySeries.*`、
  `TestHongGuoLibrary.*`、`TestHongGuoEmby.*`。使用隔离 DSN 实际执行，无数据库缺失跳过。
- 较宽选择中的 repository 测试独立通过（48.512 秒）；最终 service 用例名称选择在
  repository 包中没有匹配项，不将该次空运行当作 repository 验证。
- gofmt 与 `git diff --check` 通过；没有前端业务变更，不重复前端构建。
- 临时 PostgreSQL 容器已关闭并自动移除，合成测试数据随之清理；未改生产数据。
- 按本轮范围及 finish-work 未提交检查，保留工作树等待提交，不执行归档或日志自动提交。
- PostgreSQL 17 隔离数据库，新旧完整 payload 对比通过：播放筛选、祖先 NSFW、
  hidden/allowed/locked-empty、三种排序、越界页准确总数、Latest。
- 电影多版本、Web 缺图/中文标题筛选测试通过。
- 常规电影库空库、无剧集、有剧集、其他库隔离通过，捕获 SQL 确认为 EXISTS 无 COUNT。
- 1000 作品 × 50 集 × 2 版本（十万绑定）EXPLAIN ANALYZE：
  标题候选约 222 ms、详情 12 ms；Latest 候选 205 ms、详情 12 ms；
  Web 候选 141 ms、详情 9 ms。是单次合成样本，不是生产端到端性能承诺。
- 只读独立复核提出非 LATERAL 引用 root 疑点；主线程复验标题查询实际成功。
  root 是 EXISTS 所在查询的外层相关引用，并非派生表同层 FROM 邻项，故无须改 LATERAL。

## 根因与防复发

- 类别：隐含执行计划假设和性能覆盖不足。
- 初稿相关 IN 导致候选/详情反复扫描；改按身份 JOIN 后通过扫描量断言。
- 初稿逐作品 LATERAL 状态汇总约 7 秒；改一次分组汇总后约 205 ms。
- 已在 NFO 规范沉淀页内范围和真实执行计划要求，并保留可运行回归。
- 全局混合查询和常规剧库日期聚合不在本轮范围；不扩大改动。

## 未验证

较宽的回归选择包含无关下载/扫描用例，运行约 191 秒后主动中止；不计作通过。
改用本轮浏览、状态、分页相关的定向回归。
未部署、未连接生产库、未做真实 Emby 设备交互或生产并发压测；无数据库迁移。
日期及状态筛选仍读取候选文件，耗时会随库规模变化。
