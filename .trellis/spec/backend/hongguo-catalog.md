# HongGuoDB Catalog Isolation

## 1. Scope / Trigger

Apply when changing HongGuo collection, file binding, grouping, user state,
statistics, or Web/Emby projections. This is a separate catalog, not a TMDb
provider and not a source of remote playable streams.

## 2. Signatures

- Catalog tables: `hongguo_discoveries`, `hongguo_works`, `hongguo_episodes`, `hongguo_people`,
  `hongguo_credits`, `hongguo_snapshots`, `hongguo_artworks`,
  `hongguo_sync_states`, `hongguo_sync_failures`, `hongguo_groups`,
  `hongguo_group_members`, `hongguo_media_bindings`.
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
  plus local `artwork_id` and parsed `tags`. One LEFT JOIN loads artwork IDs;
  do not fetch each work's full detail to build poster cards.
- `/api/admin/playback-stats?system=hongguo` uses the existing filter/DTO
  contract but only reads independent events; omitted system remains `catalog`.
- Emby identities: `hg-work-`, `hg-group-`, `hg-season-`, `hg-episode-`,
  `hg-person-` followed by internal UUID. Provider ID values remain upstream IDs.

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
  `SaveDiscoveryPage` batch-upserts summaries and the next-page checkpoint in
  one transaction. It never calls detail HTTP, writes canonical works, generates
  episodes/artwork, or rebinds files. Task updates are emitted once per page.
- Discovery continues from each category checkpoint until a valid empty page,
  without a 20-page per-run cap. A page containing only IDs already seen in
  this category during this run is a pagination-stall error, not completion.
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
- `PendingDiscoveries(ctx, after, cutoff)` keyset-pages 100 summaries by source
  ID, excluding existing canonical works and all failure rows. Pending state is
  derived from business tables, not a second mutable status or execution log.
  Refresh drains pending work present at its fixed start cutoff, then retries
  up to 50 failures selected at start and refreshes up to 100 existing works
  older than 24 hours using the persistent refresh cursor. Later discoveries
  wait for the next run; explicit source-ID refresh bypasses that cooldown.
  Only hydrated works appear in the existing Web/Emby catalog; discovery rows
  cannot be grouped or bound. Repeated discovery cannot overwrite details.
- Discovery, refresh and artwork each have independent task slots and may run
  together. Each slot rejects duplicate runs. Cancel, disable and shutdown
  cancel all three slots; shutdown joins all three before DB close.
  Artwork uses a fixed pass cutoff and checks source URL on write-back, so newly
  collected images wait for the next pass and stale downloads cannot overwrite
  refreshed URLs. Discovery checkpoints and failure retries are
  business tables, not task logs. Normal refresh excludes failed IDs; only due
  retries request them. Successful due retries are not repeated in the same run.
  Cancellation retains `context.Canceled` identity and finishes interrupted.
- Disabling stops source tasks and new bindings; a disabled file upsert rolls
  back rather than replacing an existing binding. Existing files, metadata and
  user state remain readable. No destructive uninstall is provided.
- Manual groups only change presentation. State key is `(user_id, source_id,
  episode_number)`: episode 0 is work favorite, episode 1 is also movie progress.
  Events are unique per user/session/source/episode and survive file deletion
  and manual unwatch. User tables are excluded from `HongGuoModels()`.
- Apply file/profile visibility before logical grouping, count and pagination.
  Emby mixed pages merge identities in SQL before pagination and batch-load only
  the current page. Reuse existing old-catalog payload builders with `Fields`;
  do not call complete `Item` once per list row. Omitted Fields retains existing
  defaults; explicit Fields controls People/ProviderIds/MediaSources.
- Source statistics rank by source work, even after manual grouping. Display
  current group title/season without changing the event identity. Missing or
  rebound files must not be linked as the original event's available media.
- Web source pages rebuild when authenticated user/profile changes. Task URL
  system is `common|catalog|hongguo`; stats URL is `catalog|hongguo`.
- Web labels use 红果短剧; internal source IDs remain `hongguo`. Discovery is
  `/discover?system=hongguo`. Works/list/detail/episodes and group-detail GETs
  require `can_view_discover`; media, artwork and user-state routes retain
  existing authentication and file/profile visibility rules.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Anonymous source API | 401 |
| No can_view_discover on works/list/detail/episodes or group-detail GET | 403 |
| Non-admin import/group/status/cancel/pending/statistics | 403 |
| Invalid source ID, pagination or stats system | 400 |
| Hidden file/library or locked profile playback | Not found; no state mutation |
| Duplicate group season or movie member | Reject transaction |
| Source file sent to old metadata writer | Reject before any old metadata insert |
| Disabled source upsert | Error; prior file/binding unchanged |
| Detail/image network failure | Preserve successful data; independent retry |
| Discovery checkpoint write fails | Roll back the entire summary page |
| Discovery repeats a failed or hydrated ID | Preserve failure cooldown and authoritative work |
| Discovery page has no unseen IDs this run, or nonempty page 10000 | Fail explicitly; do not claim full coverage or reset to page 1 |
| Category page has fewer than 24 raw entries | Save summaries and reset that category to page 1 in one transaction |
| Category page N > 1 is 404 and page N-1 is still short | Re-save the short page, reset to page 1, and log the recovery context |
| Category first page is 404, or page N-1 has 24 raw entries | Fail with category, page and relative path; preserve the checkpoint |

## 5. Good / Base / Bad Cases

- Good: group source work B as season 3; `S01E002` still binds B episode 2.
- Base: an ungrouped source work remains a usable standalone first season.
- Bad: match by similar titles, fabricate release dates, or store a source ID
  in `media.metadata_id`.
- Good: local file Range yields 206; user-provided STRM retains the shared
  redirect path. No remote video URL is extracted from the source website.
- Good: one 24-item category page records summaries without 24 detail requests;
  a later refresh hydrates them before they become bindable catalog entries.

## 6. Tests Required

- `internal/hongguo`: ID precision, movie evidence, count separation, snapshot
  whitelist, cancellation/HTTP bounds and explicit path IDs.
- `TestHongGuoDetailIsolationIdentityAndRollback`: real migration twice,
  stable IDs, old-data sentinel and transaction rollback.
- `TestHongGuoBindingGroupingAndStableProgress`: pending/rebinding, grouping,
  disable rollback and stable user state.
- `TestHongGuoRefreshRetriesAndShutdown`, `TestHongGuoArtworkLifecycle`:
  cooling, same-run deduplication, cancellation, mutual exclusion and repair.
- `TestHongGuoArtworkRunsAlongsideCollection`: parallel HTTP work, per-slot
  duplicate rejection, cancel/disable/shutdown of all three tasks and slot reuse.
- `TestHongGuoDiscoveryDefersDetailsAndResumes`: no detail HTTP during discovery,
  page-level restart, more-than-100 pending hydration, no summary overwrite,
  regular cooldown and explicit-ID bypass.
- `TestHongGuoDiscoveryCheckpointAndRetryIsolation`: atomic page rollback,
  fixed cutoff, failure cooldown, durable pending recovery and no summary artwork.
- `TestHongGuoDiscoveryScansUntilCategoryEnd`: all four categories pass page 20,
  short/empty-page termination, saved-next-page 404 recovery, first/middle 404
  preservation, repeated-page rejection and nonempty-limit failure.
- `TestCategoryCompletionUsesSourceItemCount`: 23 raw entries complete the
  category; 24 raw entries remain a full page even if work projection deduplicates them.
- `TestHongGuoEmbyPlayableIdentityAndUserState`: versions, logical identity,
  visibility, people, pagination, Fields and legacy writer rejection.
- `TestHongGuoHTTPAccessAndStateIsolation`: JWT/admin/profile boundaries,
  local Range, STRM redirect, source DTO redaction and statistics routing.
- `TestHongGuoPlaybackStatistics`: filters, grouping, date intersection,
  pagination, deleted media and old-event isolation.
- PostgreSQL assertions count only with `MEDIASTATION_TEST_POSTGRES_DSN` set.
  Web lint/build and task-log/series-loading checks are required. Actual
  device playback and deployment migration remain separate acceptance checks.

## 7. Wrong vs Correct

Wrong: create old metadata, then rely on Media's CHECK to reject its binding.

Correct: reject `media.CatalogSource != ""` immediately in the old mutation
entry point, before provider requests or metadata creation. Assert that a
rejected operation leaves the old metadata count unchanged.

Wrong: call `SaveDetail` with a classification inferred from a category summary.

Correct: save the summary in `hongguo_discoveries`; `PendingDiscoveries` hands
the ID to the existing detail parser/transaction before media binding is allowed.

Wrong: treat every page-N 404 as completion, or compare the filtered work count
with 24. Either can skip later pages.

Correct: use raw source item count for normal completion; recover an old 404
checkpoint only after confirming its preceding page is short.
