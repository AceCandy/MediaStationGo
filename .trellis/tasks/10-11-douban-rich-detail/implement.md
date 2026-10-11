# Implementation
1. Add rich JSONB storage and CAS.
2. Parse verified webpage sections; integrate manual and periodic supplementary refresh with protected persistence.
3. Add authenticated lazy avatar endpoint using people image storage.
4. Project rich DTO and implement wide theme-aware dialog with shared MediaCredits.
5. Parser/CAS/image and handler tests; targeted Go race tests on isolated PostgreSQL, Web lint/build and browser mock responsive tests.
6. Independent read-only review, fix verified issues, update spec. Do not commit until requested.

## Verification results
- Isolated PostgreSQL 16: race tests for Douban/PeopleImage passed across service,
  handler, repository, model and database packages. Follow-up rich CAS/retry
  regression tests passed after final fixes.
- Go vet on the five affected packages passed.
- Web lint/build, image URL checks and data-center mock browser scenarios passed;
  checked light/dark at 390/768/1279/1280/1440, real mock image decode, avatar
  circle/version, backdrop/Escape/focus, partial-success feedback and cancellation.
- Desktop and mobile screenshots inspected; temporary capture files removed.
- Independent backend/frontend reviews completed; identified independent retry
  gap fixed and reviewed again. Native timestamp precision covered by avatar
  retry preservation test.
- Not verified: deployed process/schema upgrade, authenticated upstream HTTP
  webpage availability or real upstream avatar import. No production restart,
  production database write or commit performed.

## Refresh ownership/navigation follow-up
- Introduced the common `douban_data_refresh` execution kind; startup reclassifies exact legacy refresh executions while retaining definition/log keys and schedule settings.
- Candidate scan now pages directly through Movie/Series center rows, including unbound subjects, preserving existing freshness and retry predicates.
- Data Center navigation is admin-only in Viewer Space between Discover and Me.
- Verified isolated PostgreSQL race tests for unbound refresh, fresh/expired/partial/degraded/retry candidates, tuple paging, exact idempotent history migration and task ownership. Web lint/build and browser mock checks passed, including navigation scope/order/access and both themes across responsive widths.
- Independent read-only review found no confirmed defect. Production deployment/restart and actual upstream refresh were not exercised for this follow-up.

## Bounded refresh execution plan
1. Add shared oldest-first base candidates/count, protected save outcomes and strict cached base fetch observation.
2. Replace task batch loop with bounded snapshot, preserve retry/pause and report accurate metrics.
3. Reuse scheduler count controls and add refresh-mode validation/API/UI/actions/timestamps.
4. Add request-whitelist, fairness/limits/freshness/concurrency/persistence regressions; run targeted PostgreSQL race tests, Web lint/build/browser mocks and independent read-only review.

## Bounded refresh verification results
- Implemented strict movie/tv Rexxar-only scheduled refresh, shared TTL/retry
  selection, oldest-first bounded snapshots and configurable 1–100 count
  (default 100). Supplement freshness never schedules base refresh.
- Protected save observations distinguish cache/retry/concurrent-save skips,
  changed and unchanged accepted writes; task metrics count physical HTTP
  attempts and recount remaining eligible rows using this round's cutoff.
- Added explicit base/full manual modes, preserved default full compatibility,
  rich data and independent timestamps; reused existing schedule count controls.
- Isolated PostgreSQL 16 race tests passed for Douban/Scheduler/TaskDefinition/
  TaskSystem across service, repository, handler, database and model. Final
  service/handler race regressions passed after concurrent-retry and invalid-mode
  coverage was added. Go vet passed for service, repository and handler.
- Web lint/build passed. Browser mocks passed for base/full actions, retained
  supplement data, cancellation, independent timestamps, task quantity limits,
  request metrics and light/dark responsive widths. Existing Hongguo/Huangguo
  supplement task compatibility checks also passed.
- Independent read-only backend and frontend reviews found no confirmed defect;
  final source inspection and git diff whitespace checks passed.
- Not verified: live upstream requests, production restart/deployment or schema
  upgrade. The batch cap can leave eligible rows for later rounds; physical HTTP
  attempts can exceed selected rows when proxy retries occur. No commit made.
