# Implementation

1. Add shared season date projection and cooldown policy; cover date boundaries and malformed/unknown inputs.
2. Build and sort the due-season list once; reuse conditional season lease acquisition and fixed-cutoff page claims.
3. Apply policy to checkpoint guards, incomplete saves, 404 and inventory registration. Remove misleading fixed-delay logs.
4. Run focused repository/service tests on isolated PostgreSQL, including existing leases, retries and 30,000-row plans. Run targeted Go vet and diff checks.
5. Independently review the final diff and synchronize the task execution contract. Record checks and remaining deployment limitations.

Scope: TMDb recheck repository/service files and their regression tests, task records and the background-task contract. No frontend or dependency changes.

## Verification Results

- Passed isolated PostgreSQL 17 tests with `MEDIASTATION_TEST_POSTGRES_DSN` configured (not skipped): `go test -race ./internal/repository ./internal/service -run 'TestTMDbRecheck|TestTMDbMetadataRecheck|TestFetchTMDbMetadataRecheck|TestSeriesInventory|TestTMDbSeasonBatch' -count=1`.
- Passed `go vet ./internal/repository ./internal/service` and `git diff --check`.
- Date policy unit cases cover 30/31/365/366-day boundaries, ongoing long seasons, upcoming/distant-future dates and invalid dates. Integration cases cover all four cooldowns and omitted upstream date preservation for incomplete success, 404 and inventory absence, plus checkpoint guards and actual delay logs.
- Generic-plan test with 30,001 jobs / 1,001 seasons: ID-based acquisition used target primary-key probes (~0.20 ms), one-time order preparation aggregated dates once per season (~342 ms, including ~311 ms PostgreSQL JIT). These are local fixture measurements, not a production latency guarantee.
- Independent read-only review found no blocking issue. Final main-thread review checked subsequent simplification and local-date preservation, followed by the final race-enabled suite above.
- No full-repository suite, frontend checks, live TMDb requests or deployment were performed. Existing queued due times transition on their next due processing. No production schema/data changes or new dependencies.
