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

## Verification

Series-library deep links (`series` or `series_id`) must skip the library
catalogue page. After library type resolution, request the linked series card
and its episodes independently using the URL identity. Card completion must
not refetch episodes; season/version changes reuse them. Explicit refresh
reloads both, stale responses are ignored, and returning to the catalogue
restores 50-item pagination. Run `node scripts/check-series-loading.mjs` from
`web` to verify this request lifecycle.

For routing or layout changes, verify at least 390x844, 768x1024, 1440x900,
and the exact responsive breakpoint affected by the change. Check canonical
URLs, active navigation, keyboard focus return, control target sizes, document
scroll width, and runtime console errors.
