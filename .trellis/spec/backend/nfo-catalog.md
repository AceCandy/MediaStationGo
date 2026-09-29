# Local NFO Catalog Boundaries

## 1. Scope / Trigger

Applies to `nfo_movie` and `nfo_tv` ingestion, independent state, Web/Emby
projections and tasks. Four models are registered in `AllModels`; startup
creates their tables. No historical migration is provided, as the user
confirmed there are no existing NFO libraries.

## 2. Signatures

- `NFORepository.Ingest(ctx, media, input) (changed bool, err error)` writes
  independent items and file snapshots transactionally; source is `nfo`, with
  NULL `media.metadata_id`.
- Scans and file events use common kinds `scan`/`watch` and definitions
  `library_scan`/`library_watch`, including NFO and HongGuo libraries.
- `POST /api/tasks/definitions/library_scan/run` requires `{library_id: string}`.
  The legacy `nfo_scan/run` action still accepts NFO targets and creates a common scan.
- Task Center redirects the legacy `system=nfo` URL to `system=common`;
  invalid/duplicate system values retain existing canonicalization behavior.

## 3. Contracts

- `nfo_items`, `nfo_media_bindings`, `nfo_user_states` and
  `nfo_playback_events` own identities, per-file snapshots and user state.
  Public logical IDs use `nfo-<uuid>`; never persist them in metadata_id.
- Movie versions require matching directory prefixes and explicit delimiters.
  Episode versions require matching filename prefixes through SxxExx and
  explicit version suffixes; equal episode numbers alone never merge.
- Each episode requires valid local NFO. Missing show/season NFO creates only
  initial placeholders and never erases accepted parent fields. Missing images
  retain accepted immutable assets. External IDs never trigger provider calls.
- Search, hierarchy, versions, favorites and history route by source. Ordinary
  scraping excludes NFO. Administrator statistics supports `system=nfo` and
  combined `system=all`; see playback-contracts.md. Playlists are not integrated.
- Series presentations retain their own SeriesID/SeriesTitle so card projection
  cannot erase hierarchy identity. Conflict-upsert reloads use a fresh GORM
  object to avoid filtering by a newly generated, unpersisted primary key.
- Each scheduled scan or watcher batch creates one common execution across
  library types. NFO scans share the common timer and library selector. Catalog
  isolation does not require separate scan/watch tasks or timers.
- Legacy `nfo_scan`/`nfo_watch` definitions are hidden from the task list, but
  their history/log APIs remain readable without rewriting stored records.
  The legacy NFO action retains its non-NFO target rejection.
- NFO network-scrape exclusion depends on library type, never task kind;
  this also applies to STRM refresh after generation.
- If watcher library lookup fails, requeue both video candidates and NFO/image
  sidecars through the existing debounce queue. Query failure must not consume
  accepted event types; directory recovery is also requeued, while unsupported
  extensions remain excluded.
- Legacy empty-system task rows derive system from kind. The catalog fallback
  excludes both `hongguo_` and `nfo_` prefixes.
- NFO ingestion serializes a path before checking/inserting its row. Unchanged
  file facts and snapshot fingerprint do not rewrite accepted snapshots.
  File-size/mtime changes invalidate complete probe documents in the same
  transaction. Invalid/missing NFO retains the previous binding and snapshot.
- Valid unrelated XML is not accepted as an empty NFO. Explicit season zero is
  supported; missing/invalid seasons are not silently converted into specials.
- Adult visibility is library-scoped. Items, bindings and snapshots have no
  NSFW field; imports, editing, exports and projections must not reintroduce it.
- In the retained local scraping path, `applyLocalMetadataMatch` must pass the
  merged `next` object to `persistLocalMetadata`. Passing the original media
  drops newly read episode coordinates and binds the file to the Series.
- 指定 NFO 库及全局 Movie/Series 浏览共用 `NFOWorkCandidates`，`NFOWorkNodes`
  仅展开页内作品文件。指定库的所有排序候选先排除最新文件时间为空的作品，
  直接使用条目库归属和有效文件汇总，不再重复查询文件归属/存在性；正常入库事务建立同库条目、层级和绑定。全部已看等价于不存在未看可见文件，
  必须复用 `PlaybackStates` 的有效状态，不能只读原始 completed。按分集身份关联状态以保留 Latest 的有界索引探测；不要把集合型已看 UNION 直接套入短页查询，十万文件计划曾因此扫描全部用户状态。
  沿季通过 LATERAL/`OFFSET 0` 定位分集，防止相关查询反复扫描分集目录。
  Only requested played filters compute candidate states. DateCreated uses
  `nfo_items.created_at` in both library and global candidates; no file-date
  aggregate is needed. Only state qualification reads root/season/episode files;
  simple candidates read the item table. Missing-poster/title filters retain the
  full view predicate. Global items with uninitialized latest time retain exact fallback.
- 指定库 Latest 不计算作品总数；普通列表越界页仍返回准确总数。
  Latest、DateLastContentAdded 与 Web 最近添加读取条目的
  `latest_media_added_at`，空值最后，特殊筛选和季集路径保持原路由。
- Library state-filtered Items and Latest sort lightweight item candidates first,
  then `filteredWorkBatchPage` takes 50 and applies effective state only to those
  identities. Refill until enough qualified results or exhaustion; preserve season/
  episode-number precedence and stable ID ties. Counted lists compute exact totals
  in the same read-only repeatable-read snapshot. Latest stops when full and never
  counts. Only the final page reaches `NFOWorkNodes`.
- Web `nfoLibraryPage` 候选只计算身份和排序时间，页内才统计代表文件、集数、
  版本数；缺图/中文标题过滤在候选及统计阶段保持一致。
- Detail, child hierarchy and version reads share `nfoItemViewQuery`: resolve the
  requested items, immediate episodes and season descendants once, then constrain
  `b.item_id = ANY(ARRAY(...))` before the unchanged visibility/state projection.
  `nfoWorkFileItems` uses UNION to deduplicate overlapping series/season/episode
  inputs and a lateral `OFFSET 0` to bound descendant reads. A plain joined leaf
  subquery can overestimate cardinality and hash-scan all items; an outer Media
  fence alone can instead scan all bindings. Verify actual plans, not SQL shape.
  Scoped nodes may include partial ancestors: callers must still filter the
  requested node/parent; never use a leaf-scoped ancestor as a whole-series summary.
  Mixed Items and Resume/NextUp hydration use `nfoItemNodes(ctx, userID, ids...)`
  and the same `NFOWorkNodes` scope, not full-catalog `nfoNodes` followed by outer IDs.
  `nfoWorkFileItems(ctx, ids, filter)` applies item-library permissions inside each
  native identity/parent branch. Do not wrap its output in another joined item
  scan: the 100,000-file regression observed 52,000 unrelated item reads that way.
  Concrete file permissions remain; membership is not a replacement for child
  existence, effective state or missing-file-field checks. Search may use own
  library and non-null maintained time to skip redundant file existence, with
  exact fallback for missing-field predicates and uninitialized time.
- `NFOWorkNodes(..., containersOnly)` may omit representative media, watched time,
  position and file-date aggregates only when every selected row is Series.
  It uses effective `CompletedPlaybackStates`; mixed/Movie/Episode and detail
  hydration retain full states, version preference and LastPlayedDate.
- NFO item DateCreated is its first creation time, including Movie/Episode
  payloads through `MediaView.CatalogCreatedAt`. Ingest conflict updates never
  overwrite it. Files can precede metadata; new versions, rescan and deletion of
  the earliest file do not change a surviving item's time. Media.CreatedAt,
  MediaSource/PlaybackInfo dates and latest_media_added_at keep file semantics.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| `nfo_scan` targets ordinary library | HTTP 400; no execution |
| Public scan targets an NFO library | Common scan execution; local NFO ingestion |
| Mixed ordinary/HongGuo/NFO file events | One common watch execution |
| Same file and snapshot scanned twice | Second ingest returns changed=false |
| NFO becomes invalid/missing | Preserve accepted snapshot, report file state |
| Adult library is hidden by user/profile | No visible file |
| `<html>` supplied as movie NFO | Reject instead of clearing metadata |
| Single-episode NFO has no valid season | Reject; no invented season zero |
| Fresh or repeated startup | Create/reuse four independent tables |

## 5. Good / Base / Bad Cases

Good: two clear movie versions share a local item but retain file snapshots.
Base: a raw episode learns S01E02 from its NFO before legacy persistence.
Bad: set `metadata_id` to an NFO UUID, infer identity from a provider ID, or
claim complete isolation while the scanner still uses the old writer.

## 6. Tests Required

`TestNFORepositoryPreservesFilesAndPreviousSnapshot` covers repeat/changed
snapshots, invalid-NFO preservation, independent views and library filters.
`TestNFORejectsWrongDocumentAndUnknownSeason` covers document/season validation.
`TestNFOTasksUseCommonDefinitions`, `TestNFOTaskLegacySystemFilter`,
`TestNFOWatcherSharesMixedBatch`, and `TestNFOSchedulerSharesLibraries`
cover common attribution and historical access. Run PostgreSQL tests with the isolated test DSN.
`TestWatcherBatchRequeuesSidecarsWhenLibraryQueryFails` covers sidecar-only and
mixed batches on lookup failure, including every supported image extension.
`TestNFOOnlyTVPersistsShowAndEpisodeNFO` must retain its episode-title/coordinate
assertions; its fixture must include `MediaProbeMetadata` used by MediaView.
`TestNFOFreshStartupAndScan` verifies repeated startup, local image refresh,
independent state/events, search and Web/Emby reads without shared metadata.
`TestNFOSeriesHierarchyAndStateIsolation` verifies explicit versions, Web
cards/episodes/recent items, SearchHints, hierarchy, favorites, played state,
resume grouping and hidden-library filtering. Browser/device playback remains
a separate manual acceptance step.
`TestNFOLibraryPagingMatchesHierarchy` 对比原层级完整响应、播放筛选、权限、排序与越界页；
`TestNFOLibraryMovieVersionsAndFilters` 覆盖电影版本、文件资料筛选及删除旧版本后的有效已看候选。
`TestNFOLibraryPagePlans` 使用十万绑定、2.5 万条有效状态执行真实 EXPLAIN ANALYZE，
断言标题/DateLastContentAdded 候选及页内详情不扫描全库绑定、状态或逐作品重扫分集目录。
同时核对结果顺序、总数，确认 Latest 不计作品总数；DateCreated 必须单独保留回归，
不能把只测 DateCreated 的用例当成 Latest 性能验证。
The same plan fixture covers series/season/episode detail, child lists and
versions, including payload SQL. Bound visits as `(rows + filtered) * loops`;
`TestNFOLibraryPagingMatchesHierarchy` compares scoped nodes under
hidden/empty/allowed library permissions and watched state. The repository batch
test compares original OR queries with scoped movie/series/season/episode reads
and overlapping parent/child inputs, retaining every visible version exactly once.
The 100,000-file plan fixture also checks mixed-page payload SQL. The global browse
oracle compares complete batch nodes with the original hierarchy, including counts.
`TestNFOLibraryCreatedAtUsesFirstItemCreation` covers sorting and payload dates,
late metadata, added versions, rescan and deletion of the first file. Plan tests
assert no file-date aggregation in candidates and no unused container fields.

## 7. Wrong vs Correct

Wrong: create a separate scan/watch task for each isolated catalog, or infer
whether scraping is allowed from the execution kind.
Correct: share file tasks and select ingestion/scraping behavior by library type.

Wrong: merge NFO into `next`, then call `persistLocalMetadata(ctx, m, ...)`.
Correct: pass `&next`, so metadata kind/season/episode use the merged facts.

Wrong: route all NFO library lists to tables that the application does not
create or migrate yet. Correct: finish and verify schema/upgrade/read integration
before activating that route.

Wrong: 仅凭 SQL 外层有页内 ID 过滤就断言查询受限；相关 IN 子查询可能反复扫描。
Correct: 沿作品/季/集主键 JOIN 绑定，并以真实大样本执行计划验证扫描行数。
