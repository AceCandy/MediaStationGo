# Routing and Loading Contracts

## Route, Access, and Navigation Ownership

`web/src/appRoutes.tsx` is the single manifest for application route IDs,
paths, access metadata, and navigation metadata. Consumers such as
`layoutNavigation.ts` derive navigation groups from that manifest.

Required behavior:

- Declare `permission` and `adminOnly` on the route manifest entry that owns
  the page. Do not repeat those values in a separate navigation list.
- Apply manifest access metadata to direct route rendering as well as visible
  navigation. Hiding a link is not an access control.
- Keep canonical internal links on the maintained viewer or `/admin/*` path.
  Legacy root paths exist only as guarded `Navigate replace` redirects.
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

For routing or layout changes, verify at least 390x844, 768x1024, 1440x900,
and the exact responsive breakpoint affected by the change. Check canonical
URLs, active navigation, keyboard focus return, control target sizes, document
scroll width, and runtime console errors.
