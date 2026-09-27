# 作品级列表查询契约

## 1. Scope / Trigger

适用于普通、NFO、红果的 Web/Emby 作品列表、库内/跨库最近添加，以及作品收藏、人物关联列表。按请求语义复用查询，不按客户端名称或 URL 分别维护优化方案。

这是后续实现和审查的统一要求，不代表所有旧入口已完成迁移；当前差距及验收记录位于任务 `09-26-work-latest-media-added`。来源身份、排序、播放和分页的既有对外契约仍须保留。

## 2. Signatures

- Emby `Items(ctx, ItemsParams)`：`ParentID`、`IncludeItemTypes`、`Filters`、`SortBy/SortOrder`、`StartIndex/Limit` 和 `Fields` 决定查询需求；返回 `Items/TotalRecordCount/StartIndex`。
- Emby `LatestItems(ctx, userID, parentID, limit, isPlayed, fields...)`：最近添加数组，不对外返回总数；内部也不应仅为复用计数列表而额外计算总数。
- Web `ListLibraryMetadataPage`、`ListRecentLogicalWorks`：遵守各自原有排序和代表文件规则，不因复用 Emby 查询而改变输出。
- 复用已有 `latest_media_added_at`、`FilterWorkLibraries`、`PlaybackStates` 及各来源页内节点/版本加载器；不引入通用查询框架或新的持久计数。

## 3. Contracts

1. Movie/Series 请求优先从作品及其持久库归属产生轻量候选。不能只为判断“有文件”或取得作品 ID，先展开全库 Media、季集、版本和展示关联后去重。
2. 最新文件时间仅用于其对应排序；不能替换 DateCreated、上映日期、播放时间，或依赖特定代表版本的排序。时间非空不证明当前库/用户有可见文件。
3. 库归属是成员预筛，不是权限或状态缓存。未知 `library_ids IS NULL` 继续进入原资格检查；NFO 复用单库身份。跨库时间与当前库可见成员范围分开，红果保留无文件首季代表和跨季合集时间。
4. 文件存在性使用受作品身份约束的资格查询；用户、库、NSFW、文件属性与有效播放筛选必须在计数和分页前生效。保留普通分集先筛再聚剧等既有差异，不能把部分父节点当完整容器状态。
5. 筛选后只统计逻辑候选；计数和分页避免重复生成昂贵候选，并保留越界页准确总数。不需要总数的请求不计数。不能以需要准确总数为由加载全库展示数据。
6. 分页后才批量读取海报、简介、展示用已看/未看集数、文件版本及其他详情。父子混合身份须去重文件；限定内层绑定范围，不能仅依赖最外层 `id IN (...)`。
7. Season/Episode、断点续播、文件属性筛选和文件依赖排序按真实语义处理；只为必要的资格/排序读取相关文件，不一律改成作品最近添加。已验证的索引搜索不退回数据库全库枚举。
8. 作品优先是查询职责边界，不是强制数据库逐作品相关探测。空目录、统计滞后或稀疏命中可能使该计划更慢；保留行为正确且有执行计划证据的实现，禁止盲加 OFFSET、物化或强制规划器配置。
9. 汇总已看身份集合时优先使用 `CompletedPlaybackStates`：显式已看直接保留，未显式完成且有断点的记录才交给 `PlaybackStates` 推导。NFO Latest 等逐身份资格检查保留原有 `PlaybackStates` 索引探测，避免集合 UNION 导致短页扫描全部状态。不能简化为只读原始 completed，也不能为了布尔筛选计算全部续播展示列。

当前实现边界：全局 Movie/Series、普通 Emby 计数页/混合电影库、Web 电影页已接入；无库 Latest 保留原默认 Movie/Episode 层级但不额外计数。Web 普通剧库的分集日期排序仍复用一次受限文件范围，页后才做展示统计。普通全局直接绑定整剧/季与分集的既有祖父分组保持兼容，不能在性能修改中合并而悄然改变总数。

## 4. Validation & Error Matrix

| 情况 | 必须结果 |
| --- | --- |
| 受限空库权限、全部隐藏、无合格文件 | 空结果；计数接口总数为 0 |
| 前排作品不满足权限或状态 | 继续检查后续候选，不在资格前截断 |
| 越界页 | 空 Items，保留准确 TotalRecordCount |
| 作品跨 A/B 库、A 删除最后文件 | A 不再展示，B 保留；时间按既有全局规则 |
| 同作品多版本/分段、红果多季 | 保持逻辑身份、去重、原代表和排序 |
| 查询失败 | 返回原错误，不伪装成功空页 |

## 5. Good / Base / Bad Cases

- Good：Movie/Series 候选按请求筛选排序，分页后仅为选中作品生成详情；Latest 复用资格规则但不计总数。
- Base：DateCreated 必须按原文件范围计算时，只生成所需排序值，不顺带统计封面角标或展开全层级。
- Bad：某 APP 调用 Latest 已优化，另一个 APP 调用 Items 却重复实现全库文件展开；或为复用最快路径而改变后者排序/层级。

## 6. Tests Required

- 保存改动前查询作 oracle，比较完整 IDs、顺序、总数与关键 payload；覆盖三来源、库内/跨库、显式/默认类型、不同排序、收藏/人物/播放筛选、空页、权限和多版本。
- 复用 `TestEmbyGlobalBrowseSingleCandidateQuery`、`TestEmbyLatestCandidatePlans`、`TestHongGuoLibraryPagePlan`、`TestNFOLibraryPagePlans` 和 Web 库页/最近添加测试；新增消费者必须加入调用入口矩阵，不能只测 helper。
- PostgreSQL 实际计划验证候选扫描、`(rows + filtered) * loops`、物化次数、页内绑定范围及完整 payload 查询。包含大小库、稀疏命中、空目录和统计滞后；旧计划已证明更快的分布不能丢失。
- SQL 条数不增加、响应只含一页、使用了索引，都不是独立的性能验收。记录合成计划/服务方法/生产 HTTP 各自边界，不以一个入口的毫秒数代表全部客户端。
- 文件访问下降但耗时不降时，补采带计时的 EXPLAIN，分别查看 JIT 编译、必要日期汇总与页内详情；`TIMING OFF` 下缺失的 JIT Timing 不是“编译耗时为零”。性能基准与其它数据库测试分开执行，不把并行负载结果作严格前后对比。

## 7. Wrong vs Correct

Wrong：`全库文件 × 层级/展示关联 → 去重作品 → count/page → 再查全库详情`。

Correct：`作品候选 → 原资格及所需排序值 → 逻辑 count/page（按需）→ 当前页详情`。

Wrong：看到 SQL 中有 Media 就全部替换，或把所有日期排序改为最新入库时间。

Correct：先区分文件是否用于必要资格/排序、当前页展示还是冗余候选枚举；保持协议结果，用执行计划确认改动收益。
