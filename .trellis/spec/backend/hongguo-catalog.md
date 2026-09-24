# HongGuoDB Catalog Isolation

## 1. Scope / Trigger

Apply when changing HongGuo collection, file binding, grouping, user state,
statistics, or Web/Emby projections. This is a separate catalog, not a TMDb
provider and not a source of remote playable streams. Administrator downloads
are a separate authorized exception; playback still uses existing local/STRM files.

## 2. Signatures

- Catalog tables: `hongguo_discoveries`, `hongguo_rank_entries`, `hongguo_works`, `hongguo_episodes`, `hongguo_people`,
  `hongguo_credits`, `hongguo_snapshots`, `hongguo_artworks`,
  `hongguo_sync_states`, `hongguo_sync_failures`, `hongguo_media_bindings`.
  Official grouping lives on `hongguo_works.related_album_id/season_index`;
  migration drops retired manual group/member tables without CASCADE.
- `hongguo_discoveries.source_category` and `hongguo_works.source_category`
  contain `real-drama|comic-drama|ai-drama`, or empty for legacy/direct-ID rows.
  The former `comic` source is not collected or shown; existing rows are retained.
- User-owned tables: `hongguo_user_states`, `hongguo_playback_events`.
- Shared file: `media.catalog_source=hongguo`, `lookup_catalog_id` is the
  upstream string ID, `metadata_id` must remain NULL (database CHECK).
- File convention: `[hongguo-<sourceID>]`, source season `S01`, source episode
  `Exxx`. The source episode number, not its upstream video ID, is the binding
  coordinate. `HongGuoDB` is also accepted in the explicit source tag.
- APIs live under `/api/catalogs/hongguo`; `works`, `works/:id/episodes`,
  `works/:id/media`, `groups`, `libraries/:id`, `me`, `pending`, `status`,
  `cancel`, and `artwork/:id` are registered in `handler/hongguo.go`.
- `GET works` returns paginated `HongGuoListWork`: existing public work fields
  plus local `artwork_id`, parsed `tags`, `hydrated`, and optional `group_id`. It merges canonical
  works with discovery-only summaries by `source_id` before count/pagination;
  do not fetch each work's full detail to build poster cards.
  List and search batch-load group membership for the returned hydrated work IDs;
  no Media existence check or per-card detail query is involved. Group members
  remain on the existing authenticated group-detail GET, loaded on demand.
- `GET search?keyword=...` requires `can_view_discover` and an enabled source.
  It reads the official `/search/{keyword}` first response (or fixed detail URL
  for a numeric source ID), preserving string IDs and upstream order. Return
  the actual returned item count, not the upstream total as invented pagination.
  Merge local hydration, classification and artwork IDs in batches, never
  expose upstream image URLs or infer source category from tags. Search registers
  unhydrated results in `hongguo_discoveries` using source-ID conflict-do-nothing;
  repeated searches preserve existing summaries, categories and failure cooldowns.
  After registration, search requests an asynchronous event-triggered refresh;
  the search request itself never downloads media or creates canonical works/episodes. Queue persistence
  failure fails the search rather than claiming successful registration.
  Administrators may explicitly invoke the existing
  `POST works/:id/refresh`; viewers only register summaries through search. Late refresh responses must
  not navigate a different search/account after the original page unmounts.
  In the `other` category, administrators may assign one official source
  category. Update matching discovery and work rows atomically; viewers remain
  read-only.
- `GET works` accepts optional `source_category=real-drama|comic-drama|ai-drama|other`;
  `other` filters empty persisted categories and is never stored as a category.
  optional `category=<allowlisted exact tag for that source category>`, or one
  official `rank=hot-drama|hot-real-drama|hot-ai-drama|hot-comic-drama`.
  Rank cannot be combined with source/category filters. The retired local
  `sort=latest|rating|hot` parameter is rejected.
- `/api/admin/playback-stats?system=hongguo` uses the existing filter/DTO
  contract but only reads independent events; omitted system remains `catalog`.
  `system=all` combines ordinary, HongGuo and NFO projections before aggregation
  and pagination; event storage and source-work ranking remain independent.
- Emby identities: `hg-work-`, `hg-season-`, `hg-episode-`, `hg-person-`
  followed by internal UUID; `hg-group-` is followed by official album ID.
  Provider ID values remain upstream IDs.

## 3. Contracts

- Never create a `metadata_items` surrogate. Reject source files before old
  automatic scraping, manual search/matching, or manual metadata editing writes
  any old metadata. The final Media CHECK alone cannot prevent orphan metadata.
- Parse source IDs as strings with `json.Decoder.UseNumber`. Only consistent,
  explicit completed-one-episode evidence makes a movie. Updated, total and
  accessible episode counts are distinct. First visible time is not a claimed
  authoritative release date. Preserve episode positions when video IDs are absent.
- Detail updates are transactional and idempotent. Do not delete missing
  episodes merely because one response omits them. Keep only whitelisted snapshots.
- Source images live under `DataDir/catalogs/hongguo/artwork`; public DTOs/logs
  exclude upstream image URLs and credentials. Retain an old image until its
  replacement succeeds. Missing local images enqueue repair.
- `Client.Category` returns list summaries, not authoritative details.
  `SaveDiscoveryPage` batch-upserts summaries, source-ID artwork download rows,
  and the next-page checkpoint in one transaction. It never calls detail HTTP,
  writes canonical works, generates episodes, or rebinds files. Empty cover URLs
  create no artwork row. Task updates are emitted once per page.
- `SaveDiscoveryPage` copies the current source category into every discovery
  row. `SaveDetail` carries it into the hydrated work by source ID and preserves
  an existing non-empty value for direct-ID refreshes. Never infer this field
  from title or detail tags. Existing empty rows are populated after discovery
  sees the source ID and that work is refreshed again.
- Discovery task `processed`/`new` counts only source IDs newly inserted into
  `hongguo_discoveries` with no existing `hongguo_works` row. Category rescans,
  cross-category duplicates and rank replacements still update summaries and
  positions but never count old IDs as new. Insert eligibility is determined
  by the committed database insert within the summary/checkpoint or rank
  transaction, not by the upstream page length.
- Official ranks come from `/rank/hot-drama`, `/rank/hot-real-drama`,
  `/rank/hot-ai-drama`, and `/rank/hot-comic-drama`. Parse the server-rendered
  ordered list and `rel=next`; follow at most `MaxRankPage=100` pages. Replace
  one rank atomically only after all its pages succeed, preserving the previous
  rank on fetch/parse failure. `hongguo_rank_entries` stores `(rank_key,
  source_id, position)` because one work may belong to multiple ranks. Rank
  summaries enter the existing discovery/detail hydration queue; a rank update
  may fill an empty source category but must not overwrite an established one.
- Discovery continues from each category checkpoint until a valid empty page,
  without a 20-page per-run cap. A page containing only IDs already seen in
  this category during this run is a pagination-stall error, not completion.
  After a category reaches its tail, `AfterID` records the first-page boundary;
  later runs scan from page 1 and stop after reaching that boundary, preserving
  full-scan behavior for categories without a boundary or with an interrupted
  checkpoint.
  The Web source currently returns 24 raw entries per full page. Determine the
  last page from the raw `recommendList` length, not the filtered/deduplicated
  work count; save a short last page and reset its checkpoint atomically.
  To recover an older checkpoint already advanced past such a short page, a
  page-N 404 may reset only after page N-1 is re-read successfully and is
  still short. A first-page 404 or a 404 after a full preceding page is an
  error and preserves page N.
  `MaxCategoryPage=10000` remains a safety bound: a nonempty boundary page is
  saved without resetting its checkpoint, then fails explicitly. Only an empty
  page resets a category to page 1 for the next run. HTTP/parse/save failures
  stop the run and preserve the last committed checkpoint.
- The public aggregate category routes currently expose a capped window, not
  the entire source catalog. Topic routes such as `/category/ai-drama/drama`
  expose additional IDs with explicit parent-category evidence. An approved
  one-off topic backfill may union these routes and skip cross-category or
  established-category conflicts. Repeated stable scans do not prove historical
  completeness. Do not enable sitemap collection without a separate request.
- `PendingDiscoveries(ctx, after, cutoff)` keyset-pages 100 summaries by source
  ID, excluding existing canonical works and all failure rows. Pending state is
  derived from business tables, not a second mutable status or execution log.
  Refresh drains pending work present at its fixed start cutoff, then retries
  up to 50 eligible failures selected at start and refreshes up to 100 existing
  **unfinished** works older than 24 hours using the persistent refresh cursor.
  A canonical work marked `completed=true` is excluded from both scheduled
  existing-work refreshes and automatic due-failure retries; an administrator's
  explicit source-ID refresh remains allowed. Later discoveries
  wait for the next run; explicit source-ID refresh bypasses that cooldown.
  Successful discovery-page/rank persistence and successful search registration
  call `requestRefresh`. Wakeups coalesce behind the existing refresh mutex;
  after the current manual/scheduled/event run releases it, one event worker
  rechecks pending summaries and starts the same task execution only if needed.
  Wakeups arriving during that run request a subsequent fixed-cutoff pass.
  Workers reserve the mutex before goroutine launch, use service-owned cancellation
  rather than the search HTTP lifetime, and are joined by `Wait`. Cancel/disable/
  shutdown clear queued wakeups and cancel a reserved worker before its run starts.
  Empty or cooling-only queues do not create an automatic execution. Business
  summaries survive process exit and are still recoverable by periodic/manual runs.
  Discovery-only rows appear as read-only Web cards with `hydrated=false`, but
  remain absent from Emby, detail, grouping, playback, and binding projections.
  Their click path reports that source details do not exist. Repeated discovery
  cannot overwrite canonical details or a canonical work's poster URL.
- Discovery, refresh and artwork each have independent task slots and may run
  together. Each slot rejects duplicate runs. Cancel, disable and shutdown
  cancel all three slots; shutdown joins all three before DB close.
  Artwork uses a fixed pass cutoff and checks source URL on write-back, so newly
  collected images wait for the next pass and stale downloads cannot overwrite
  refreshed URLs. Discovery checkpoints and failure retries are
  business tables, not task logs. Normal refresh excludes failed IDs; only due
  retries request them. Successful due retries are not repeated in the same run.
  Cancellation retains `context.Canceled` identity and finishes interrupted.
- Refresh reports successful first hydration as `new`, an existing work whose
  persisted whitelisted detail snapshot or source category differs as `updated`,
  and the same business detail as `unchanged`; refreshed/updated/fetched timestamps
  alone do not mean a content update. These three counts, failures and deferred
  items remain separate in the task summary and per-item details. The snapshot
  comparison must decode numbers with `json.Decoder.UseNumber`: numeric video
  IDs beyond float64 precision can otherwise hide real episode-ID changes.
  `TestHongGuoDetailChangePreservesNumericIDs` covers adjacent 19-digit IDs,
  changed source covers, and identical snapshots across refresh timestamps.
  The snapshot
  includes source poster URL and person avatar URLs but not local artwork download
  progress. Initial hydration and unfinished periodic refresh share the same
  detail save; no extra source HTTP request is needed for this classification.
- Detail HTTP 404 records `retry_at` at about 72 hours from the completed attempt,
  increments the task's `deferred` metric, and does not increment batch `failed`;
  other detail errors retain the one-hour retry and failed-batch behavior. A
  successful due retry clears the failure row. Existing works and episodes are
  untouched until a new detail response is parsed and saved successfully.
- Existing `hongguo_artworks` tables add/backfill/check/index `source_id` and
  replace their owner constraint inside one transaction before generic
  `AutoMigrate`; fresh databases add the owner constraint after table creation.
  The compatible constraint accepts source-only discovery posters and legacy
  work-only posters, but rejects ownerless rows.
- Disabling stops source tasks and new bindings; a disabled file upsert rolls
  back rather than replacing an existing binding. Existing files, metadata and
  user state remain readable. No destructive uninstall is provided.
- Official albums only change presentation. State key is `(user_id, source_id,
  episode_number)`: episode 0 is work favorite, episode 1 is also movie progress.
  Events are unique per user/session/source/episode and survive file deletion
  and manual unwatch. User tables are excluded from `HongGuoModels()`.
- Apply file/profile visibility before logical grouping, count and pagination.
  Emby mixed browsing pages merge identities in SQL before pagination and batch-load only
  the current page. Reuse existing old-catalog payload builders with `Fields`;
  do not call complete `Item` once per list row. Omitted Fields retains existing
  defaults; explicit Fields controls People/ProviderIds/MediaSources.
- Source statistics rank by source work, even after official grouping. Display
  current group title/season without changing the event identity. Missing or
  rebound files must not be linked as the original event's available media.
- Web source pages rebuild when authenticated user/profile changes. Task URL
  system is `common|catalog|hongguo|nfo`; stats URL is `all|catalog|hongguo|nfo`
  with Web default `all` and source-qualified detail/ranking identities.
- Web labels use 红果短剧; internal source IDs remain `hongguo`. Discovery is
  `/discover?system=hongguo`. Works/list/detail/episodes and group-detail GETs
  require `can_view_discover`; media, artwork and user-state routes retain
  existing authentication and file/profile visibility rules.
- Category browsing applies exact JSON-array tag membership to hydrated works.
  A tag filter requires a source category and must belong to that source's
  allowlist; the unfiltered list excludes retained historical `comic` rows.
  Category browsing orders by `first_visible_at DESC NULLS LAST`, then local
  `created_at DESC, id DESC`, before pagination. Never substitute collection
  time for a missing source first-visible time. Cards hide the source-category
  label when a source category is selected; mixed lists and search retain it.
  Rank browsing joins
  `hongguo_rank_entries` and orders by the stored official position; never
  derive a rank from local rating, rating count, or collection time.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Anonymous source API | 401 |
| No can_view_discover on works/list/detail/episodes or group-detail GET | 403 |
| Non-admin import/status/cancel/pending/statistics | 403 |
| Manual group POST/PUT/DELETE | Not registered; 404 |
| Invalid source ID, pagination or stats system | 400 |
| Unknown works source category, tag category or rank; rank mixed with category filters; retired sort parameter | 400; do not silently fall back to another list. |
| Hidden file/library or locked profile playback | Not found; no state mutation |
| Invalid official album ID or nonpositive/out-of-range season | Reject supplement; preserve existing relation |
| Source file sent to old metadata writer | Reject before any old metadata insert |
| Disabled source upsert | Error; prior file/binding unchanged |
| Detail/image network failure | Preserve successful data; independent retry |
| Discovery checkpoint write fails | Roll back the entire summary page |
| Discovery repeats a failed or hydrated ID | Preserve failure cooldown and authoritative work |
| Detail HTTP 404 | Keep the summary/previous work, retry after 72 hours, count as deferred, and continue the batch |
| Other detail failure | Keep successful data, retry after one hour, and fail the batch after processing later items |
| Discovery page has no unseen IDs this run, or nonempty page 10000 | Fail explicitly; do not claim full coverage or reset to page 1 |
| Category page has fewer than 24 raw entries | Save summaries and reset that category to page 1 in one transaction |
| Category page N > 1 is 404 and page N-1 is still short | Re-save the short page, reset to page 1, and log the recovery context |
| Category first page is 404, or page N-1 has 24 raw entries | Fail with category, page and relative path; preserve the checkpoint |

## 5. Good / Base / Bad Cases

- Good: official album places source work B at season 3; `S01E002` still binds B episode 2.
- Base: an ungrouped source work remains a usable standalone first season.
- Bad: match by similar titles, fabricate release dates, or store a source ID
  in `media.metadata_id`.
- Good: local file Range yields 206; user-provided STRM retains the shared
  redirect path. Playback never extracts a remote video URL from the source website.
- Good: one 24-item category page records summaries without 24 detail requests;
  a later refresh hydrates them before they become bindable catalog entries.
- Base: `GET works?source_category=real-drama&category=都市` returns only hydrated works whose
  tags contain the exact `都市` element. Bad: matching `都市剧` as `都市`, or
  presenting local rating or collection order as the source site's hot chart.
- Good: one source work appears in both `hot-drama` and its type rank with each
  official position preserved. Bad: storing rank identity in the single-valued
  `source_category`, or deleting the old rank before every source page succeeds.
- Good: a discovery-only summary shows its local poster and a pending badge;
  clicking it reports missing source details without requesting or fabricating
  detail/episodes. Base: a summary with no cover uses the existing placeholder.
  Bad: treating the summary episode count as authoritative playable episodes.
- Good: `GET works?source_category=ai-drama` filters the persisted upstream
  category. Base: an old direct-ID work with an empty category remains visible
  under 全部. Bad: guessing that work is AI-generated from its title or tags.

## 6. Tests Required

- `TestHongGuoWakeupCoalescesAndCancelClearsPending`: merged wakeups while busy,
  cancel clears pending state, and shutdown rejects later signals (including race).
- `TestHongGuoWakeupDrainsLaterDiscoveries`: blocked first HTTP request, a second
  summary registered after cutoff, serialized follow-up hydration and event triggers.

- `TestHongGuoSearchQueuesMissingDetails`: missing-only durable registration,
  repeated/duplicate result IDs, category preservation, pending-task selection,
  failure cooldown and no canonical work/episode/artwork creation.

- `internal/hongguo`: ID precision, movie evidence, count separation, snapshot
  whitelist, cancellation/HTTP bounds and explicit path IDs.
- `TestHongGuoDetailIsolationIdentityAndRollback`: real migration twice,
  stable IDs, old-data sentinel, transaction rollback and exact-tag list filtering.
- `TestHongGuoBindingGroupingAndStableProgress`: pending/rebinding, grouping,
  disable rollback and stable user state.
- `TestHongGuoRefreshRetriesAndShutdown`, `TestHongGuoArtworkLifecycle`:
  cooling, same-run deduplication, cancellation, mutual exclusion and repair.
- `TestHongGuoArtworkRunsAlongsideCollection`: parallel HTTP work, per-slot
  duplicate rejection, cancel/disable/shutdown of all three tasks and slot reuse.
- `TestHongGuoDiscoveryDefersDetailsAndResumes`: no detail HTTP during discovery,
  page-level restart, more-than-100 pending hydration, no summary overwrite,
  regular cooldown and explicit-ID bypass.
- `TestHongGuoRefreshStopsAtCompletionAndReportsChanges`: completed old works
  and due failures get no automatic detail request; new summaries are hydrated,
  unfinished unchanged details count separately, a later completion counts as
  updated then leaves the automatic candidate set, and explicit-ID refresh
  remains available. Discovery repository/service tests assert unique new IDs
  across repeated category and rank scans, even when an existing work had no
  discovery summary.
- `TestHongGuoDiscoveryCheckpointAndRetryIsolation`: atomic page rollback,
  fixed cutoff, failure cooldown, durable pending recovery, source-category
  propagation and one source-ID artwork row per summary cover.
- `TestHongGuoArtworkOwnershipMigration`: legacy poster source-ID backfill,
  duplicate-source rollback, idempotence, source-only/ownerless/work-only
  constraint behavior.
- `TestHongGuoNotFoundIsDeferredWithoutFailingBatch`: later-item continuation,
  72-hour cooldown, deferred task metrics, no fake work, and due-retry recovery.
- `TestHongGuoDiscoveryScansUntilCategoryEnd`: all three collected categories pass page 20,
  short/empty-page termination, saved-next-page 404 recovery, first/middle 404
  preservation, repeated-page rejection and nonempty-limit failure.
- `TestCategoryCompletionUsesSourceItemCount`: 23 raw entries complete the
  category; 24 raw entries remain a full page even if work projection deduplicates them.
- `TestRankUsesOfficialPageOrderAndPagination`: all four official routes use
  canonical pagination URLs, server-rendered order, and `rel=next`.
- Repository tests cover atomic stale-entry replacement, multi-rank-capable
  identity, established source-category preservation, and official position order.
- `TestHongGuoEmbyPlayableIdentityAndUserState`: versions, logical identity,
  visibility, people, pagination, Fields and legacy writer rejection.
- `TestHongGuoHTTPAccessAndStateIsolation`: JWT/admin/profile boundaries,
  local Range, STRM redirect, source DTO redaction, source/category/sort query
  validation and statistics routing.
- `TestHongGuoPlaybackStatistics`: filters, grouping, date intersection,
  pagination, deleted media and old-event isolation.
- PostgreSQL assertions count only with `MEDIASTATION_TEST_POSTGRES_DSN` set.
  Web lint/build and task-log/series-loading checks are required. Actual
  device playback and deployment migration remain separate acceptance checks.

## 7. Wrong vs Correct

Wrong: infer cross-season membership from similar titles or maintain editable group rows.

Correct: use the exact official album ID and season index; preserve source identities.

Wrong: create old metadata, then rely on Media's CHECK to reject its binding.

Correct: reject `media.CatalogSource != ""` immediately in the old mutation
entry point, before provider requests or metadata creation. Assert that a
rejected operation leaves the old metadata count unchanged.

Wrong: call `SaveDetail` with a classification inferred from a category summary.

Correct: save the summary in `hongguo_discoveries`; `PendingDiscoveries` hands
the ID to the existing detail parser/transaction before media binding is allowed.

Wrong: create a placeholder `hongguo_work` or episodes so a discovery summary can
appear in Web lists.

Correct: union the summary into the list projection with `hydrated=false`, reuse
the source-ID artwork queue, and keep all detail/binding APIs canonical-only.

Wrong: treat every page-N 404 as completion, or compare the filtered work count
with 24. Either can skip later pages.

Correct: use raw source item count for normal completion; recover an old 404
checkpoint only after confirming its preceding page is short.

Wrong: filter the serialized `tags` column with a substring search.

Correct: query exact JSON-array membership so similarly named tags remain distinct.

## Scenario: Independent Emby search indexes

### 1. Scope / Trigger

Global Movie/Series keyword searches across ordinary, HongGuo and NFO catalogs.

### 2. Signatures

`NewOpenSearchHongGuoBackend(SearchConfig)` uses the normalized ordinary alias
plus `_hongguo`, with `document_type=hongguo`. `HongGuoRepository.SearchCandidates`
returns logical works; `BackfillSearchIndex` shares the existing rebuild coordinator.

### 3. Contracts

Index canonical logical `hg-work-` / `hg-group-` titles, never shared metadata
surrogates. Before OpenSearch limits candidates, query current visible file-backed
identities and pass them as `CandidateIDs`; revalidate returned IDs in PostgreSQL.
The ordinary repository already adds independent NFO database candidates.
Emby merges candidates using the existing 100-result ranking limit and only
hydrates the final page. NFO existence must not route normal keyword search
through the full browse aggregation. Hierarchy and playback-state filters retain
their database paths; NFO does not borrow ordinary/HongGuo person identities.

SaveDetail, SaveAlbum and confirmed catalog cleanup refresh old/new identities
after commit, including the previous album title when its earliest member leaves.
Rebuilds replay dirty IDs before atomic alias activation. HongGuo incremental
writes serialize, but searches read the failure flag atomically without waiting
for index writes. Ordinary incremental concurrency is unchanged. Both source
warmups share configured batching/delay but run independently.

### 4. Validation & Error Matrix

Missing/unready/failing index -> PostgreSQL fallback. Known failed incremental
write -> bypass the index until a successful rebuild. More than 65,536 visible
IDs -> database fallback, never truncate permissions. Empty/locked/hidden scope
-> no results. Cancellation propagates; incomplete rebuilds are never activated.

### 5. Good/Base/Bad Cases

Good: a visible season makes one official album searchable. Base: absent index
still returns matching files. Bad: an NFO library disables ordinary OpenSearch,
or visibility is applied only after the candidate limit.

### 6. Tests Required

`TestHongGuoSearchIndexLifecycleAndVisibility`, `TestEmbySourceSearchUsesSeparateBackendsWithNFO`,
and `TestEmbyHongGuoSearchKeepsPlayedFilterWithoutNFO` run on isolated PostgreSQL.
`TestOpenSearchHongGuoAliasAndCandidateScope` checks HTTP isolation; opt-in
`MEDIASTATION_TEST_OPENSEARCH_LIVE=1` uses a uniquely named temporary index and
deletes it in cleanup. Retain existing metadata rebuild/cancellation tests.

### 7. Wrong vs Correct

Wrong: count all three expanded catalogs before testing the search title.
Correct: recall eligible candidates per source, rank once, hydrate one page.

## Scenario: Official cross-season albums

### 1. Scope / Trigger

Official App relationships replace manual grouping for Web and Emby display.

### 2. Signatures

`POST /novel/player/video_detail/v1/` takes string `series_id`; read
`data.video_data.related_album_id/season_index` with `UseNumber` and matching
`series_id_str` (fallback `series_id`). Reuse signed, bounded `appRequest`.
Keep webpage detail collection: App detail does not replace webpage rating
counts or first-visible timestamps.

### 3. Contracts

Store `related_album_id` and `season_index` on `hongguo_works` only.
`album_checked_at` and `album_retry_at` are private checkpoints. Successful empty
relationships clear the old relation and count as checked; errors preserve it
and mark the work for the next supplement pass without cooldown. The non-null
`album_retry_at` is a pending marker; even legacy future timestamps are eligible.
Webpage persistence never overwrites these fields.
Album detail business code `101001` (series taken down) succeeds with the
requested source ID as album ID and season 1. This affects presentation only;
it never deletes the work, episodes or files. For a successful matching work
with a valid nonempty album ID, a missing or non-integer `season_index` defaults
to 1. Valid integer seasons retain their value; out-of-range integers fail.
These defaults use the ordinary successful-save path and clear pending retries.
`GET groups/:id` is a read-only projection ordered by season then source ID;
the title is the earliest stored season's original title. Only series with
positive season numbers participate; duplicate season numbers retain distinct
source identities. No title parsing or manual override is available.
Task `hongguo_album` uses 100-row source-ID keyset batches with a fixed cutoff
and 200-ms cancellable spacing. It shares the refresh execution lock and runs
manually or daily (`hongguo.hongguo_album.enabled/interval_seconds`). A successful
detail refresh enqueues and attempts only that work's album. Query/validation
failure logs a sanitized warning and increments `album_warnings`, without
failing the detail task or entering its sync-failure queue. Ordinary refresh
never calls `backfillAlbums`: historical pending/failed albums belong to the
independent supplement task. The source-ID cursor ensures each work is tried
at most once per pass; the existing schedule/manual action starts the next pass.
Both paths share
the cancellable request spacing. Successful rows resume from private
checkpoints without reprocessing unchanged successful-empty rows.

### 4. Validation & Error Matrix

Invalid/mismatched IDs, malformed/trailing JSON, nonzero status other than `101001`, or album
seasons outside 1–100000 fail without clearing relationships. Missing/empty/zero
album IDs are successful standalone results. HTTP cancellation leaves work
pending; checkpoint failures stop the batch. Old manual write routes return 404.
Neither cancellation nor database/checkpoint failures may be downgraded to
album-query warnings. Diagnostic errors expose only bounded integer business
codes and fixed field descriptions/numeric season ranges, never upstream
messages, response bodies, credentials or arbitrary string field values.

### 5. Good / Base / Bad Cases

Good: different titles sharing an exact album ID form one series. Base: no
official relation remains standalone. Bad: merge identically titled works with
different album IDs or change episode UUIDs after backfill.

### 6. Tests Required

`TestParseAlbum`, `TestAlbumRequestAndCancellation`,
`TestHongGuoOfficialAlbumsAndBackfill`, `TestHongGuoAlbumFailureAndResume`, and
`TestHongGuoRetireManualGroups` cover precision, validation, signed requests,
pagination, independent webpage writes, failure/cancellation/restart, lock
exclusion and repeatable table removal preserving works. Existing search,
binding, playback and HTTP tests cover projected fields and stable identities.
`web/scripts/check-hongguo-batch.mjs` checks read-only albums and batch downloads.
`TestParseAlbum` also covers `101001` self-ID/season-1 defaults, missing/null/
non-integer season defaults, preserved valid seasons, overflow/range rejection,
other business-code failures and trailing-JSON rejection before defaults.
`TestAlbumErrorsIdentifySafeFields` covers business-code and field diagnostics
without upstream text leakage. `TestHongGuoRefreshDefersAlbumFailure` covers
manual/batch success, retained relations, immediate next-pass retry, separate warning
metrics, no historical-album sweep and supplement recovery on PostgreSQL.
`TestHongGuoRefreshDoesNotIgnoreAlbumCheckpointOrCancellation` covers failed
result/retry persistence and interrupted requests retaining terminal errors.

### 7. Wrong vs Correct

Wrong: treat an unknown App error as an empty album or continuously retry confirmed
standalone works. Correct: distinguish checked-empty and pending states; do not
add per-work cooldown for optional album query failures.

Wrong: fail an otherwise successful detail task because optional album queries
failed, or consume all historical album retries in every detail refresh.
Correct: warn for the current work and leave retries to `hongguo_album`.

Wrong: derive `source_category` from hydrated detail tags.

Correct: persist the category used by `Client.Category` on the discovery row and
carry it by source ID when the authoritative detail becomes a work.

## Scenario: Shared Web library presentation

### 1. Scope / Trigger

HongGuo library cards and details reuse ordinary series/file components. Catalog
isolation is a storage/state boundary, not a reason for a separate reduced UI.

### 2. Signatures

Use `GET /api/libraries/:id/series`, `/series/episodes?key=metadata:<hg-id>&season=N`,
and `/api/media/:fileID/{series,season,versions,credits}`. Series favorites use
`PUT /api/media/:fileID/series/favorite` with `{favourite:boolean}`. Legacy
`hongguo_id=<sourceID>` resolves through `key=hongguo:<sourceID>` within that library.

### 3. Contracts

`MediaCard` and `LibrarySeriesDetailSection` consume the existing response shapes.
Every whole-series field (title, overview, rating, tags, poster, credits) comes
from the first stored official season, ordered by season index then source ID.
This does not require a playable file for that season. If season 1 is not stored,
use the earliest stored season. Do not mix another season's richer fields into it.
Season details retain their own work metadata; missing season posters stay empty.
Missing episode overview/still/release date stays empty and uses ordinary UI rules.
Never claim `first_visible_at` is a release date or create metadata surrogates.

Filter files by library/profile visibility before grouping and pagination. Count
distinct episode identities, not files. Duplicate official season numbers retain
distinct `catalog_item_id` values; alternate files retain a shared episode identity.
Library card pagination starts from `hongguo_works` with an indexed `EXISTS`
check against visible bound files. Share the eligible work set between total and
page selection; aggregate episode counts and representative files only for the
returned source-work IDs. Do not sort every file before `LIMIT`. Keep the work
existence lookup boundary (`OFFSET 0`) and verify real PostgreSQL plans, including
JIT compilation costs: joining file aggregation into the page CTE can inflate
estimated rows and trigger expensive JIT optimization. Empty pages retain total.
Empty album/season fields remain standalone `hg-work-<UUID>` identities at season
1, with the source ID preserved; never persist a synthetic self-album just to
render this fallback, since that would change existing Web/Emby identities.
Playback and favorites keep source/user keys. Whole-series favorites affect only
visible member sources; movies use the existing source favorite API. Disable old
metadata editing and TMDb/Douban controls for HongGuo while retaining file operations.

### 4. Validation & Error Matrix

Visible file without discovery permission -> shared detail/credits/state permitted.
Invisible file or locked profile -> 404 and no state mutation. Unknown legacy
source in the requested library -> empty card result. Empty library -> empty page.

### 5. Good / Base / Bad Cases

Good: only season 2 has files, but stored season 1 owns the series header. Base:
standalone work is season 1; a movie uses `/media/:fileID`. Bad: missing episode
stills select a different detail layout, or two versions inflate episode count.

### 6. Tests Required

`TestHongGuoLibrarySeriesPresentation` covers first-season fields/credits without
files, own-season blanks, duplicate seasons/versions, scoped pagination/filtering,
history/favorites/user isolation, cross-season resume, legacy links and movies,
standalone season-1 fallback, unavailable works, and out-of-range page totals.
`TestHongGuoHTTPAccessAndStateIsolation` verifies shared details without discovery
permission and locked-profile read/write rejection. Run with isolated PostgreSQL.
`check-series-loading.mjs`, `check-series-presentation.mjs` and
`check-hongguo-discover.mjs` cover common loaders, missing fields, unsupported
controls, switching/restoring seasons/versions, old links and dual-theme layouts.

### 7. Wrong vs Correct

Wrong: add a HongGuo-only card/detail tree because episode fields are sparse.
Correct: adapt read projections to the common components and leave absent fields
absent. API mocks must register child routes before a bare parent URL: the browser
mock matcher can match URL prefixes and otherwise hide the child response.

## Scenario: 管理员下载与外部备份交接

### 1. Scope / Trigger

下载只输出文件，不能创建媒体、旧元数据、云盘对象或 STRM，也不自动追更。
`/admin/media/downloads` 位于文件空间；红果详情的下载按钮只对管理员显示。

### 2. Signatures

- 管理员 API：`/api/catalogs/hongguo/downloads` 下 GET/PUT `config`、GET 列表、POST 入队、POST `:id/cancel|retry`。
- 独立表 `hong_guo_download_works` 固定作品位置；`hong_guo_downloads` 按 `(source_id, episode)` 唯一，保存状态、租约、发布散列和暂存位置。
- Settings: `hongguo.download_root`, `hongguo.download_concurrency`, `hongguo.verification_concurrency`, `hongguo.hardware_verification`, `hongguo.download_priority`; task kind `hongguo_download`.
- Work management APIs under the same administrator group: `GET /works?page=1`, `GET /works/:source/episodes?page=1`, `POST /works/:source/retry`. Existing flat-list and single-episode APIs remain compatible.
- `GET /works?failed_only=true` filters source works with at least one currently failed episode before counting/pagination. Omitted/false keeps all works; invalid boolean returns 400. Filter by eligible source IDs, not by the outer episode status: summaries and expanded episodes retain every status. The Web checkbox defaults off, persists in the URL, resets page to 1 when toggled, cancels stale requests and distinguishes empty filtered results. When failures disappear after retry/polling, clamp out-of-range pages. Service/HTTP tests cover full summaries and failures behind 50 newer nonfailed works; browser checks cover toggling and reload persistence. Never filter only the loaded page or modify queue state when toggling.

### 3. Contracts

- PUT config accepts `{root:string, concurrency?:integer, verification_concurrency?:integer, priority?:"app"|"official"|"fallback"}` and returns `{root,temporary_dir,output_dir,concurrency,verification_concurrency,priority}`. Transfer concurrency is 1–10 and verification concurrency is 1–20; transfer defaults to 3 and verification to 2. Default priority is App. Preserve existing saved priorities; remaining sources follow App/fallback/official order with no duplicate. Missing/null optional fields preserve stored values. Save supplied keys together and wake the dispatcher only after success. Read the bounded key set in one query. Derive `downloading/` and `completed/` from one absolute root; CD2 backs up only the latter.
- Config also accepts optional `hardware_verification:boolean` and returns the resolved boolean. Default false; omitted/null preserves stored value, non-boolean JSON is rejected. The existing settings modal owns all five drafts. Cancel discards edits, polling cannot overwrite them, and dirty forms cannot close via Escape/backdrop. Use the shared Select for priority; Escape in its listbox closes only that menu. Show highest available quality as fixed behavior, not a setting.
- Work lists paginate 50 source IDs, with `total` counting works; each item includes `source_id`, `title`, `total` episode count and counts for all eight statuses, including `waiting_verify`, across all episodes. Order by first task creation descending, then source ID. Expanded episodes are sorted before pagination: downloading, verifying/publishing, waiting_verify, failed, queued, cancelled, completed; ties use episode/id. Do not group or prioritize only one flat episode page in the browser.
- Work retry locks only `failed` rows of that source, reuses the single-episode identity validation, and returns `{added,skipped}`. Missing local episode records stay failed and count as skipped; other database failures roll back the operation. Completed, cancelled and active rows must not change. Successful retry wakes the existing worker; never duplicate tasks or change placement.
- Download Space omits the duplicate content heading and uses Discover's underline source navigation. Storage settings live in `ModalShell`; cancel discards drafts, successful save closes, and polling cannot overwrite edits. Work cards default collapsed and preserve expansion during polling/retry; only expanded episode lists poll. Work-level retry covers every failed episode, irrespective of the expanded page. Keep single-episode operations inside the expanded list.
- POST 接收 `{source_id:string}`，返回 202 `{added:number}`；只能使用已有权威分集，本地视频 ID 缺失不阻止入队，执行时重新解析。GET 使用 `page`，每页 50 项，返回 `{items,total,page}`。
- 以 `FirstVisibleAt` 北京时间归档，不冒充上映日期：`2026/09/作品 [hongguo-ID]/Season 01/S01E001.mp4`；仅年份用 `2026/未知月份/作品`，无年份用 `未知年份/作品`。
- 作品首次入队固定根路径和相对目录；改配置、改标题、重试或补集不能迁移旧作品。标题中的方括号转换为全角，确保来源标签无歧义。
- New transfers, including source fallback and single/work retries, resolve the current video ID from a fresh `Client.Detail(source_id).VideoIDs[episode-1]` before requesting any of the three media sources. HongGuo uses source season S01. Missing/out-of-range IDs fail without reusing stale IDs unless the confirmed reconciliation below removes the task; detail fetch failures retain bounded source retries. Persist the resolved ID under the active lease before transfer. Do not renumber completed outputs or modify completed tasks. Raw/hash checkpoints bypass this lookup; retries preserve the checkpoint's original video ID so encrypted bytes use their original key. Tests: `TestHongGuoDownloadResolvesLatestEpisode`, `TestHongGuoDownloadSeparateVerificationAndRecovery`.
- 下载期间整部下架必须同时满足两次详情 `ErrNotFound` 和成功的标题搜索不含目标 source ID。单次 404、搜索异常、解析失败及 App 单集 `101002` 均不能触发整部删除。确认后事务清理没有 completed/raw/hash 保护的下载；仍有完成历史、可恢复下载、公共媒体或媒体绑定时保留资料。否则清理作品、分集、作品演职员关系/快照/海报记录、聚合成员、发现/榜单/同步失败和下载位置。共享人物、用户观看状态及完成文件不删除，内容寻址图片缓存不随记录删除。
- 分集变更须取得两份一致的完结列表，且总数、已更新数和数组长度一致；不完整列表保持原任务。空位还须原视频两次 `ErrVideoTakenDown` 才清理该未完成集；已清理空位不能阻塞后续下载。仅尾集减少或空位删除不改其余集号。集数变化且原有对应关系改变，或已知视频在相同集数下换位，视为重排：无完成/恢复/媒体绑定内容时按最新列表原路径重新入队；已有内容时保留并报需确认整部重下。仅同集号视频 ID 更新不推断为重新分集。
- `RemoveUnavailableHongGuoDownloads` 与入队使用 work → download 行的锁顺序；清理必须复核当前下载租约。入队在持有作品锁后读取分集，防止用旧列表重建已清理任务。被清理的执行者不能重建任务或继续发布；完成记录仍保护已被外部备份移走的文件。旧失败任务通过原有重试入口触发核对，不对列表 GET 做删除，也不新增后台失败轮询。
- `ResolveDownloadSource` resolves only the named app/official/fallback source. The worker tries priority first, then advances after the last attempted source in the configured three-source order on parse/network/incomplete-media errors; do not bypass priority to compare quality across sources. At most nine source attempts span transfer and verification stages. Cancel, local I/O and unknown tool errors do not trigger fallback. FFmpeg/FFprobe source classification accepts only explicit corrupt-media diagnostics, never arbitrary stderr. Stderr is bounded by `exec.Cmd.Output` and never exposed. Checkpointed publication failure never redownloads.
- Official pages validate both work and video IDs and currently expose one supported `main_url`; no verified multi-quality official schema exists in current fixtures. Fallback selects the highest numeric quality among valid URL/key candidates. App uses the fixed signed POST `/novel/player/video_model/v1/`, exact requested video ID and `need_all_video_definition=true`; reject redirects, bound request time/body and reuse the public-IP download transport. Parse object/string models and array/map variants; select highest compatible quality, short-side fallback for portrait dimensions, prefer H.264 at equal quality, skip bytevc2 and invalid URL/key candidates. Only full MP4 is supported, not HLS. URLs, device identifiers, signatures and keys remain ephemeral and absent from public DTOs, business rows and logs.
- Preserve official HTTP statuses, classify App code 101002 as video taken down, and expose only numeric unknown App status codes, never upstream message/debug text. A fallback `parse:0,url:...` may merely base64-encode the original request reference; it is not automatically a media URL. Public episode rows add `source,quality,width,height,codec,source_errors`; source errors store only the most recent safe failure per source in a JSON-serialized text column and manual retry clears them. Reset media attributes when switching sources. Before completion attributes describe the selected source; after successful verification, FFprobe dimensions/codec and short-side quality replace them. Legacy missing values stay unknown, with no file rescans or fabricated metadata. Existing AutoMigrate adds these nullable columns without rewriting completed files.
- 下载连接在 DNS 解析及重定向后拒绝私网、回环、链路本地和保留地址。不能为兼容代理 fake-IP 而静默放开此限制。
- When a hostname resolves to `198.18.0.0/15`, `downloadLookupIP` retries through AliDNS DoH (`dns.alidns.com`, pinned TCP endpoint `223.5.5.5:443`, verified TLS, no proxy or redirects). Literal IPs never trigger fallback. DoH has an 8-second deadline and 64-KiB response limit; only validated public A records may be dialed. Empty, malformed, mixed-private or failed answers fail closed; never connect to Fake-IP addresses. Ordinary public DNS resolution remains unchanged.
- `DownloadRequest` exposes only local DNS/Fake-IP/connect/timeout/TLS/redirect error categories or HTTP status; never forward `url.Error` text containing signed URLs. No new proxy setting or global network-policy change is introduced.
- 下载后 FFprobe 检查视频/时长，按配置执行 FFmpeg 完整解码，计算 SHA256 并同步磁盘；先保存发布检查点，再在租约/状态锁保护下通过无覆盖硬链发布。SHA256 用于本地产物恢复一致性，不代表来源校验和。
- `hongguo.full_verification` persists optional API field `full_verification:boolean`, default true. Omitted/null preserves the saved setting. Snapshot it when verification starts for all three sources; false skips only full audio/video decoding, including hardware fallback. Remux/decryption, probe/duration checks, hash and publication remain required. The settings modal disables hardware acceleration while full verification is off without clearing its saved preference. Active verification is not interrupted; later verification uses the saved value. `TestHongGuoDownloadFullVerification` covers decode gating and retained duration validation.
- Hardware verification snapshots the current setting when verification starts, without restarting or interrupting active work. Only the full decode command gains `-hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -hwaccel_output_format vaapi`; remux/probe/hash/publication remain unchanged. Any hardware command failure falls back once to the existing software decode on the same file before source-error classification; cancellation returns immediately without fallback. Record hardware use/fallback with fixed task messages, never raw stderr. Missing device/permissions/unsupported codecs therefore do not alone trigger redownload. Hardware success is not proof of software-equivalent corruption detection; neither guarantees source-byte identity. No automatic permission/device changes. Good: unsupported device falls back and passes software; bad: publish after both decoders fail or hide hardware errors as successful checks.
- One service instance admits independently configured transfer and verification/publication workers. Both limits are reread on dispatch and saving wakes the dispatcher: increases admit waiting work without restart; decreases drain active work without interruption, admitting only below the new limit. The completion channel covers both maximum pool sizes. `ClaimHongGuoDownload` selects rows without raw/hash checkpoints; `ClaimHongGuoVerification` selects checkpointed queued/waiting or expired active rows. Waiting verification is durable and has no active worker/FFmpeg process. Shutdown cancels and joins both pools; the two-second heartbeat stops cancelled/stale workers.
- `raw_size`, `source_tries`, `encrypted` and `duration` are private recovery fields; `source` also identifies the public download source. Sync raw bytes and stage/parent directories before recording raw size and releasing the transfer lease to `waiting_verify`. Verification validates raw file size, hardlinks it into a new lease-owned stage, syncs it, then changes the DB checkpoint before removing old names. Failed checkpoint adoption removes only the unadopted stage. Encrypted raw files re-resolve only the original source's key; keys and signed URLs are never persisted. Unavailable/changed key data may force bounded source fallback. Local filesystem/tool failures do not switch sources.
- Source validation failure clears the raw checkpoint and requeues for a fresh transfer slot, preserving at most nine source attempts across stages. Manual retries reset the source-attempt budget; shutdown does not consume an interrupted transfer attempt. `attempts` counts transfer claims, not verification claims. Publication remains hash/size checkpointed and no-overwrite; completed output is never auto-deleted. Temporary files can accumulate behind slower verification.

### 4. Validation & Error Matrix

- 匿名/非管理员：401/403；服务缺失：503。
- 非法 ID、分页、相对或文件系统根路径、与媒体库重叠的路径：400。
- 两个子目录不能执行同文件系统原子发布：配置失败。
- Fractional/out-of-range transfer or verification concurrency or unknown priority -> 400 before changing settings; old root-only requests preserve all optional settings. Unknown/local tool failures stop without cycling sources.
- App 101002 -> explicit taken-down error and normal bounded fallback; official 404 -> retain HTTP 404, not generic network failure. Empty/unsupported variants -> safe source failure. No compatible higher variant -> choose the highest remaining valid option, never claim all episodes have unrestricted 1080p.
- 同名目标不匹配发布检查点、媒体截断或时长不匹配：失败，不能覆盖目标或标记完成。
- Waiting verification -> no transfer slot consumed; missing/wrong-length raw file -> bounded redownload; cancelled verifier -> old token cannot update/re-publish, immediate retry gets a distinct stage.
- 完成任务不能取消/删除；只允许重试失败或取消任务。

### 5. Good / Base / Bad Cases

- Good：验证完成才进入 `completed`，CD2 与 Symedia 在外部处理；分层后的 STRM 保留来源标签与源季集编号。
- Base：日期未知直接进入 `未知年份/作品`。
- Bad：让 CD2 备份 `downloading`，或将进程退出成功/文件存在当作完整性证明。
- Good: set fallback first and use official after a 403 or corrupt MP4. Base: official returns one URL. Bad: claim global highest quality from one URL, interrupt running downloads when reducing concurrency, or retry a missing FFmpeg installation against every source.

### 6. Tests Required

- 下载专项 Go 测试覆盖身份与网络拒绝、目录/标签、持久化去重与固定位置、取消/过期租约、发布恢复/冲突、真实本地 FFmpeg 校验和接口权限；数据库测试须提供 `MEDIASTATION_TEST_POSTGRES_DSN`。
- App tests cover highest compatible selection, portrait size, encrypted key parsing, safe takedown/HTTP errors, signed exact-ID POST and cancellation. Three-source service tests verify configured orders, original-source key re-resolution, persisted errors and FFprobe attributes. `MEDIASTATION_TEST_HONGGUO_APP_LIVE=1` enables `TestDownloadAppLive` and `TestHongGuoDownloadAppPipelineLive` (the latter requires isolated PostgreSQL); both use auto-cleaned temporary media, never production queue rows. Real success of one episode is not proof of universal availability or 1080p access.
- `TestHongGuoDownloadHardwareFallback` checks exact hardware arguments, disabled/success/fallback/both-fail/cancel paths without requiring a GPU. Config tests cover default-off, persistence, omitted fields, false updates and invalid types. Opt-in `TestHongGuoDownloadHardwareLive` reads `MEDIASTATION_TEST_HONGGUO_VAAPI_FILE`, verifies a preexisting file without changing it, and asserts no software fallback occurred. Enabling hardware does not certify every codec or driver version.
- `TestHongGuoDownloadWorkGroupingAndRetry` covers >50 episodes, whole-work status counts, per-source episode pagination, missing-ID skips, repeated retry, preserved completed paths and isolation from other works. HTTP access checks cover all three work endpoints; browser checks cover collapsed loading, bulk retry, episode pagination and modal save/cancel/responsive behavior.
- `TestHongGuoDownloadSeparateVerificationAndRecovery` proves three blocked transfers coexist with two blocked verifiers and one waiting verifier, verifier cancel/immediate retry isolates tokens/stages, and service reconstruction publishes staged files without downloading again. `TestHongGuoDownloadEpisodePriorityBeforePagination` places active episodes beyond index 50 and asserts first-page priority plus final completed ordering. `TestHongGuoSearchResultsAreReadOnlyAndKeepSourceOrder` checks list/search group membership without Media rows.
- `TestHongGuoDownloadUnavailableConfirmation`、`TestHongGuoDownloadEpisodeReconciliation`、`TestHongGuoDownloadConcurrentRemoval` 使用隔离 PostgreSQL 覆盖确认/恢复/搜索故障、资料清理与回滚、旧租约拒绝、完成/raw/hash/media 保护、尾集减少、空位及重复核对、不完整/不稳定列表、新版本重排和两个执行者同时清理；并运行 `-race`。
- Cancellation tests open connections with the isolated schema in pgx RuntimeParams. A session-only `SET search_path` is lost if cancellation discards a connection; reconnecting must never silently use the default schema. Keep this test setup scoped to download tests and close its pool before schema cleanup.
- `TestHongGuoDownloadSettings`, `TestHongGuoDownloadConfigHTTP`: defaults, persistence, omitted-field compatibility, invalid integer/priority and unchanged settings on rejection. `TestHongGuoDownloadDynamicConcurrencyAndShutdown`, `TestHongGuoDownloadCancelThenRetryRunningLease`: concurrency 3→1→2, no interruption/duplicate claims, old HTTP cancellation, new-lease publication and joined shutdown. Source priority tests cover both orders, parsing/403/truncation/corrupt-file fallback and local errors; command classification tests cover unknown/local diagnostics and redaction. `TestDownloadFallbackSelectsHighestValidQuality` rejects invalid higher options. Run with isolated PostgreSQL and `-race`.
- `web/scripts/check-download-space.mjs` 覆盖管理员/普通用户入口、入队、重试、保存与未保存输入保持、响应式；另运行 Web lint/build。
- `MEDIASTATION_TEST_HONGGUO_DOWNLOAD_LIVE=1` 为真实来源下载单独验收开关；通过本地样例不能替代真实来源、CD2/Symedia 或部署迁移验收。
- `TestDownloadFakeIPFallback`, `TestDownloadDNSFailureDoesNotReturnFakeIP`, and `TestDownloadErrorsAreRedacted` cover fallback boundaries, malformed/unsafe answers, fail-closed behavior and URL redaction. Opt-in `TestDownloadFakeIPLive` reads one real episode completely and decodes it with FFmpeg in a test-cleaned temporary directory; this does not certify queue publication or cloud integration.

### 7. Wrong vs Correct

Wrong：下载临时文件直接写到备份目录，取消后旧工作者继续覆盖目标。

Correct：隔离暂存、验证并持久化检查点，在有效租约与行锁下无覆盖发布。

Wrong: treat fallback's encoded request reference as a video URL, or hide App's taken-down reason behind the final webpage error. Correct: accept only supported media/key candidates and retain bounded per-source errors alongside the final task error.

Browser test mocks match first registration: do not assume adding the same route replaces an earlier static response. Assert outgoing configuration separately from its mocked response; real persistence belongs in the HTTP/database test.

## Scenario: Scheduled supplement and relocated execution history

### 1. Scope / Trigger

Download Space owns download execution history. Task Center owns the independent `hongguo_download_supplement` manual/scheduled task; this is new-work acquisition, not episode catch-up.

### 2. Signatures

`POST /api/catalogs/hongguo/downloads/supplement` accepts `{count: integer}` and returns 202 `{requested,candidates,works,episodes,skipped,failed}`. History remains `GET /api/tasks/definitions/hongguo_download/executions`.

The direct endpoint remains for compatibility. Task Center uses `POST /api/tasks/definitions/hongguo_download_supplement/run` with `{count}` (202 starts background work), and the existing schedule endpoint with `{enabled,interval_seconds,count}`. Settings keys are `hongguo.download_supplement.enabled`, `.interval_seconds`, `.count`; defaults are disabled, 86400 seconds and 10 works. Save all three atomically; manual count never changes schedule count. Omitted schedule count preserves the saved value.

### 3. Contracts

Count means 1–100 source works, not episodes or logical groups. Select canonical works using discovery's first-visible/created/id descending ordering, excluding comic, invalid IDs, missing/invalid episode video IDs, episode counts outside 1–10000, and any existing download placement or episode row regardless of status. Never use task logs or Media membership as eligibility. First-time placement conflict inside the existing enqueue transaction prevents concurrent duplicate work; normal manual enqueue still supplements newly available episodes. Return actual committed counts, not requested counts. Failed/concurrently claimed candidates may leave the request short; no automatic candidate-refill loop or source hydration.

Hide the download definition only from task-center enumeration; preserve its registration, execution persistence, log mapping and history authorization. The settings-adjacent button opens paginated download execution history only. The separate supplement definition retains each round's trigger, actual counts and failures. Each round adds new works, not a queue target. Scheduler prevents overlapping rounds; the shared supplement service also guards the compatibility endpoint. Scheduler shutdown cancels and joins its supplement run. Explain that enqueue/transfer success is not final file completion. Cancel stale history reads and prevent duplicate submissions. A lost request response can leave already committed works queued: inspect Download Space before requesting more.

### 4. Validation & Error Matrix

Anonymous/non-admin -> 401/403. Invalid/fractional count or body over 4 KiB -> 400 without queue writes. Disabled source or missing root -> background task failure (direct compatibility endpoint rejects synchronously). Insufficient candidates -> successful round with actual counts. Per-work enqueue failure -> failed round with counts, preserving prior committed works. Concurrent rounds are rejected. No new schema, credentials or upstream URLs.

### 5. Good / Base / Bad Cases

Good: request 10, find 3, enqueue 3 and explain the shortage. Base: none eligible, enqueue zero. Bad: automatically requeue cancelled downloads or claim 10 works when only 3 were committed.

### 6. Tests Required

`TestHongGuoDownloadSupplement`, `TestHongGuoDownloadSupplementOrderAndConcurrent`, `TestHongGuoDownloadHistoryHiddenFromTaskCenter`: real PostgreSQL filtering/order/counts and concurrent first placement, existing state preservation and retained history. Scheduler/HTTP supplement tests cover defaults, atomic validation, reload, manual-count isolation, actual timer-loop trigger, concurrency, shutdown cancellation and failure history. `check-download-space.mjs` covers history paging and absence of supplement entry; `check-hongguo-supplement-task.mjs` covers manual quantity, schedule settings, invalid count, failure retention, focus restoration and dual-theme narrow layouts.

### 7. Wrong vs Correct

Wrong: delete the task definition or filter only the loaded discovery page. Correct: preserve history identity, hide its task-center row and select bounded eligible works server-side before LIMIT.
