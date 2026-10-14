# Discover Feed Loading Contract

## 1. Scope / Trigger

Use this contract when changing `DiscoverPage`, `discoverAPI.feed`, the discover feed handler, section caching, or discover poster loading. It prevents repeat Provider calls, stale-request UI writes, and page-wide image cache busting.

## 2. Signatures

```ts
discoverAPI.feed(
  sectionKeys: string[],
  page?: number,
  refresh?: boolean,
  signal?: AbortSignal,
): Promise<DiscoverFeedResult>
```

```http
GET /api/discover/feed?sections=<comma-separated keys>&page=<positive integer>&refresh=1
```

```go
loadDiscoverSections(parent context.Context, svc *service.Container, keys []string, page int, refresh bool) []discoverSectionResult
```

## 3. Contracts

- Missing `refresh`, or any value other than `1`, may return an existing non-empty section cache entry within the service's six-hour TTL without calling the Provider.
- `refresh=1` bypasses only the pre-request cache fast path. Provider failure still resolves in this order: section cache -> configured Provider fallback -> per-section error.
- Responses keep `items[sectionKey]` and `_meta[sectionKey]`; metadata includes `page` and `has_next`, with `stale`, `fallback`, `warning`, `error`, or `disabled` only when applicable.
- Same-Provider jobs remain serial; different Provider groups use the existing bounded worker count. Cache hits do not alter input ordering.
- Douban cache entries and cloned responses keep the same official large poster URL. Discover feed does not prefetch artwork; source image tasks own downloads. The image proxy applies the current configured CDN only during an image-cache miss, so changing that configuration never mutates feed URLs or requires section-cache invalidation.
- Discover mounts only the selected source. TMDb/Douban rankings are under
  `system=tmdb|douban&section=rank`; Bangumi uses `system=bangumi`.
  Filter section definitions by provider before choosing the URL-owned `rank`.
  Keep selected-key identity stable during replace normalization so the first
  page is not requested twice. Rank changes restart at page one; refresh and
  continuation request only the selected section. Rankings never mount search;
  category pages retain search. Clear legacy ranking search parameters before
  they can select a search result branch.
- Starting a new feed batch aborts the previous controller. Aborted batches must not update rows, errors, loading state, or localStorage cache.
- Commit the continuation cursor only after its page succeeds without a section
  error. Keep prior rows, pause observers on failure and retry the same failed
  page. A failed section-definition read shows an error/retry state, not an empty
  source. Section in-flight identity includes `auth.sessionVersion`, so even a
  same-account relogin cannot reuse an obsolete request.
- Discover poster URLs stay stable across mounts and manual data refresh. Use native `loading="lazy"`; only a failed individual image retry may add `v=r1` through `v=r3` and `refresh=1`.
- Missing/failed poster images retain their work card, title and detail trigger
  with a placeholder rather than removing the work from a row.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| `page < 1` or invalid | Normalize to page `1`. |
| Unknown section key | Drop it without failing other sections. |
| Ordinary request with valid non-empty cache | Return cached items with their stable official Douban poster URLs; do not call Provider. |
| Ordinary request with empty or expired cache | Call Provider through existing scheduling. |
| `refresh=1`, Provider succeeds | Return fresh items and replace the section cache when non-empty. |
| `refresh=1`, Provider fails, cache exists | Return cache with `stale=true`, correct `has_next`, and do not call fallback. |
| Provider fails without cache | Try configured fallback, then return a per-section error. |
| TMDb/Douban first page is empty | Treat as failure; retain the previous snapshot and success time. |
| Continuation succeeds with empty items | Return empty items and do not cache them. |
| Browser aborts an old batch | Do not display a request error or clear the active batch's loading state. |

## 5. Good / Base / Bad Cases

- Good: a hot page visit reuses section cache, keeps poster URLs unchanged, and transfers cached images without page-wide version parameters.
- Base: a cold cache calls Providers with the existing concurrency limits and fills both service and localStorage caches.
- Bad: changing one row page re-fetches unchanged rows, or refresh adds a timestamp/version to every poster URL.

## 6. Tests Required

- Handler: cache hit returns the keyed response and does not call Provider.
- Handler: mixed cache hit/miss preserves order and still loads misses.
- Handler: `refresh=1` calls Provider, returns fresh data, and a following ordinary request reuses that data.
- Handler: refresh failure with cache asserts items, `stale=true`, `has_next`, and absence of `fallback`.
- Handler: update the Douban image CDN between two cache-hit requests; assert both responses and the stored cache keep the same official poster URL.
- Web checks: lint and build pass; browser verification observes one section on row pagination, `refresh=1` on manual refresh, an actual abort during rapid actions, stable poster URLs, and `r1`-`r3` only on failed-image retry.

## 7. Wrong vs Correct

### Wrong

```ts
setImageVersion(String(Date.now()))
discoverAPI.feed(allSelectedSections, changedRowPage)
```

This forces every poster to use a new URL and re-fetches rows whose page did not change.

### Correct

```ts
activeController.abort()
discoverAPI.feed([changedSection], changedRowPage, false, nextController.signal)
```

Keep ordinary poster URLs stable and use `refresh=1` only for explicit recommendation-data refresh or an individual failed-image retry.

## HongGuo catalog dialogs

- Both HongGuo and HuangGuo AI ranking rows display the same green `↓ 已下载`
  badge inside their poster as category cards. Keep the metadata-row text so the
  state remains readable when the ranking poster is collapsed. Use the existing
  `downloaded` flag (at least one completed episode record); do not infer whole-work
  completion or file existence. `check-catalog-rankings.mjs` asserts both sources,
  absence for undownloaded works and poster containment across themes/widths.

- HongGuo discovery search composes two existing requests: official `search(keyword)` once per explicit search, and local `list(keyword,'','','',page)` with page size 50, starting at page 1 regardless of legacy URL page. Scroll and local retry never repeat the official request. Separate controllers and retry counters retain successful results when either source fails; failed local pages cannot advance until retried successfully. Disconnect the observer before incrementing, and pause it during loading/errors/detail dialogs.
- Merge by source ID with official first-seen order and hydrated metadata precedence; do not downgrade a canonical work to a pending summary. Show the number of deduplicated displayed cards, explicitly label official first-screen plus collected local data, and never imply the count covers all upstream hits. Local queries include title substrings or exact source IDs and pending summaries, independent of category/rank when keyword search is active. Category saves update both displayed sources.
- Appending preserves selection and does not remount the feed. Explicit repeat-search/refresh restarts search-local page 1 and both sources; category/rank refresh retains its existing URL-page semantics. Query/account remount aborts obsolete reads. Local pagination remains a live catalog view, not a frozen snapshot; catalog mutations can change membership/order between pages. No official pagination or additional import/refresh task is fabricated by local pagination.
- `web/scripts/check-hongguo-search.mjs` verifies source overlap, hydrated precedence in both directions, >50 matches, independent errors/retries, no skipped failed page, no repeated official search on scroll, selection preservation, empty sources, explicit refresh, stale-response exclusion and responsive source notices.

- Fileless or castless works remain valid discovery details: hide empty cast
  sections instead of showing a placeholder. HuangGuo AI details show work
  metadata and download actions, without confirmed-episode/local-file sections
  or requests to load those sections. Verify cast-present and cast-empty HongGuo
  fixtures plus HuangGuo AI poster clicks in their browser checks.

- Visual parity means shared presentation components, not merely `ModalShell`: use `DiscoverModalHeader`, `DiscoverArtworkPanel`, `MetadataFacts`, `MetadataTags`, `MetadataOverview` and `MediaCredits`, with the same `lg:grid-cols-[260px_1fr]` layout. Keep synopsis, tags and credits in the metadata column. Preserve the HongGuo launch-date label; do not invent runtime, country, language or backdrop data. Verify rendered screenshots as well as interaction tests when changing this layout.

- `HongGuoPage` keeps its list mounted when URL `id` changes; only `HongGuoDetailModal` is keyed by the source ID. Closing removes `id` and legacy `media_page`, preserving filters and loaded rows. Detail requests abort on close or identity change.
- Cards, including summary-only and other-category cards, open the same dialog outside administrator selection mode. Summary remains visible if detail loading fails, with a read-only detail retry. Keep the administrator download action; show source-category editing only for empty/`other` categories and hide it after successful classification. Do not show favorites, manual provider refresh, download-space navigation, media files, playback or grouping UI in the detail dialog. Opening the dialog must not request favorite state. Backend favorites and task-center refresh remain unchanged.
- Administrator discovery multiselect uses source-ID-deduplicated ordered work snapshots, retained during incremental loading and cleared on query/account remount or exit. Pending summaries cannot be selected. Reuse download config/enqueue APIs sequentially, report accepted works/new tasks and retain only failures; missing root blocks all enqueue calls. Stop issuing subsequent requests and ignore results after unmount; an already submitted request may finish. Actual download concurrency stays server-owned.
- Creating a group from selection requires 2–1000 series. A ModalShell shows poster/title/source ID and contiguous season numbers, initially in selection order; explicit up/down actions determine the payload to existing `saveGroup(undefined,title,members)`. Never infer season order from titles or overwrite other groups. Cancel writes nothing, failures preserve the draft, submission blocks duplicate actions, and focus stays in the form and returns to selection controls on close. Logical grouping never renames files or changes source coordinates/progress.
- `web/scripts/check-hongguo-batch.mjs` covers ordered payloads, move/cancel/retry, pending/movie restrictions, partial download failures, missing root, delayed submission after unmount, admin isolation, and dual-theme responsive layouts. Browser mocks verify frontend requests, not database persistence or live downloads.
- Group ordering also supports vertical Reorder drag using the installed motion package, a touch-none handle, arrow keys and retained up/down buttons. Keep the scrollable `motion.div layoutScroll` outside Reorder.Group so the installed auto-scroll implementation finds it as an ancestor. Save success overlays group_id on merged local/remote cards, so older list responses cannot erase a confirmed write. The override is query/account scoped and clears on explicit refresh; do not collapse cards or clear unrelated loaded results.
- `HongGuoGroupBadge` is a sibling of the poster button, never a nested button. Hover/focus/tap loads the existing group endpoint only when needed; show other source titles with seasons, not Media-dependent ownership. The portal is viewport-bounded and supports pointer transition, Tab/ArrowDown entry, Escape/focus return, error retry and stale-request cancellation. Pointer leave must not dismiss a keyboard-focused panel. Resize events target Window, not Node: guard DOM containment checks. Tests exercise actual mouse drag, browser-native emulated touch events, keyboard order, saved badges, stale list results and focus entry/exit.
- The group link is a transparent 14px icon inside a 24px target immediately before the bottom-right episode label, aligned in one row. The footer overlay passes clicks through except for the icon; truncate long labels instead of wrapping. Open the portal above/below based on available space, cap its height and never cover the trigger: a viewport-clamped overlapping panel can reopen on hover after Escape. Browser checks assert icon/label geometry, non-overlap, focus return and narrow dual-theme layouts.
- Download rows are normally single-line, with path/error details on demand, distinct text/icon/color statuses and 2px gradient progress. Work progress means completed tasks / total current tasks, never upstream series completeness; only a positive total whose tasks are all completed shows the all-complete badge. Unknown transfer totals remain indeterminate. Check all statuses in both themes at 390/768/1024/1440 and retain server-side ordering before pagination.
- Download Space's existing episode details also show source, quality, resolution, codec and each source's latest failure; never URLs or keys. Legacy missing fields show 未记录. Pending source dimensions are labeled 待校验, completed values come from FFprobe. Priority options show the complete App/fallback/official order, preserving saved preferences; source changes do not reset active transfers or add a quality setting.
- `HongGuoLibraryView` uses library-local `hongguo_id` to open `HongGuoLibraryDetail`, which owns existing file pagination and grouping controls. Library cards must not redirect to discovery for playback. Playback return state keeps the library URL.
- `web/scripts/check-hongguo-discover.mjs` verifies dialog opening, no list refetch, focus return, no discovery media requests, explicit refresh, category editing and responsive layouts.

## TMDb detail and library badges

### TMDb search

- `POST /api/discover/search` requires `can_view_discover` and accepts
  `{query, kind: "multi"|"movie"|"tv", page}`; query is 1–100 Unicode
  characters, page is 1–500, body limit is 4 KiB. Responses are private/no-store.
- Search uses TMDb paginated search summaries, excludes people/adult results,
  and deduplicates by work kind plus TMDb ID. The response contains `items`,
  `page`, and upstream-derived `has_next`, even when filtering leaves a page empty.
- TMDb search/feeds persist summaries in the independent TMDb center. They do not
  enqueue explicit hydration, create local Metadata/Media, fetch result details,
  or recursively download images. Existing periodic source maintenance naturally
  selects incomplete summaries; no additional discovery-to-refresh queue is needed.
- URL-owned `tmdb_query` and `tmdb_kind` are independent from HongGuo query keys.
  Abort old requests on search change/unmount; retain accumulated rows on page
  failure and retry that page. Reuse `ContentRow` badges and `DiscoverDetailModal`.
- Tests: `TestDiscoverSearchTMDbPaginationAndIdentity`,
  `TestTMDbCenterDiscoverySearchOnlySummaries`,
  `TestDiscoverSearchRejectsInvalidInput`, and discovery permission tests.

### 1. Scope

TMDb discovery previews remain dialogs without season/episode lists. The detail
GET reads the source center; a separate controlled ensure POST requests missing
source components. Neither operation edits local metadata or media.

### 2. Signatures

- `GET /api/discover/tmdb/:kind/:id` (`kind=movie|tv`).
- `POST /api/discover/library-status`, body `{ "items": [{ "tmdb_id": 123, "media_type": "tv" }] }`.

### 3. Contracts

- Both routes require `can_view_discover`; successful responses use `Cache-Control: private, no-store`.
- Detail returns public Match fields, positive deduplicated `runtime_minutes`, and `credits`; no raw provider payload or season/episode fetches. Only a unique, same-kind stored Douban identifier supplies the Douban link.
- Detail resolves exact TMDb ID + work kind in the source center, returning source
  titles, overview, artwork, runtime and credits even when linked local metadata
  exists. Local associations supply library badges and safe provider linkage,
  never source presentation. Preserve saved local titles/translations/roles and
  artwork choices. Library/NSFW filters apply to ownership badges. No media paths
  or playback targets are exposed.
- Missing detail triggers `/catalogs/tmdb/subjects/:uuid/ensure`, followed by
  two-second polling up to 45 attempts. At the limit retain current data, explain
  that background processing continues and offer manual reread. Pending state is
  a safe flag, not a job projection. Complete scopes never requeue just because
  TTL expired; force refresh remains an administrator operation.
- Movie runtime and series `episode_run_time` come from the source payload.
  Never use an episode file's duration as series runtime or probe physical media.
- Dialog and media detail share `MetadataFacts`, `MetadataOverview`, `MetadataCategories`, `ProviderBadge` and `MediaCredits`. Keep file resolution/size/container/paths and management actions out of discovery. Dialog backdrop spans the panel with a theme-aware downward gradient; do not repeat it as a small image beneath the poster.
- Status returns `{items: [...]}` containing only exact TMDb ID + kind matches with visible media records in enabled, nondeleted libraries. Any episode, including STRM, marks the series; metadata alone does not. Apply current user/profile library and NSFW filters. This is database membership, not a physical-file/playability check.
- Fetch details only when opening a dialog. Batch status requests in groups of at most 100, never one request per card. Abort obsolete requests. Keep status out of shared feed caches and localStorage; account/profile access-key changes remount the catalog.

### Separate local TMDb refresh endpoint

```http
POST /api/discover/tmdb/:kind/:id/refresh
```

- The request has no body, requires both `can_view_discover` and administrator access, and accepts only `kind=movie|tv` with a positive TMDb ID.
- The service resolves the existing same-kind metadata identifier and delegates to `ScraperService.RefreshMetadataTMDb`; it never creates metadata or media rows. The response is the refreshed `DiscoverDetail` with the same safe projection and `Cache-Control: private, no-store`.
- Refresh reads fresh TMDb details, replaces valid metadata, artwork, provider snapshot and loaded credit scopes, and preserves non-empty local fields when TMDb omits them. Stable person identity plus unchanged original role preserves saved Chinese translations; changed or removed credits follow the provider snapshot.
- This administrator endpoint is retained for its existing callers; the Discover
  source dialog exposes no local refresh action and never calls it. Opening a
  dialog requests missing source data only and preserves local data.
- The discover POST skips the upstream refresh when the existing non-degraded TMDb snapshot is less than three hours old, returning local details normally. The persisted `FetchedAt` makes the cooldown shared across browsers and restarts; opening a dialog does not extend it. Missing, degraded or expired snapshots refresh normally. Failed refreshes do not advance the snapshot timestamp. The media detail manual refresh bypasses this cooldown. Source-only previews use read-only GET plus a bounded ensure POST; this never creates local metadata.

### 4. Validation and errors

- Nonpositive ID, unsupported kind, malformed body, empty batch or batch over 100: `400`; request body limit: 16 KiB.
- Anonymous: `401`; missing permission: `403`; detail provider failure: safe `502` without upstream secrets.
- Refresh by a non-administrator: `403`; no matching local metadata: `404`; missing/ambiguous identity or returned-ID mismatch: `400`; provider or persistence failure: safe `502` without credentials or upstream URLs.
- Detail failure retains list summary and offers retry; status failure offers retry without claiming missing titles are definitely absent.

### 5. Cases

- Good: series with only episode 7 STRM is marked; same-number movie is independent.
- Base: missing runtime/credits show unavailable; absent reliable Douban ID means no Douban link.
- Good: an administrator sees translated local data immediately, then the latest TMDb fields, images and credits after refresh; a failed refresh leaves the prior projection intact and offers retry.
- Bad: metadata-only, hidden/disabled library, or scan hint marks a title as owned.

### 6. Tests

- `TestDiscoverLibraryItemsUseVisibleFilesAndExactKind`: exact identity, files, STRM, visibility and limits against PostgreSQL.
- `TestDiscoverDetailFieldsIdentityAndReadOnly`: projection, runtime, identifiers, no writes or raw-data leaks.
- `TestDiscoverDetailUsesMetadataWithoutMedia`: movie/series metadata-only records return translated credits, selected artwork, rating and same-kind binding with zero remote requests, including missing local fields and unconfigured TMDb; the same work still has no ownership badge.
- `TestListMediaCreditsReturnsOrderedCast` and episode/series credit tests preserve the original media endpoint after sharing the projection; `web/scripts/check-series-presentation.mjs` preserves media-only rows, playback order and provider controls.
- `TestDiscoverDetailHTTPPermissionAndValidation`: permission and input boundaries.
- Refresh handler/service tests: administrator gate, exact identity lookup, no metadata creation, valid-field replacement, omitted-field preservation, image/snapshot update, translated-credit preservation and safe failure response.
- `TestDiscoverTMDbRefreshCooldown`: recent snapshots cause zero upstream requests, three-hour-old snapshots refresh, successful repeats reuse the snapshot, and manual metadata refresh bypasses the cooldown.
- `web/scripts/check-discover-detail.mjs`: dialog fields, safe links, no prefetch, retry, account isolation and responsive themes.

### 7. Wrong vs correct

- Wrong: title matching or checking all episodes, or putting `in_library` into globally shared feed data.
- Correct: exact provider ID + movie/series kind, visible media existence, and request-scoped status rendered separately from shared discovery items.
- Wrong: make a detail GET mutate local metadata or replace source facts with
  local translations. Correct: source GET plus bounded missing-source ensure;
  local membership and maintenance remain separate.


## Scenario: Catalog Ranking Lists

### 1. Scope / Trigger
HongGuo and HuangGuo AI ranking presentation in their discovery pages.

### 2. Signatures
`hongguoAPI.list(keyword, sourceCategory, category, rank, page, signal?, pageSize = 50)`;
`huangguoaiAPI.list({ keyword, category, tag, rank, page, page_size? }, signal?)`.
The existing works endpoints accept `page_size=10`; omitted client size stays 50.

### 3. Contracts
Ranking mode without a keyword uses shared `CatalogRankingHeader/Row`, with
10-item incremental loading and continuous ordinal `index + 1`. Start the local
cursor at one, canonicalize legacy URL pages to one, and keep the cursor out of
the URL. Preserve upstream order and deduplicate appended rows by source ID.
The bottom observer loads one batch at a time and pauses while details are open.
Rank changes and refresh reset rows and abort obsolete reads; detail ID changes
preserve rows. Hover and keyboard focus reveal poster/synopsis using CSS
transitions; reduced motion disables meaningful animation. Missing artwork or
synopsis uses a placeholder. Header mount scrolls to its start on rank changes.
Below desktop widths, copy and rank buttons occupy separate rows. Category/search
grids keep 50-item incremental loading and existing source/search semantics.
HongGuo ranking has no multiselect; category/search administrator selection remains.

### 4. Validation & Error Matrix
Failed continuation preserves loaded rows and retries the same batch. Partial or
empty final batches stop continuation. Switching ranks resets to batch one;
obsolete responses cannot append to the new rank. Verify settled responsive
layout after resize animations; reduced-motion duration is effectively zero.

### 5. Good / Base / Bad Cases
Good: 43 results append as 10/10/10/10/3, ranked 01–43.
Base: fewer than 10 results need no continuation. Bad: changing global API size
causes category/search feeds to skip results.

### 6. Tests Required
Run `node scripts/check-catalog-rankings.mjs` against local Vite (`DISCOVER_TEST_URL`).
Synthetic data covers scroll loading, retry/cancellation, continuous ordinals,
hover/focus, reduced motion, detail preservation, absent ranking selection,
artwork failure, category regression and responsive themes. Also run HongGuo
discovery/search/batch and HuangGuo AI checks, lint and build.

### 7. Wrong vs Correct
Wrong: change every list request to 10 and retain a 50-item has-more calculation.
Correct: only rankings pass 10; category/search keep 50 and continuation uses the
actual requested size.


## Scenario: HuangGuo AI Unified Search

### 1. Scope / Trigger
HuangGuo AI discovery keyword search and incremental loading.

### 2. Signatures
Use existing `huangguoaiAPI.list({keyword, category: '', tag: '', rank: '', page, page_size: 50}, signal)`
and `huangguoaiAPI.search(keyword, page, signal)`; upstream pages retain their source size.

### 3. Contracts
One search form queries both sources; remove search-scope selection and manual
tag input. Normalize legacy mode/tag away and start both cursors at one for a keyword.
Maintain independent rows, cursors, loading, has-more, error and retry state.
Merge by source ID in official-first order, preserving hydrated metadata and OR'ing
downloaded flags. Later official pages continue independently from 50-item local
pages; never equate deduplicated displayed count with the upstream total.
Details keep lists mounted. Query/access changes abort both sources. Explicit
repeat-search/refresh clears both sources and resets both cursors to one.

### 4. Validation & Error Matrix
A failed source preserves the successful source and prior results. Retry only its
failed page without advancing it or refetching the other source. Pause automatic
continuation for the failed source until retried; successful sources can continue. An exhausted source stays exhausted while
the other continues. Stale success/failure/completion cannot mutate a newer query.

### 5. Good / Base / Bad Cases
Good: 24 official hits overlapping two of 50 local hits display 72 deduplicated
works and both retain their pagination. Base: empty official results still display
local matches. Bad: a scope selector is hidden but search still queries only local,
or retrying official page two advances local page two to three.

### 6. Tests Required
Run `node scripts/check-huangguoai-search.mjs` from `web` against local Vite on 4179
(or `HUANGGUOAI_TEST_URL`) for overlap/metadata precedence/download flags,
independent paging/failure/retry, explicit refresh, obsolete response/empty results,
legacy URL cleanup and dark/light responsive layout. Run existing HuangGuo AI and
catalog ranking checks, lint and build. Account isolation also follows the access-key
remount and AbortController contract; do not claim account-switch browser coverage
from keyword-switch tests alone.

### 7. Wrong vs Correct
Wrong: a shared page or retry counter refetches both sources and skips a failed page.
Correct: advance and retry the local/official cursors independently, then merge.

## Scenario: Source data in Discover

### 1. Scope / Trigger
TMDb/Douban saved catalog browsing and source-scoped rankings inside Discover.

### 2. Signatures
- TMDb root GETs: `/api/catalogs/tmdb/subjects?kind=movie|tv&page=1` and
  `/api/catalogs/tmdb/people?page=1`; existing UUID detail/children/credits/works GETs.
- Douban GETs: `/api/catalogs/douban/subjects?keyword=&type=movie|series&page=1&page_size=30`
  and `/api/catalogs/douban/subjects/:id`; poster and `credits/:index/image` support GET/HEAD.
- Administrator reads/writes remain under `/api/admin/tmdb` and `/api/admin/douban`.

### 3. Contracts
- `/discover?system=tmdb|douban` defaults to saved data. TMDb lists movie/tv/person,
  with keyword/kind/page and administrator-only quality/state filters in the URL;
  season/episode navigation stays in one ModalShell. Subject/person UUID selections
  normalize before detail requests. GETs read persisted data; explicit
  search/import/ensure remain POSTs.
- Root lists and child pages contain 30 records. Person works paginate 60 distinct
  works before loading their roles; all roles of one work appear on one card.
  Type/cast/crew/year ordering is database-only and never refetches filmography.
- Users with `can_view_discover` can read saved catalogs and images and request
  missing-source ensure. Opening a complete saved detail does not enqueue work;
  viewer browsing never issues force refresh/import or image force-retry requests.
  Raw JSON and refresh diagnostics/controls are administrator-only. Public
  projections and image URLs do not reveal storage keys, request queries or
  credentials. Session remount/abort guards stale responses;
  Shared ModalShell contains Tab/focus in the highest z-index dialog, handles
  top-dialog Escape, locks scrolling and restores the initiating control on
  close. Capture that control before child autofocus/StrictMode effects; retain
  it when async source loading replaces one modal shell with another.
- Poster grids share `CatalogPosterCard`, with 320px lazy images, saved-data
  ratings and circular person portraits; detail images use 600px. Ordinary image
  GETs retain existing on-demand local download/thumbnail generation, independently
  of source data refresh. Mutable image URLs remain private/no-cache.
- Rankings save source summaries without per-item detail requests. Douban
  rankings use exact `{douban_id,kind}` ensure, then reuse `DoubanSubjectDetails`.
  Missing base/credits run as a durable API-only supplement task; no webpage or
  recursive personnel requests. Ensure failure retains the summary and offers
  retry. Pending detail reads stop after 45 two-second attempts and offer reread.
- Source refresh never edits local metadata, roles, translations or selections.
  Source image retry is separate; ordinary page reads do not start all-season,
  all-episode, all-person or full-filmography recursion.

### 4. Validation & Error Matrix
Anonymous reads -> 401; missing Discover permission -> 403; viewer administrator
reads/writes -> 403. Malformed UUID/filter/page -> 400 server-side. Public reads
are private/no-store and never return raw JSON or attempt/retry timestamps.
Missing saved Douban ranking details retain the summary while a controlled POST
registers supplementation. Ensure cannot force complete records to refresh or
alter local work fields. Invalid/duplicate URL
values converge with replace without rendering an invalid-ID dialog.

### 5. Good / Base / Bad Cases
Good: a viewer browses saved series, opens one season and its episode, then
reads an actor's saved works without starting a refresh. Base: missing artwork
shows a placeholder. Bad: every tab mounts at once or raw diagnostics are hidden
only in the UI while public responses contain them.

### 6. Tests Required
`TestDiscoverCatalogReadPermissionsAndProjection` uses isolated PostgreSQL for
401/403/200, GET/HEAD images, public/admin projections and zero queued refreshes.
Run `check-discover-centers.mjs`, `check-douban-data-center.mjs`,
`check-discover-detail.mjs`, `check-discover-boundaries.mjs`, HongGuo/HuangGuo
checks, lint and build from `web`.
Mocked browser tests cover source scoping, hierarchy, ordinary/admin controls,
rank history, duplicate/invalid URLs, cancellation, auth switching and dual themes.
Wait for resize and scheduled focus restoration before layout/focus assertions.

### 7. Wrong vs Correct
Wrong: move an admin-only page into viewer navigation but retain admin GETs for
ordinary users. Correct: permission-guarded public projections plus separate
administrator writes, verified at both route and browser boundaries.

## Scenario: Unified categories, rankings and TMDb detail

### 1. Scope / Trigger
Discover source browsing shares visual controls while preserving each source's
search, download, selection and pagination lifecycle.

### 2. Signatures
- `GET /api/catalogs/tmdb/categories?kind=movie|tv|person` returns
  `{data:[{value,label}]}`; `series` is accepted server-side as the TV alias.
- TMDb subjects accept `genre` (positive signed-32-bit source genre ID); people
  accept `department` (exact primary department, at most 100 characters).
- `GET /api/catalogs/douban/categories?type=movie|series` has the same response;
  subject lists accept `genre` (exact normalized name, at most 100 characters).
- Administrator equivalents use the existing `/api/admin/{source}` roots.

### 3. Contracts
Category options/filtering read persisted JSON/department values without calling
providers. Multi-genre membership uses EXISTS/JSON containment before count and
pagination, never a join that duplicates subjects. Missing genres remain in All.
Douban recognizes subject/data wrappers, genres/genre arrays of strings or
name/title objects, and comma/slash-delimited strings; the first nonempty candidate
wins. TMDb department is one primary occupation, with translated labels but raw
values in requests. Type changes clear incompatible filters and reset page one.

Source navigation says 分类/榜单; Bangumi has only 榜单. Poster grids share column
breakpoints; ranked feeds use CatalogRankingHeader/Row and source order positions.
Search results retain a grid. Keep source cursor sizes and retry behavior; unknown
ranking totals must not be presented as a fabricated total or fixed page size.

TMDb rankings resolve center_id then open the same TMDbDetailModal as categories.
Person navigation uses person_id, never profile.id. One shell owns history, parent
links and focus containment. Source facts/score and circular clickable credits
are shared; seasons use posters, episodes landscape rows and person works posters.
Admin force operations are folded into the management menu, raw diagnostics into
details. Viewing a saved person's works is a read; refresh is a separate action.
Selected missing detail may request ensure through the permission-guarded catalog
route; do not recursively hydrate all related entities. Pending reads stop after
45 attempts per read cycle and expose manual reread. Child/credit failures expose
same-page retry without discarding the loaded main detail.

### 4. Validation & Error Matrix
Unauthenticated/unauthorized categories return 401/403; invalid kind/type, invalid
integer genre, and overlong text filters return 400. Public reads remain private,
no-store. Failed option reads retain search and expose retry. Failed identity
resolution retains the ranking summary with retry; admin operations remain absent
for viewers.

### 5. Good / Base / Bad Cases
Good: one film appears in both drama and comedy, once per filtered page. Base:
summary without genre appears under All. Bad: changing a genre requests TMDb,
adds duplicate cards, or uses a person's profile UUID for navigation.

### 6. Tests Required
Isolated PostgreSQL: TestCatalogCategoriesFilterBeforePagination,
TestDoubanCategoriesMatchPayloadFormats, TestDiscoverCatalogReadPermissionsAndProjection.
Browser: check-discover-centers (category parameters, hierarchy, permissions),
check-discover-detail (shared source detail, people/works, bounded polling,
related-data retry, themes/widths), check-discover-boundaries, Douban and both
HongGuo source checks. Wait for layout settling before pointer/focus assertions;
URL change alone does not prove a lazy source page has finished replacing its
predecessor.

### 7. Wrong vs Correct
Wrong: render a separate TMDb ranking detail or change source query lifecycles to
match a visual control. Correct: resolve the center identity and reuse the modal;
share presentation while preserving source-owned searches/cursors.


## Alignment closure: shared controls and failure recovery

- CatalogSearchForm, CatalogPager, CatalogRankingSkeleton and CatalogLoadMore
  share presentation only. Keep each source's own query, refresh and cursor
  semantics, including independent local/official search retries.
- Ranking initial loading uses horizontal row skeletons. Successful partial
  feeds offer manual continuation alongside the observer; exhausted feeds show
  an end label. A failed continuation must never imply the feed is complete.
- TMDb related reads retain the previous successful page while loading or after
  failure. Render the response page number, disable paging during loading and
  retry the requested failed page. Ranking title, poster and summary survive
  center-ID resolution followed by a failed first detail read.
- DoubanSubjectDetails owns force-refresh state and cancellation. Category and
  ranking entry points share the same administrator-only management menu;
  category callers may request list reread after a mutation. Viewers receive no
  management controls and the refresh handler also guards administrator role.
- End labels may use role=status. Browser tests must distinguish loading text
  from completion announcements rather than treating every status as pending.
  Wait for enabled controls and settled layout before pointer pagination checks.


## Source credit rails

### 1. Scope / Trigger
Use for source-detail cast/crew presentation and TMDb subject-credit reads.

### 2. Signatures
`GET /api/{catalogs|admin}/tmdb/subjects/:id/credits?category=cast|crew&page=1&page_size=20`
returns `{data,total,page,page_size}`. Omitted page_size remains 60; person works
continue using their separate work-level 60-record contract.

### 3. Contracts
- Source subject details present separate cast/crew horizontal rails with circular
  profiles. TMDb requests category=cast|crew with page_size=20 independently;
  subject credits validate 1..60, filter/count before pagination, preserve cast
  source order and put principal crew before department/job groups. Existing
  stored records need no refresh or migration for this query correction.
- Person works remain work-level pages of 60. Season/episode selectors and local
  library credit presentation retain their previous contracts.
- Douban/HongGuo split already-returned arrays into display batches of 20. Preserve
  each group's input order. Do not invent credit sources for Bangumi/HuangGuo AI.
- Load near the horizontal right edge, append without removing previous cards,
  and retain an accessible manual continuation/retry action. Hide empty successful
  groups. Progressive scroll containers remain keyboard-focusable after the last
  page. Show group total, not only the number of rendered cards.
- Requests have per-group synchronous in-flight guards, successful-page cursors
  and cancellation. Release the guard only after result/error commit: a settled
  counter with useLayoutEffect covers immediately resolving/rejecting requests
  whose loading=true/false updates collapse into one React batch. Failed scrolling
  cannot automatically retry; manual retry requests the same page.
- The TMDb header uses the modal surface without another bordered background card.
  Region/language labels reuse ISO-based Intl.DisplayNames Chinese display labels;
  known jobs use explicit Chinese labels. Unknown names/roles/jobs retain source
  text. Never overwrite source facts with guessed translations.
### 4. Validation & Error Matrix
Invalid UUID/page/category or subject page_size outside 1..60 returns 400.
An exhausted page returns an empty data array with the filtered total. Failed
continuation retains prior cards and retries the same page. Aborted requests
cannot write rows, error state or release a newer request's lock.

### 5. Good / Base / Bad Cases
Good: cast pages 1→2(fails)→2(success) append without moving crew's cursor.
Base: fewer than 20 saved credits produce one rail without pagination controls.
Bad: sorting a mixed 60-record result in the browser, leaving cast on later pages.

### 6. Tests Required
- TestTMDbCreditCategoryPagination (real isolated PostgreSQL),
  check-catalog-credit-rails, check-discover-detail, check-discover-centers,
  check-douban-data-center and check-hongguo-discover; dual-theme mobile/desktop
  screenshots. Scrolling fixtures must return booleans from wait predicates and
  wait for settled layout before pointer clicks, without relaxing cursor assertions.

### 7. Wrong vs Correct
Wrong: clear the in-flight ref immediately in finally before the error is rendered.
Correct: increment a settlement state counter in finally and release the ref in
useLayoutEffect after commit; loading=true→false may otherwise batch to no change.


## Saved source catalog continuation

- TMDb Movie/TV/Person and Douban Movie/Series category grids use
  `useCatalogContinuation`, 30 records per request. URL owns filters and detail
  selection; remove `page` from the URL. Successful pages advance the cursor and
  append by row identity; failures preserve rows, pause observers and retry the
  same page. Filter/session changes cancel old reads; refresh restarts page one.
- Detail open/close preserves list nodes and pauses automatic continuation.
  CatalogLoadMore retains a keyboard-accessible fallback. Mode tabs use a compact
  `w-fit` container, sharing HongGuo tab primitives without stretching the bar.
- TMDb ownership reads are bounded to the incoming batch. An ownership failure
  does not hide catalog data. No query-sized growing ownership requests.
- Verify `check-catalog-continuation.mjs`, `check-discover-centers.mjs`, and
  `check-douban-data-center.mjs`, including literal search, retry, filter abort,
  people, detail preservation and responsive themes.


## Scenario: Ranking refresh and success time

### 1. Scope / Trigger
Shared ranking headers, source scheduling and persistent discovery snapshots.

### 2. Signatures
- `DiscoverService.RankingPage(ctx,key,page,refresh,load)` returns items, success time and an optional error with the old items.
- `discover_ranking_pages(key,page,payload,fetched_at)` persists TMDb/Douban pages.
- Feed `_meta[key].updated_at` is emitted for page one only.
- `POST /catalogs/hongguo|huangguoai/ranks/:rank/refresh` returns `{updated_at}`; source list responses carry the same field.
- `tmdb_data_rank` / `douban_data_rank`: daily by default, settings `metadata.<kind>_enabled/_interval_seconds`, configurable through existing task definitions.

### 3. Contracts
- Manual refresh reloads the selected first page; periodic jobs visit each source's first pages. Continuations remain user-driven. No fabricated top-100 cap or full-catalog traversal.
- Preserve source summaries and existing quality precedence. TMDb query persistence already saves quality-1 subjects; Douban saves partial summaries. Existing periodic maintenance selects these records, without explicit supplement jobs or local media creation.
- Fresh snapshot and timestamp are saved together; microsecond timestamps round-trip through PostgreSQL. Successful first-page replacement invalidates older continuation snapshots. Persistent absence must not resurrect a deleted page from process memory.
- Same-source work is serial with cancellable admission; different sources retain existing bounded concurrency. Failed/empty first pages retain prior data/time; fallback results never count as success.
- HongGuo/HuangGuo manual selection reuses source Run mutual exclusion, shutdown and task history. Timestamps come from committed rank entries. Their existing discovery behavior remains intact.
- Bangumi reads its own persistent weekly-calendar success time, waits for explicit refresh and marks failed/stale results honestly; it does not create a duplicate discovery snapshot.
- Refresh endpoints require discovery permission; HuangGuo retains adult/profile visibility. Task configuration remains administrator-owned.
- The shared header displays refresh state and local formatted success time, or no successful update. Disable duplicate refresh; abort obsolete requests, pause continuation while refreshing and keep rows when the subsequent list read fails. A successful POST may advance time even if rereading the list fails, with an explicit read error.

### 4. Validation & Error Matrix
- Invalid source rank -> 400; active source rank task / disabled source -> 409.
- Provider or persistence failure -> old data/time plus visible warning/error, never a false new success.
- Partial scheduled failure -> failed execution with per-rank metrics, retaining successful ranks.
- Continuation/end-of-list -> does not advance displayed first-page timestamp.

### 5. Good/Base/Bad Cases
Good: scheduled ranking writes summary placeholders, then the enabled source maintenance task fills missing basics on its own cadence.
Base: repeating a ranking reuses source identities and does not downgrade complete details.
Bad: treating every existing placeholder as complete, or immediately enqueueing details/images from each ranking response.

### 6. Tests Required
`TestDiscoverRankingPersistenceFailureAndPagination`, `TestDiscoverRankingCanceledLockWait`, `TestDiscoverRankTasksKeepSummariesForPeriodicMaintenance`, `TestDoubanRankSummariesArePeriodicCandidates`, `TestSchedulerRegistersSourceRankings`, source manual-rank tests and `TestDiscoverBangumiManualRefreshKeepsSourceTimestamp` cover persistence, cancellation, candidate selection and source time.
`web/scripts/check-ranking-refresh.mjs` covers all four source UIs, stale URLs, failure/success, repeated clicks, canceled navigation, post-refresh list failure and responsive themes. `check-catalog-rankings.mjs` retains pagination and category regression coverage.

### 7. Wrong vs Correct
Wrong: label cache-read time as a successful ranking refresh, or discard displayed rows immediately after a successful refresh POST.
Correct: display persisted source success time and replace rows only after their reread succeeds.
