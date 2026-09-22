# Routing and Loading Contracts

## Route, Access, and Navigation Ownership

`web/src/appRoutes.tsx` is the single manifest for application route IDs,
paths, access metadata, and navigation metadata. Consumers such as
`layoutNavigation.ts` project destination leaves from that manifest and combine
them with separately defined non-route groups.

Required behavior:

- Declare `permission` and `adminOnly` on the route manifest entry that owns
  the page. Do not repeat those values in a separate navigation list.
- Apply manifest access metadata to direct route rendering as well as visible
  navigation. Hiding a link is not an access control.
- Model top-level layout spaces separately from destination routes. A space is
  an expandable button without a route or `aria-current`; only a visible leaf
  route is a navigation link and may receive `aria-current="page"`.
- `AppRouteNavigation.scope` is `viewer | files | management | header`. Render the first three
  spaces in that order as `观看空间`, `文件空间`, and `管理空间`; `files` and
  `management` are administrator-only layout groups. Render `header` leaves as
  global header actions using their inherited access metadata.
- Project leaves for each scope directly from the manifest after applying
  inherited access metadata. Do not add a second-level `group` field or a
  separately maintained leaf list.
- Determine a space's active and auto-open state by testing each leaf's
  `activePaths` together with its `end` flag. Do not flatten paths into a
  prefix-only group matcher: `/admin/storage` belongs to Management Space,
  while `/admin/storage/duplicates` and `/admin/storage/recycle` belong to File
  Space.
- Match active paths on segment boundaries: a path matches itself or descendants
  below `path/`, never an unrelated path with the same string prefix.
- Keep canonical internal links on the maintained viewer or `/admin/*` path.
  Legacy root paths exist only as guarded `Navigate replace` redirects.
- Redirect `/admin` and non-content management group roots with `replace` to a
  deterministic retained leaf; redirect targets must not form cycles.
- Keep ordinary `/search` authenticated but independent of AI permissions.
  Apply AI capability and provider checks only to `mode=ai` behavior.
- System settings expose one management link to `/admin/settings` (redirects
  to `general`). Existing `/admin/settings/general|playback|recognition-words|access`
  paths remain valid; `watching` owns playback behavior. These paths render
  page-local category links and share the `/admin/settings` layout mount key
  so category changes preserve unsaved settings. Keep the recognition panel
  mounted after its first visit, hiding it while another category is active.
  Run `node scripts/check-settings-tabs.mjs` against the Web preview to cover
  drafts, save payloads, readback, permissions, navigation and responsive themes.

## Permission Lifecycle

The permission store caches a result only for the authenticated user ID that
requested it.

- Clear permissions and readiness when the authenticated user changes or logs
  out.
- Ignore an in-flight response when its requested user ID is no longer current.
- Treat load failure as retryable state, not as permission denial.
- A guard may decide only after permissions for the current user are ready.

This contract prevents an administrator-to-viewer account switch from briefly
reusing administrator navigation or route access.

## Canonical URL State

Viewer composition state is URL owned:

- Libraries: `view=library|poster`, default `library`.
- Library detail filters: `missing_poster=1` and `missing_chinese_title=1`; an
  absent flag means the filter is disabled. Media detail links opened from
  this list carry the complete library URL as router state so “返回媒体库”
  restores these filters.
- Me: `tab=favourites|playlists|history`, default `favourites`.
- Discover: `system=catalog|hongguo`, default `catalog`; mount only the selected
  catalog and rebuild it on user/profile changes. `/hongguo` is a guarded
  replace redirect to `/discover`, preserving query parameters and setting
  `system=hongguo`. Both routes require `can_view_discover`.
- Search: `mode=ai` when both permission and provider state allow it; otherwise
  ordinary search.

Missing values render the documented default. Duplicate or invalid values use
replace navigation to the canonical URL so refresh, deep links, and browser
history converge on one state.

## Bounded Poster Loading

The poster view mounts its loading effect only while
`/libraries?view=poster` is active.

- Request 50 items per library page with version grouping disabled.
- Schedule at most three library-page requests in one explicit batch.
- Merge successful results in deterministic scheduled order and de-duplicate
  media IDs before series grouping.
- Advance a library page only after that page succeeds. A failed page remains
  retryable from the next explicit load action.
- Continue only after the user activates the load-more control; stop when every
  library is exhausted.

Do not restore the former unbounded or multi-thousand-item initial request
under another component or helper name.

## Scenario: Bounded Search Loading

### 1. Scope / Trigger

Changes to the search page, top-bar suggestions, or their request lifecycle.

### 2. Signatures

`mediaAPI.searchPage(q, page, 30, { signal })` calls `GET /media` with
`q`, `page`, `page_size`; `search(q, limit, signal)` and
`aiAPI.smartSearch(query, signal)` also accept cancellation.

### 3. Contracts

Load only page 1 initially. Append later pages in backend rank order only after
an explicit load-more action; advance the page only after success. Display the
backend total separately from loaded items. AI search remains a single request.
URL changes and unmount abort requests and invalidate their sequence; suggestions
also abort on blur/query/mode changes. Cancellation is not a visible failure.

### 4. Validation & Error Matrix

- Page failure: keep loaded cards and retry the same page.
- Query/mode change: clear old results, reset page and cancel pending work.
- Stale response: ignore success, failure and completion state updates.
- Last page: hide load-more; do not automatically request another page.
- Short/empty page with later candidates in `total`: keep load-more available;
  do not mistake a missing representative for exhaustion of all later pages.

### 5. Good / Base / Bad Cases

Good: 65 matches load as 30, 30, 5; base: 14 matches need one request;
bad: request 2000 or loop until the entire result set is loaded on mount.

### 6. Tests Required

Run `node scripts/check-search-loading.mjs http://127.0.0.1:6237` against an
isolated Vite dev server. It mocks all APIs and checks StrictMode, first-page
size, retry, pagination completion, page/AI/suggestion cancellation, unmount,
stale responses and dark/light responsive widths. Close the server afterward.

### 7. Wrong vs Correct

Wrong: increment the page before awaiting its response, or only hide a stale
response without cancelling it. Correct: advance on success and combine
AbortController with a sequence check.

## Verification

Run `node scripts/check-hongguo-discover.mjs` against a local Web preview on
port 4179 (or `DISCOVER_TEST_URL`) for legacy query preservation, lazy catalog
switching, discovery permission, image-failure fallback and responsive layouts.
It uses mocked APIs; actual downloaded poster rendering remains deployment QA.

Series-library deep links (`series` or `series_id`) must skip the library
catalogue page. After library type resolution, request the linked series card
and its episodes independently using the URL identity. Card completion must
not refetch episodes; version changes reuse them, while season changes fetch
only the selected season. Explicit refresh reloads both, stale responses are
ignored, and returning to the catalogue restores 50-item pagination. Run
`node scripts/check-series-loading.mjs` from `web` to verify this lifecycle.

## Scenario: Season-Scoped Web Series Detail

### 1. Scope / Trigger

Opening a series deep link with `season` or switching the selected season.

### 2. Signatures

`GET /api/libraries/:id/series/episodes?key=metadata:<id>&season=<non-negative integer>`;
`season` is optional. A targeted series card may include `seasons: number[]`.
Season-scoped episode responses add `resume: HistoryItem | null`, where a
candidate has `media` (one playable file) and `is_next` for the following episode.

### 3. Contracts

When `season` is present, load only its visible file versions and history;
the card's `seasons` supplies the selector without loading other episodes.
The header's play control uses the separate user- and library-scoped whole-series
`resume` candidate: after the last completed S1 episode, it targets S2 E1,
while an explicit `season=1` URL still shows S1 in the episode selector.
Without `season`, retain the previous full-series response. NFO and ordinary
series follow the same season filter and visibility rules. Whole-series admin
actions explicitly fetch without `season` and never fall back to the current
season on failure. No external font request is needed for first paint.

### 4. Validation & Error Matrix

Invalid or negative `season` returns HTTP 400; an unavailable season yields
an empty list. No eligible continuation returns `resume: null`; a continuation
lookup failure returns an error, never another user's candidate. Failed
whole-series fetch reports an error and does not delete the current-season
subset or dismiss the selected series.

### 5. Good / Base / Bad Cases

Good: a `season=1` deep link loads season 1 plus the list of visible seasons,
but after S1 is watched its header plays S2 E1 without fetching S2's file list.
Base: an old URL without a season still loads the full series. Bad: a season
switch or admin delete silently reuses the old/current-season episode list.

### 6. Tests Required

`check-series-loading.mjs` checks per-season requests, the separate next candidate,
stale responses and retry. `TestContinuationCrossSeason` checks scoped NFO/ordinary
S1→S2 candidates; the handler and NFO/ordinary service tests check season lists
and filtered episodes. Run
`npm run lint`, `npm run build`, and `git diff --check` for Web changes.

### 7. Wrong vs Correct

Wrong: derive every season or the whole-series next candidate from the
current-season episodes, or delete those episodes as the entire series.
Correct: return season numbers and the one whole-series continuation separately;
fetch every visible episode only when a whole-series operation is confirmed.

For routing or layout changes, verify at least 390x844, 768x1024, 1440x900,
and the exact responsive breakpoint affected by the change. Check canonical
URLs, active navigation, keyboard focus return, control target sizes, document
scroll width, and runtime console errors.
