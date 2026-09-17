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
- Douban cache entries, cloned responses, and preheat inputs keep the same official large poster URL. The image proxy applies the current configured CDN only during an image-cache miss, so changing that configuration never mutates feed URLs or requires section-cache invalidation.
- The Web page groups sections by page for initial load and refresh. A row page change requests only that section and target page.
- Starting a new feed batch aborts the previous controller. Aborted batches must not update rows, errors, loading state, or localStorage cache.
- Discover poster URLs stay stable across mounts and manual data refresh. Use native `loading="lazy"`; only a failed individual image retry may add `v=r1` through `v=r3` and `refresh=1`.

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
| Provider succeeds with empty items | Return empty items and do not cache them. |
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

- HongGuo discovery search composes two existing requests: official `search(keyword)` once per explicit search, and local `list(keyword,'','','',page)` with page size 50, starting at page 1 regardless of legacy URL page. Scroll and local retry never repeat the official request. Separate controllers and retry counters retain successful results when either source fails; failed local pages cannot advance until retried successfully. Disconnect the observer before incrementing, and pause it during loading/errors/detail dialogs.
- Merge by source ID with official first-seen order and hydrated metadata precedence; do not downgrade a canonical work to a pending summary. Show the number of deduplicated displayed cards, explicitly label official first-screen plus collected local data, and never imply the count covers all upstream hits. Local queries include title substrings or exact source IDs and pending summaries, independent of category/rank when keyword search is active. Category saves update both displayed sources.
- Appending preserves selection and does not remount the feed. Explicit repeat-search/refresh restarts search-local page 1 and both sources; category/rank refresh retains its existing URL-page semantics. Query/account remount aborts obsolete reads. Local pagination remains a live catalog view, not a frozen snapshot; catalog mutations can change membership/order between pages. No official pagination or additional import/refresh task is fabricated by local pagination.
- `web/scripts/check-hongguo-search.mjs` verifies source overlap, hydrated precedence in both directions, >50 matches, independent errors/retries, no skipped failed page, no repeated official search on scroll, selection preservation, empty sources, explicit refresh, stale-response exclusion and responsive source notices.

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
- Only absent Metadata identities are registered in the existing durable catalog
  hydration queue. Creation/details/artwork happen in its background worker;
  repeated searches preserve job identity and retry state. Do not overwrite
  existing metadata with summaries or create Media records. Queue failures are
  errors, never silently successful search-and-import responses.
- URL-owned `tmdb_query` and `tmdb_kind` are independent from HongGuo query keys.
  Abort old requests on search change/unmount; retain accumulated rows on page
  failure and retry that page. Reuse `ContentRow` badges and `DiscoverDetailModal`.
- Tests: `TestDiscoverSearchTMDbPaginationAndIdentity`,
  `TestDiscoverSearchQueueSkipsExistingAndDeduplicates`,
  `TestDiscoverSearchRejectsInvalidInput`, and discovery permission tests.

### 1. Scope

TMDb discovery previews remain dialogs without season/episode lists. The detail GET is a safe read-only projection; an administrator may explicitly follow it with the separate refresh POST below. Opening the dialog must not create metadata, snapshots or media records.

### 2. Signatures

- `GET /api/discover/tmdb/:kind/:id` (`kind=movie|tv`).
- `POST /api/discover/library-status`, body `{ "items": [{ "tmdb_id": 123, "media_type": "tv" }] }`.

### 3. Contracts

- Both routes require `can_view_discover`; successful responses use `Cache-Control: private, no-store`.
- Detail returns public Match fields, positive deduplicated `runtime_minutes`, and `credits`; no raw provider payload or season/episode fetches. Only a unique, same-kind stored Douban identifier supplies the Douban link.
- Detail resolves local metadata by exact TMDb ID + work kind, independently of media existence and library visibility. A local hit uses canonical work presentation, selected artwork and `MediaService.ListMetadataCredits` (saved translated `Person.Name` / `MetadataCredit.Role`, actor-first order). Ordinary users remain read-only and use that local projection; only absence of matching metadata falls back to TMDb. An administrator opening a local hit may then start the separate asynchronous refresh endpoint below; the dialog keeps the local projection visible while refreshing and retains it on failure. The discovery permission still applies; no media paths or playback targets are exposed. Library/NSFW media visibility filters belong to the ownership badge, not this shared catalog preview.
- Local movie runtime uses canonical `RuntimeSec` when positive, otherwise the stored TMDb snapshot; series runtime uses only snapshot `episode_run_time`. Never label an arbitrary episode file's duration as whole-series runtime. Remote preview uses the same snapshot runtime parser. No physical media probing is triggered.
- Dialog and media detail share `MetadataFacts`, `MetadataOverview`, `MetadataCategories`, `ProviderBadge` and `MediaCredits`. Keep file resolution/size/container/paths and management actions out of discovery. Dialog backdrop spans the panel with a theme-aware downward gradient; do not repeat it as a small image beneath the poster.
- Status returns `{items: [...]}` containing only exact TMDb ID + kind matches with visible media records in enabled, nondeleted libraries. Any episode, including STRM, marks the series; metadata alone does not. Apply current user/profile library and NSFW filters. This is database membership, not a physical-file/playability check.
- Fetch details only when opening a dialog. Batch status requests in groups of at most 100, never one request per card. Abort obsolete requests. Keep status out of shared feed caches and localStorage; account/profile access-key changes remount the catalog.

### TMDb refresh after local detail load

```http
POST /api/discover/tmdb/:kind/:id/refresh
```

- The request has no body, requires both `can_view_discover` and administrator access, and accepts only `kind=movie|tv` with a positive TMDb ID.
- The service resolves the existing same-kind metadata identifier and delegates to `ScraperService.RefreshMetadataTMDb`; it never creates metadata or media rows. The response is the refreshed `DiscoverDetail` with the same safe projection and `Cache-Control: private, no-store`.
- Refresh reads fresh TMDb details, replaces valid metadata, artwork, provider snapshot and loaded credit scopes, and preserves non-empty local fields when TMDb omits them. Stable person identity plus unchanged original role preserves saved Chinese translations; changed or removed credits follow the provider snapshot.
- The dialog starts this request only after an administrator receives `local_metadata=true` from the detail request. The initial detail remains visible, stale requests are aborted, and a failed refresh exposes retry while retaining the current data.
- The discover POST skips the upstream refresh when the existing non-degraded TMDb snapshot is less than three hours old, returning local details normally. The persisted `FetchedAt` makes the cooldown shared across browsers and restarts; opening a dialog does not extend it. Missing, degraded or expired snapshots refresh normally. Failed refreshes do not advance the snapshot timestamp. The media detail manual refresh bypasses this cooldown. Remote-only previews without local metadata retain their existing read-only behavior.

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
- Wrong: make the detail GET mutate shared metadata for every viewer, or hide the local projection until refresh completes.
- Correct: GET is a safe projection; administrator-only POST reuses the canonical metadata refresh path after the local projection is shown, with abort/retry and old-data retention on failure.
- Wrong: reuse the cast layout but read `OriginalRole` from TMDb for an already-owned title. Correct: reuse both the local data projection and the display components; verify translated fields, units and source precedence together.
