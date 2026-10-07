# Regression evidence

- Baseline task endpoint: 27 definitions, stale test expected 26.
- Counts test inserted HongGuo movies, violating `chk_hongguo_work_series`.
- Shared parallel fixture now uses source 101 in album 101; payload requests still used the obsolete `hg-work-source` identity.
- Isolated PostgreSQL 16 hydration plan: work reads 5,240, comprising page work scan 90, album scans 450, current-file work lookups 200 and replacement work lookups 4,500. The main excess was effective-state replacement lookup, not album presentation.
- Outer unique-source LEFT JOIN in `PlaybackStates` reduces total work reads to 940. State reads remain 300, artwork reads 90, JIT functions 0. Existing 3,000-work limit is unchanged.
- Targeted race regressions pass for counts, parallel payloads (overlap/error/single connection), hydration payload parity/plan, source search and replay/deleted versions.
- Independent NFO plan test passes in approximately 31 seconds; the previous package timeout at eight minutes was not evidence of a failing NFO plan.
- Read-only independent review found no issues in the five code/test files; it checked source uniqueness, orphan histories, replacement eligibility and alias isolation.
- Full handler/repository race regressions pass (approximately 168s/309s); vet passes for all three packages.
- Broader service race run completed more than 300 top-level tests, including HongGuo library/detail/container plans, global payload parity, NFO plans, continuation and resume plans. It was stopped after confirming the package has 1,128 top-level tests; the full service package is not claimed green.
- Affected consumers still pending in that run were checked separately: `TestHongGuoEmbyPlayableIdentityAndUserState`, `TestHongGuoLibrarySeriesPresentation`, `TestPlaybackStateReplayAndDeletedVersion` (including the added orphan case), and `TestActivityRefreshKeepsPlaybackState` all pass with race detection.
- Two unrelated failures remain: `TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog` expects one favorite count/page query but observes two; `TestHongGuoDownloadWorkPagePlan` fails its episode scan limit. Both reproduce in a detached clean HEAD worktree without this repair. Their test/query code was not modified. The user was asked whether to include the ordinary Series issue; no scope expansion was authorized during this turn.
- External-provider live tests for OpenSearch HongGuo and Douban were skipped by their existing explicit opt-in guards.
- No production state changes, deployment or push. The user subsequently authorized committing and archiving this repair.
