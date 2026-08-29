# Implementation Plan

1. Add focused failing tests for bounded scan details, per-root flush/progress ordering, failed-root prune protection, and probe wake coalescing.
   - Verify: targeted Go tests fail for the intended missing behavior.
2. Add bounded scan-change retention and progress value/callback support while preserving existing scan method wrappers.
   - Verify: scan counters remain exact; detail output is capped and reports omissions.
3. Move `Flush()` to each successful root boundary and emit root/periodic progress.
   - Verify: multi-root tests prove serial order, commit-before-prune and continuation after one root failure.
4. Invalidate probe metadata when a persisted local media fingerprint changes.
   - Verify: changed media becomes eligible for backfill; unchanged media remains skipped.
5. Introduce the shared probe-backfill coordinator around existing `BackfillAll`/`BackfillLibrary` and `TaskKindProbe` handling.
   - Verify: automatic execution is visible, manual behavior stays compatible, duplicate wakes coalesce, and a wake during execution causes a later pass.
6. Replace scan and watcher ffprobe enqueue calls with boundary-level coordinator wakes; remove the unused in-memory local probe queue implementation and fields.
   - Verify: scan completion does not wait for ffprobe; watcher ingestion still requests backfill.
7. Wire scan progress into automatic, manual and scheduled scan task handles; update the probe task definition trigger description.
   - Verify: task snapshots/logs contain bounded semantic progress for event/manual/scheduled paths.
8. Run an independent review against the background-task execution contract and inspect the final diff for unrelated changes.
   - Verify: targeted tests pass, `git diff --check` passes, and no generated/private/debug files remain.

## Validation Commands

Per project policy, do not run full compilation by default. After implementation, request approval before executing targeted Go tests. Proposed commands:

```bash
go test ./internal/service -run 'Test(Scan|MediaProbe|TaskTracker)'
go test ./internal/handler -run 'Test(Scan|Probe|TaskDefinition)'
git diff --check
```

## Risky Files and Rollback Points

- `internal/service/scanner_scan.go`: prune ordering and partial-root failure behavior.
- `internal/service/scanner_local_ingest.go`: file-fingerprint and probe invalidation semantics.
- `internal/service/media_probe.go`: shared automatic/manual orchestration and wake races.
- `internal/handler/media_scan.go`, `internal/handler/tasks.go`: task creation and progress compatibility.
- `internal/service/service_builder.go`: lifecycle wiring; avoid starting duplicate workers.

No database migration is expected. Keep commits/diff separable at the scan-progress and probe-coordinator boundaries for straightforward rollback.
