# Implementation

1. Extend existing catalog read registration and safe Douban projections; preserve all administrator write gates and image handling.
2. Add source-first Discover navigation; provider-filter existing rankings and URL selection; remove standalone Data Center wrapper/route.
3. Reuse data pages/dialogs with poster cards, viewer/admin control boundaries and canonical filter/detail state.
4. Update affected browser regressions and add focused source-switch, public browse, hierarchy and permission coverage.
5. Run isolated PostgreSQL handler/service tests, Web lint/build and browser checks against a temporary local Vite server. Check dual themes and mobile/tablet/desktop/breakpoint widths.
6. Independent read-only review of full diff; primary agent fixes validated findings and repeats affected checks. Update relevant specs and record verification without committing.

## Validation

- `go test ./internal/handler -run 'Test(DoubanManagement|DiscoverCatalog|TMDbCenter|DiscoverDetailHTTPPermission)' -count=1` with isolated PostgreSQL.
- Focused Douban rich/poster tests after generated image URLs change.
- Focused supplement, public-projection and task-history classification tests
  across handler/service/database, using isolated PostgreSQL.
- `npm run lint`, `npm run build`, `git diff --check`.
- `node scripts/check-discover-centers.mjs`, updated Douban/Discover/HongGuo/HuangGuo checks, and source URL helper checks.
- `check-discover-boundaries.mjs`, task-log/task-startup and the bounded TMDb
  polling exhaustion/manual-reread regression in `check-discover-detail.mjs`.

Temporary test databases, services, browser sessions and screenshots are removed at completion. Never inspect, delete or stage the pre-existing `core` file. No production DB writes, user service restart, commits or push.

## Follow-up execution (2026-10-13)

1. Update task systems consistently across model/API/UI/history classification and focused tests.
2. Add durable Douban source-summary/supplement state and API-only worker; track TMDb explicit supplement batches independently from periodic refresh.
3. Change ranking reads to source truth, add bounded supplement APIs and safe public pending projections, and adapt dialogs/polling.
4. Fix the four frontend failure/focus boundaries and add browser regressions.
5. Run focused handler/repository/service/database tests using isolated PostgreSQL, Web lint/build and relevant mock-browser checks. Perform an independent read-only review, fix verified findings, document limits and remove temporary services/artifacts.

## Visual/category execution

1. Add persisted genre/department filter and category option queries/routes with
   isolated PostgreSQL multi-membership/pagination/permission coverage.
2. Unify source section controls, category filters, poster grid and ranked rows;
   retain each source's URL, search, download and failed-page behavior.
3. Extract and redesign the shared TMDb modal; wire category/ranking entry,
   clickable source people, season/episode navigation and compact admin actions.
4. Extend mock-browser contracts for categories and shared detail; verify themes,
   widths, focus/history, source truth, bounded ensure and retry/cancellation.
5. Independent read-only review, focused backend checks, lint/build, spec sync and
   temporary test cleanup. Do not commit, archive or restart the user service.

## Alignment audit closure

1. Preserve loaded TMDb related pages while requesting the next page; render the
   committed page number and retry the failed requested page. Preserve entry
   summary across center-ID resolution and first detail read failure.
2. Move Douban refresh state into shared details, gate by administrator role,
   expose the same management menu from categories and rankings, cancel on exit.
3. Reuse search toolbar, pager, ranking skeleton and continuation footer across
   sources; keep source query semantics and independent search cursors intact.
4. Verify failures after successful pages, summary fallback, both admin entry
   points, viewer isolation, source search and responsive themes. Independent
   review before completion; do not commit/archive/restart the user's service.
