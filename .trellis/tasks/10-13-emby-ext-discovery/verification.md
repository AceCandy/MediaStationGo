# Verification

Implemented MSG only. LinPlayer working tree was not changed in this task.
No commit, archive, deployment or player package was produced.

## Passed

- Dedicated disposable PostgreSQL 17 database; no production connection used.
- New source snapshot/model migration, complete seven-day calendar with 36
  entries, fresh restart reuse, cold/stale coalescing, failed refresh retention
  and one-minute backoff, older write rejection, caller/service cancellation.
- Exact source/kind identity matching, special-season episodes, fileless/hidden
  records, enabled-library restriction, version deduplication and invalid input.
- Actual Emby extension route authentication for four token carriers, current
  permissions/role, revoked and disabled users; PIN-locked/unlocked and explicit
  empty allowed-library profiles.
- Rankings preserve source order and reuse the existing persistent TMDb center;
  unavailable source/errors remain explicit. Calendar weekday HTTP projection.
- Stored TMDb image GET/HEAD with Emby authentication. Local HTTP image fixture
  initially hit the existing private-host guard; replaced with a persisted
  source-center image fixture, without relaxing the guard.
- Matching JSON bounds include trailing bytes and duplicate-key rejection.
- `go test -race ./internal/service ./internal/repository ./internal/handler`
  with focused `TestExtension|TestDiscover|TestBangumi|TestTMDbCenter|
  TestDoubanDiscover|TestNextUpRoutes|TestEmbyShowsRejectEmptyParent|
  TestEmbyBrowseRequestErrors|TestEmbyPlaybackInfoMinSegments|
  TestEmbyPlaybackSelectionStringMinSegments` selection: all passed.
- Web ESLint and TypeScript/Vite production build passed.
- `node scripts/check-nextup.mjs` from web directory: both protocol groups,
  search/category/details/copy/empty state; 390x844, 768x1024 and 1440x900;
  light/dark main-content accessibility; non-admin denial passed.
- Two independent read-only reviews: backend auth/matching and calendar/UI
  contracts. No required fixes. Documented fetch-start timestamp semantics.
- Final manual review of request body bounds and related regression; diff check
  and Trellis context validation passed. The large pre-existing discover spec
  exceeds automatic injection size; applicable source-center sections were read
  manually during research.

## Not verified / limits

- No real upstream credentials, live third-party availability or production-size
  performance test; HTTP providers were mocked and data stored in a test DB.
- No LinPlayer integration or physical device test in this MSG task.
- Rankings keep existing center freshness behavior; only public weekly calendar
  gains access-triggered stale-while-revalidate. Unknown totals remain null.
- Exact matches cover ordinary metadata with provider identifiers. Independent
  NFO/HongGuo/HuangGuo catalog identity mapping remains out of scope.
- Public weekly rules are not personal tracking, historical ranges or exact
  episode air times. Refresh coalescing is process-local.

Temporary browser session, Web preview and disposable PostgreSQL container were
closed after validation. Existing untracked `core` was left untouched.
