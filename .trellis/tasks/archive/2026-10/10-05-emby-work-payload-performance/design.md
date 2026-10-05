# 设计

1. loadGlobalItemPayloads 的红果作品卡片调用 hongGuoPageNodes：先通过原生身份索引解析成员，沿用原权限范围，再复用 hongGuoLibraryNodes。仅返回请求身份；季集仍走旧 hongGuoItemNodes，preferredMedia 覆盖和返回 ID 顺序保持。
2. hongGuoGlobalCandidates 的电影/分集 + latest_at + 无候选状态过滤分支走 hongGuoLatestCandidates。文件按作品/分集 DISTINCT ON 日期降序 NULLS LAST，等价原 MAX；电影归并全部版本，分集要求有效所属作品。
3. 电影与分集分支投影常量 kind，避免外层类型筛选对计算字段错误估计。分集所属作品用 CASE 保留精确校验，避免将依赖的两个关联等式估算为独立选择率。
4. WorkBatchPage 仅在只读 REPEATABLE READ 事务内 SET LOCAL jit=off，防止动态候选 SQL 的编译成本超过查询；成功和回滚均恢复连接原设置。
5. legacyGlobalBatchCandidates 把标量祖父查找改为两次 LEFT JOIN，保留三个 origin UNION 分支。全局 work_batch 仅投影资格所需身份和 ordinal，保留所有原筛选/排序表达式，减少全目录窗口排序行宽。

文件边界：emby_hongguo_browse.go、emby_global_batch.go、repository/work_batch.go；直接相关服务对照/计划测试及 work-level-queries.md。
无需迁移。全部改动可通过撤销本任务 diff 回滚；不修改通用层级投影或库内 Latest 查询。
