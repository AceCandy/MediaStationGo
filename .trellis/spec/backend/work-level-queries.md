# 作品级列表查询契约

## 1. Scope / Trigger

适用于普通、NFO、红果的 Web/Emby 作品列表、库内/跨库最近添加，以及作品收藏、人物关联列表。按请求语义复用查询，不按客户端名称或 URL 分别维护优化方案。

这是后续实现和审查的统一要求，不代表所有旧入口已完成迁移；当前差距及验收记录位于任务 `09-26-work-latest-media-added`。来源身份、排序、播放和分页的既有对外契约仍须保留。

## 2. Signatures

- Emby `Items(ctx, ItemsParams)`：`ParentID`、`IncludeItemTypes`、`Filters`、`SortBy/SortOrder`、`StartIndex/Limit` 和 `Fields` 决定查询需求；内部 `SkipTotalRecordCount` 零值保留准确计数。HTTP `/Items` 默认跳过准确计数，但始终返回数字 `TotalRecordCount`：分页多取一个合格结果，有结果时返回 `StartIndex + 取得数量` 的已知下界，空页为 0；显式 `EnableTotalRecordCount=true` 保留原准确计数。
- Emby `LatestItems(ctx, userID, parentID, limit, isPlayed, fields...)`：最近添加数组，不对外返回总数；内部也不应仅为复用计数列表而额外计算总数。
- Web `ListLibraryMetadataPage` movies and `ListRecentLogicalWorks` use persisted global work time. Movie pages order by `latest_media_added_at DESC NULLS LAST, id DESC`; representative file and Part rules remain unchanged. Other library sorts retain their existing semantics.
- 复用已有 `latest_media_added_at`、`FilterWorkLibraries`、`PlaybackStates` 及各来源页内节点/版本加载器；不引入通用查询框架或新的持久计数。
- `MediaViewRepository.WorkBatchPage(ctx, candidates, eligible, start, limit, count, countEligible...)` owns the shared batch loop. Candidates expose stable `id, ordinal`; eligibility returns `ordinal`, not `id`, because global legacy browsing can contain multiple origin groups for one ID. `FilterVisibleWorkLibraries` intersects parent, allowed and non-hidden memberships; NULL retains exact qualification.

## 3. Contracts

1. Movie/Series 请求优先从作品及其持久库归属产生轻量候选。不能只为判断“有文件”或取得作品 ID，先展开全库 Media、季集、版本和展示关联后去重。
2. Latest file time serves only its matching sort. NFO DateCreated uses the item's first `created_at`; other sources retain existing DateCreated/release/playback ordering. Web movie library cards explicitly use work latest time, not the representative-version date. A non-null latest time alone does not prove visibility.
3. Maintained library membership may replace redundant file existence checks. Ordinary unfiltered leaf works and HongGuo intersect membership with parent/allowed minus hidden libraries; NULL retains exact fallback. NFO library candidates use their own library_id and non-null latest time: normal ingestion transactionally creates same-library items and bindings. Missing-field predicates and effective state still read necessary files. Ordinary episode hierarchy/path qualification runs after the candidate batch, not while enumerating it. Global album time/title never shrink to visible members.
4. User/library permissions, file predicates and effective played filters precede the final page, not necessarily the raw candidate batch LIMIT. Adult restrictions are library-only; item/metadata/binding NSFW fields and predicates no longer exist. Preserve ordinary pre-group episode filtering and never treat partially scoped ancestors as complete container state.
5. 启用计数时，筛选后只统计逻辑候选，并保留越界页准确总数。不需要总数的请求不计数。状态/特殊资格取页先按作品字段取 50 个逻辑候选，再筛选，不足继续补取，StartIndex 只跳过合格结果；红果先归并卡片。`WorkBatchPage` 用只读可重复读快照处理计数和补取；精确计数可能处理全体资格，但不能加载全库展示数据。普通剧集计数对非空入库时间候选复用原文件 EXISTS，找到首个合格分集即停；空时间候选保留一次受限文件扫描，避免逐个探测空目录。两支互斥，时间只选执行策略，不能代替层级、文件或状态资格。
6. 分页后才批量读取海报、简介、展示用已看/未看集数、文件版本及其他详情。父子混合身份须去重文件；限定内层绑定范围，不能仅依赖最外层 `id IN (...)`。
7. Season/Episode、断点续播、文件属性筛选和文件依赖排序按真实语义处理；只为必要的资格/排序读取相关文件，不一律改成作品最近添加。已验证的索引搜索不退回数据库全库枚举。
8. 作品优先是查询职责边界，不是强制数据库逐作品相关探测。空目录、统计滞后或稀疏命中可能使该计划更慢；保留行为正确且有执行计划证据的实现，禁止盲加 OFFSET、物化或强制规划器配置。
9. 汇总已看身份集合时优先使用 `CompletedPlaybackStates`：显式已看直接保留，未显式完成且有断点的记录才交给 `PlaybackStates` 推导。NFO Latest 等逐身份资格检查保留原有 `PlaybackStates` 索引探测，避免集合 UNION 导致短页扫描全部状态。不能简化为只读原始 completed，也不能为了布尔筛选计算全部续播展示列。
10. Page kinds, not library type, determine whether hydration is container-only. Pure HongGuo/NFO Series pages omit unused representative, played-time and position aggregates; ordinary container playback consumes completed identities. Leaf and mixed pages retain version/position/time consumers. Verify actual state scans after changing from full states to completed identities.
11. Mixed-source candidates/count stay in PostgreSQL under their existing snapshot; do not introduce independent count/page connections. Final-page hydration may run at most one job per source, using `sync.WaitGroup.Go` and `context.WithCancelCause`. Each job owns its result slice, errors cancel siblings, and merging happens after Wait in the original order. Test both three-connection overlap and a single connection, and preserve preferred media for resume.

The optional-count default is limited to the HTTP Items handler and its aliases.
Keep Web and dedicated Views/Latest/Resume/NextUp/Shows contracts unchanged.
Propagate the mode through every Items branch and include it in the Items cache
key. Never interpret the uncounted helper's internal zero as an empty catalog.
The public Items wrapper normalizes external Limit to 1..500 before requesting
one extra qualified item; service and person repository allow internal 501.
Trim only the returned page, not the cached raw lookahead page. Media IDs and
root Views return complete collections; Person IDs still paginate. Empty-page
hint 0 is a lower bound, not a claim that the catalog is empty. Page-level episode
counts remain required. Search keeps its existing bounded candidate recall and
ranking: its total/hint describes that result set, not all database matches.
Native HillS/Yamby pagination must be tested separately before declaring this
trial compatible; server pagination and request logs alone cannot prove that.

Random is a supported primary sort, including `Random,SortName`. Generate one
internal integer seed per Items request and hash the fixed candidate identity;
all refill batches use the same seed. Do not use a new random() order per batch,
fall back to date sorting, or compute file dates merely to randomize works.
Ordinary Movie/Series, NFO, HongGuo, mixed movie libraries and global work pages
retain their actual file/state eligibility and source/origin/album identities.
The global random Movie/Series route also applies without NFO/HongGuo data.
Ordinary random pages bypass whole-page cache; do not add a high-cardinality
seed cache key. Separate HTTP requests may reorder, so cross-request random
pagination is not a stable snapshot. Hash sorting can still scan work candidates;
NULL membership retains exact file fallback and sparse states can require many
50-candidate batches. These are not guarantees of constant-time sampling.

For bounded container hydration, do not join the unrestricted completed-state
UNION: an outer page-source filter can still scan all user history, observed with
60,000 HongGuo and 25,000 NFO states. NFO keeps its logical-episode
`LEFT JOIN LATERAL (... OFFSET 0)` probes. HongGuo library cards instead correlate
states to each `page_works.source_id` and materialize `page_states` once. First
deduplicate visible file versions into materialized `page_files`, then join by
source ID and episode number; otherwise even the bounded state CTE may be
rescanned per file. Keep plan assertions on base-table rows/loops and CTE scans,
not only selected columns or SQL count. This does not change general
PlaybackStates consumers that need position, representative media or watched time.

Ordinary `containerEpisodeScope` limits seasons by `id IN (page IDs) OR
parent_id IN (page IDs)` inside an `OFFSET 0` subquery before joining episodes
and series. An outer cross-table season/series OR alone can scan the entire
season and series catalogs. This scope also serves manual watched/unwatched
writes: preserve mixed parent/child IDs, season zero, visible files and logical
episode deduplication. Plan checks must bound metadata visits, not only Media
and history probes.

Web `ListRecentLogicalWorks` materializes global album MAX timestamps once per
candidate batch, then applies work visibility separately. Do not run the album
representative/latest LATERAL per catalog work merely to obtain identity/time.
The private `workBatchPage` can return the selected `latest` alongside each ID;
the public ID-only `WorkBatchPage` contract stays unchanged. Preserve those
dates and ranks through hydration instead of rerunning the full combined
candidates after selection. Refill and dates share the same read-only snapshot.

当前实现边界：全局 Movie/Series、普通 Emby 计数页/混合电影库、Web 电影页已接入；无库 Latest 保留原默认 Movie/Episode 层级但不额外计数。Web 普通剧库的分集日期排序仍复用一次受限文件范围，页后才做展示统计。普通全局直接绑定整剧/季与分集的既有祖父分组保持兼容，不能在性能修改中合并而悄然改变总数。

The 50-candidate refill loop applies to ordinary metadata/Series pagers and Latest,
state-filtered library NFO/HongGuo pages, global mixed-source browsing/Latest,
Web recent works and file-filtered Web NFO pages. Unfiltered NFO/HongGuo library
pages and queries that already compute exact eligibility in required sort inputs
page directly; do not add a second qualification loop just for uniform SQL shape.
Web movie pages use global work time and qualify first-Part versions before both
count and page selection; probe-based representative choice and version statistics
run only for that page. Later-Part-only metadata must not inflate the total.
Web Series retains one scoped
file aggregation for episode release/year/file dates. Mixed movie libraries use
the required release-date aggregate's `HAVING COUNT(*) > 0`, not a duplicate EXISTS.
Search and special hierarchy routes retain their own semantics.
They still reuse maintained memberships: normal ordinary Movie/HongGuo search
skips file existence only with nonempty known membership and valid latest time;
ordinary Series search retains its season/episode/file qualification. Restricted
search needs only that scoped qualification, not an additional global EXISTS.
NFO search uses its own library and valid time, with exact checks for missing-file
fields or uninitialized time. Preserve index recall limits and ranking.
Continuation candidates use item/source membership before the unchanged concrete
file permissions, effective state and successor selection; never order by import
time instead of watched time. Ordinary season lists page lightweight seasons via
50-candidate refill and compute episode summaries only for the final page.
NFO child membership predicates belong inside each native identity/parent lookup
in `nfoWorkFileItems`, not in an outer item-table join that can scan the catalog.
Materialize `qualified` before joining it back to `work_batch`: inlining correlated
EXISTS can repeat qualification for every join pair (2,500 probes for 50 candidates).
For ordered ordinary candidates, apply type/time/work filters before the library
scope's `OFFSET 0` boundary. A restricted Latest must not walk the global time
index past unrelated libraries. `orderedWorkLibraryScope` splits positive known
membership and NULL membership into disjoint `UNION ALL` branches so GIN and
the unknown-membership index can each be used; unrestricted global ordering
retains index early stop. Keep all existing same-element visibility predicates.
HongGuo work candidates project native `w.kind` (`NOT NULL`, checked movie/series),
not `LOWER(CASE ...)`: computed type selectivity can turn one album hash join
into millions of nested-loop comparisons. Convert to Emby Movie/Series only
in payload projections, and retain the global album title/time scope.
Completed-album HongGuo work lists derive group identity directly from the work's
related_album_id; invalid/missing album coordinates wait for the existing supplement.
Do not repeat global album aggregates for batch state qualification or final-page
member IDs. Native work/album filters precede page presentation/versions; keep movie
identities and the original file/state scopes. Web library title paging shares one
album-representative CTE, scoped to the requested album when one is supplied.
Library HongGuo DateLastContentAdded/Latest uses `hongGuoLibraryLatestWorks`:
one logical-identity GROUP BY computes global MAX time and BOOL_OR visible
membership, after checking the parent with MediaVisibility.allows. Do not collect
all member-ID arrays or titles in this candidate aggregate: ARRAY_AGG can force
a catalog-wide sort. Resolve only work_batch members through native work/album
keys in a bounded LATERAL lookup; broad OR-of-IN subqueries can scan the catalog
again and trigger JIT. NULL membership still uses exact file qualification.
Other sorts and global/mixed candidates retain their existing title/time scopes.
Series count's NULL-time fallback has an uncorrelated existence guard; when no
unknown-time candidates exist, its file scan must have zero loops. Retain the
sparse empty-catalog bound as well as the dense file-backed count bound.
The ordinary membership-only shortcut requires a nonempty library array. Global
browsing may admit `[]` candidates for unassigned files, but must then use exact
file qualification: `NULL <> ALL(hidden)` is not true. Skipping that predicate
can produce a positive count with an empty payload. Keep the hidden-library plus
NULL-file-library case in `TestEmbyKnownMovieMembershipSkipsFileQualification`.

Global qualification must constrain both bindings and work metadata. In the
HongGuo branch, resolve `b.work_id` through a correlated LATERAL lookup with
`OFFSET 0`; an unrestricted work join was reordered into repeated catalog scans
in the 600,000-file fixture despite `b.work_id = ANY(item.work_ids)`. Keep plan
checks for work projections as well as file/state rows. Web recent candidates
carry grouped source work IDs so batch eligibility need not rebuild album identity.

No-NFO global Latest retains its older direct-binding contract: ordinary candidates
first keep their original per-source limit/tie window, then merge identities with
HongGuo and select the final page before hydration. It uses ID ascending on equal
latest dates, unlike the NFO-present global route's ID descending. Do not silently
change those tie windows or convert directly bound ordinary Series media to folders.

## 4. Validation & Error Matrix

| 情况 | 必须结果 |
| --- | --- |
| 受限空库权限、全部隐藏、无合格文件 | 空结果；计数接口总数为 0 |
| 前排作品不满足权限或状态 | 继续检查后续候选，不在资格前截断 |
| 越界页 | 空 Items；启用计数时保留准确 TotalRecordCount，免计数下界为 0 |
| HTTP Items 未传开关 / false / true | 前两者无额外总数 SQL、返回数字下界；true 保留准确总数，固定排序/随机种子时页面内容相同 |
| 两种计数模式交替命中缓存 | 不共享缓存键；原始前瞻页不被裁切污染，下界不冒充准确总数 |
| Limit=500 / 非法 Limit / Person IDs / 根 Views | 内部 501 前瞻 / 外部归一化为 50 / 仍分页 / 完整库集合 |
| 随机未看首批不足 | 同 seed 顺序继续补取，跳过已看/无权限；不回退日期排序 |
| 作品跨 A/B 库、A 删除最后文件 | A 不再展示，B 保留；时间按既有全局规则 |
| 同作品多版本/分段、红果多季 | 保持逻辑身份、去重、原代表和排序 |
| 查询失败 | 返回原错误，不伪装成功空页 |
| 混合详情某来源失败 | 取消其他来源，等待退出并返回原错误，不输出部分成功页 |
| 数据库只有一个连接 | 并发详情可串行获得连接，不死锁 |
| Web movie receives a newer low-priority or other-library version | Card moves by global work time; playable representative remains visible and uses original version priority |
| First 50 season candidates contain only direct season files | Continue to seasons with qualifying episodes; no short page or inflated total |

## 5. Good / Base / Bad Cases

- Good：Movie/Series 候选按请求筛选排序，分页后仅为选中作品生成详情；Latest 复用资格规则但不计总数。
- Base：DateCreated 必须按原文件范围计算时，只生成所需排序值，不顺带统计封面角标或展开全层级。
- Bad: removing only the JSON total while still executing the full count query, or changing every dedicated handler's defaults through the shared parser.
- Bad：某 APP 调用 Latest 已优化，另一个 APP 调用 Items 却重复实现全库文件展开；或为复用最快路径而改变后者排序/层级。

## 6. Tests Required

- `TestEmbyItemsOptionalTotal` exercises production route registration, parameter aliases, lower-bound versus accurate totals, page boundaries, external 500/internal 501, Person/media IDs, root Views, bounded search and cache mode switching. `TestEmbyItemsCountModes` compares complete pages for ordinary/NFO/HongGuo/mixed/hierarchy/resumable/person branches and captures actual count SQL. Refill tests run both count modes; client testing remains a separate gate.
- `TestEmbyRandomSortUsesSeed`, `TestEmbyRandomGlobalWithoutExternalCatalogs` and the random cases in `TestEmbyWorkBatchContinuesAndCounts` verify primary sort, ordinary-only mixed types, permission/state eligibility, fixed-seed page order and refill across rejected batches. `TestHongGuoLibraryPagePlan` checks Random's materialized candidate subtree has zero file rows in the maintained-membership fixture (unknown empty works may perform empty index probes), and bounds batch file visits independently of accurate count.

- 保存改动前查询作 oracle，比较完整 IDs、顺序、总数与关键 payload；覆盖三来源、库内/跨库、显式/默认类型、不同排序、收藏/人物/播放筛选、空页、权限和多版本。
- `TestSearchUsesMaintainedMembership` compares original source eligibility across permissions, NULL/empty membership and deletion; real plans require zero file/binding visits for known candidates. `TestLibraryMoviePageUsesGlobalWorkTime` covers conflicting preferred/latest dates, hidden-library global time and cross-metadata Parts. `TestLibraryMoviePageKeepsVersionsWithWorkTimeOrder` bounds probe work to page versions. `TestEmbySeasonCandidatesRefillBeforeDetails` covers sparse qualified seasons across 50-row batches, offsets, totals and hidden scope. Retain NFO child/hierarchy plan bounds on 100,000 files and mixed continuation/Resume plans.
- 复用 `TestEmbyGlobalBrowseCandidateBatches`、`TestEmbyLatestCandidatePlans`、`TestHongGuoLibraryPagePlan`、`TestNFOLibraryPagePlans` 和 Web 库页/最近添加测试；新增消费者必须加入调用入口矩阵，不能只测 helper。
- `TestEmbyGlobalBatchMixedSourcesRefill` compares mixed-source sparse pages/counts with the old hierarchy oracle across rejected batches; `TestWebNFOFilteredBatchRefill` checks missing-title library pages and recent works. `TestMixedLatestKeepsLegacyTieWindowAndIdentity` preserves no-NFO ties/direct bindings. `TestEmbyGlobalPayloadsParallel` covers both Web/Emby overlap, cancellation, source merge order and one-connection pools under `-race`.
- `TestEmbyWorkBatchContinuesAndCounts` covers ordinary Movie/Series, NFO and multi-season HongGuo: first 50 rejected, sparse matches across three batches, effective offsets, tails, exact totals and early stop. `TestEmbyWorkBatchError` checks later-batch errors and transaction isolation. `TestEmbyKnownMovieMembershipSkipsFileQualification` asserts zero Media loops for known membership and last-file deletion. Capture count, every batch and hydration separately in plan tests; old SQL-prefix matching must not silently omit the new query.
- PostgreSQL 实际计划验证候选扫描、`(rows + filtered) * loops`、物化次数、页内绑定范围及完整 payload 查询。包含大小库、稀疏命中、空目录和统计滞后；旧计划已证明更快的分布不能丢失。
- `TestEmbyLatestCandidatePlans` inspects every metadata scan under the `CTE work_batch` subtree, asserts at least one was inspected and sums visits; aliases may gain suffixes after subquery/UNION rewrites. Test both time-skew distributions without mistaking batch episode qualification for candidate enumeration. `TestHongGuoLibraryPagePlan` must send explicit Series and bound album scans/join-filter comparisons. `TestEmbySeriesDenseCountPlan` bounds total file/catalog visits and compares NULL-time fallback/state results against the old hierarchy oracle; direct Series/Season files and zero-number episodes cannot inflate the count.
- SQL 条数不增加、响应只含一页、使用了索引，都不是独立的性能验收。记录合成计划/服务方法/生产 HTTP 各自边界，不以一个入口的毫秒数代表全部客户端。
- `TestEmbyContainerPlaybackBoundsHierarchy` compares the original scope with series, season and overlapping IDs, and bounds actual metadata/file/history visits. Existing manual watched/unwatched rollback tests must also pass. `TestRecentLogicalWorksPreservesBatchTiesAndFilters` captures actual batch SQL (never an obsolete handwritten file query), asserts no page-date requery and no Media enumeration inside `CTE work_batch`; file qualification belongs to the separate `qualified` stage. `TestRecentHongGuoAlbumsAggregateOnce` bounds total work visits/join comparisons and retains hidden-member global time. `TestRecentLogicalWorksRefillsWithDates` verifies four batches, original order and dates without candidate replay.
- 文件访问下降但耗时不降时，补采带计时的 EXPLAIN，分别查看 JIT 编译、必要日期汇总与页内详情；`TIMING OFF` 下缺失的 JIT Timing 不是“编译耗时为零”。性能基准与其它数据库测试分开执行，不把并行负载结果作严格前后对比。
- 红果库内 Latest 计划还须断言无全目录标题/成员数组聚合、无 JIT，并统计所有作品访问次数。批内 50 卡片各 3 季可产生最多 150×150 次状态关联比较，不可误判成全目录合集关联；保留文件、历史和页内资料的原扫描边界。
- 红果详情状态循环按页内源作品而非文件数增长：`TestHongGuoLibraryPagePlan` 的 9 个源作品、900 个文件场景，累计状态表 loops 必须为 1..30、visits 不超过 3,000，`page_states` 扫描 rows×loops 不超过 1,000；同时覆盖空历史和 60,000 条历史，不能只验证物化了一次。

## 7. Wrong vs Correct

Wrong：`全库文件 × 层级/展示关联 → 去重作品 → count/page → 再查全库详情`。

Correct：`作品候选及所需排序值 → 每批 50 条检查原资格，不足续取 → 合格结果分页 → 当前页详情`；需要准确总数时另对同一快照的合格集合计数。

Wrong: `count=false → total=0 → return empty page / TotalRecordCount: 0`.

Correct: skip only the count, select one extra qualified result, trim to the
requested page and return its numeric lower bound. A required protocol field
must not be removed merely because its accurate computation was skipped.

Wrong：看到 SQL 中有 Media 就全部替换，或把所有日期排序改为最新入库时间。

Correct：先区分文件是否用于必要资格/排序、当前页展示还是冗余候选枚举；保持协议结果，用执行计划确认改动收益。
