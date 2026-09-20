# Execution plan

User approved the preceding behavior summary and explicitly authorized implementation; no additional planning approval is required.

1. [ ] Register independent models and persistence; test repeated PostgreSQL schema creation. No historical data migration (user confirmed no existing NFO library).
2. [ ] Add local identity/version resolver and scanner transactional ingestion; test duplicates, updates, malformed/missing sidecars and zero remote requests.
3. [ ] Add independent read projections and Web/Emby routes; verify pagination, visibility, versions, hierarchy and playback.
4. [ ] Add independent user-state/history/favorite/event source routing; verify ordinary and HongGuo unchanged. No historical state migration.
5. [ ] Separate NFO task-center system and local rescan/retry; verify logs, history and targets.
6. [ ] Run targeted Go/PostgreSQL tests, Web lint/build and diff checks; independent review; update contracts and hand off.

No service or production migration is started without an in-scope validation need. Stop any test services before completion. Preserve unrecognized user changes. No product change is complete while only the schema or scan path works.

## Final implementation checkpoint

- Steps 1–5 implemented: four models registered, transactional scanner/watcher
  ingestion, local artwork, explicit versions, independent Web/Emby projections,
  search/hierarchy/favorites/history/played state and separate NFO tasks.
  Retry is local rescan, not an online scrape job. No historical migration.
- Repeated scans are idempotent; bad/missing sidecars retain accepted snapshots.
  Ordinary scrape/recovery excludes NFO; shared metadata IDs stay NULL.
- Final tests caught and fixed series presentation losing SeriesID and default
  Emby SearchHints excluding local series. Both now have regression assertions.
- Real PostgreSQL NFO/task/scheduler/watcher/STRM tests passed, as did selected
  ordinary/HongGuo favorite, progress and Emby hierarchy regressions.
  All internal packages compile; Web lint/build and diff checks passed.
- Separate read-only ingestion/read/final-delta reviews completed.
- Broader regression is not fully green. Six failures reproduced on clean HEAD:
  TestEmbyItemsFilterByPerson, TestEmbyLatestItemsOrderByReleaseDate,
  TestHongGuoArtworkOwnershipMigration, TestMetadataSearchCountsPlayableTopLevelWorks,
  TestMediaSearchUsesExternalBackendAndFallsBack,
  TestMediaSearchFilteredSupportsChineseFuzzyTerms. No unrelated fixes applied.
- Not verified: production startup, browser QA or physical-player playback.
  Not integrated at this checkpoint: NFO playlist associations, administrator playback statistics,
  and global person identity/search. No shared metadata surrogates are created.
- Test PostgreSQL container and clean baseline worktree are removed at handoff.
  No application service was started. No commit/deployment/archive performed;
  task remains open for user acceptance.

## Approved follow-up: combined playback statistics

- Implemented `system=all|catalog|hongguo|nfo` through shared SQL aggregation.
  Individual event tables and rank identities remain separate. Existing API
  default stays catalog; Web default is all with source labels and scoped libraries.
- Reused existing source queries and a shared aggregator rather than introducing
  persisted summaries or copying three statistics pipelines. No write-path change.
- Real PostgreSQL ordinary/HongGuo/NFO/combined statistics and HTTP authorization
  tests pass: global paging, cross-table ID/time collisions, date boundaries,
  filters, season/work grouping, duplicate titles, Top 10 and deleted/rebound files.
- Go internal-package compilation, Web lint/build and independent read-only
  review passed. `check-playback-stats.mjs` passed with mocked APIs for source
  switching/reset, links/labels, empty/error states and both themes at mobile,
  tablet, desktop and the 1024px breakpoint.
- Browser waits use completed resource entries and rendered source-specific
  library counts; URL changes alone precede asynchronous view updates.
- Administrator statistics is now integrated. Remaining scope exclusion:
  NFO playlists. Production deployment and real production-volume performance
  are not verified. Temporary preview/browser/PostgreSQL are stopped at handoff.

## Archive handoff

User requested commit and archive after the implementation and validation report.
The implementation and approved statistics follow-up are accepted for archival;
the manual/deployment checks and playlist exclusion above remain explicit limits,
not claims of completed validation. No remote push or deployment is included.
