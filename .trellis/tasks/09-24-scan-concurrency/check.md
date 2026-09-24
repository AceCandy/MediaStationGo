# Verification

## Reproduction and root cause

- Before the fix, `TestLocalScanAdmissionIsGlobal` admitted another library/root and `TestSchedulerScanSharesGlobalAdmission` accepted a scheduled run while an HTTP scan held the slot.
- With two independent PostgreSQL connections both paused before the media INSERT, the old ordinary and HongGuo writers returned `23505` for `idx_media_path` and different IDs.
- Root causes: inconsistent entry-point admission (cross-layer contract gap), check-then-insert race, and attempted recovery inside an aborted PostgreSQL transaction (implicit transaction assumption). Existing serial tests did not exercise this boundary.
- Regression fixture correction: after expanding the pool, verification queries must also use a schema-pinned connection; the shared pool can open a default-schema connection. No production data was involved.

## Implemented boundary

- One ScannerService slot covers HTTP, automatic-root, scheduler and STRM admissions. Scheduler reserves before asynchronous launch. Multi-root/STRM requests retain the slot while running their existing target executions sequentially.
- Busy scan APIs return 409; automatic follow-ups log/report rejection without undoing saved roots/generated files. Task-creation failures cannot launch untracked scans.
- Shared insertion uses conflict-do-nothing only for path uniqueness, then reloads a real row/ID and rechecks ordinary/HongGuo/NFO ownership. Other insert errors remain visible. No schema/index/data migration.

## Checks passed

Real temporary PostgreSQL 16 on loopback with `MEDIASTATION_TEST_POSTGRES_DSN` set, isolated test schemas (not skipped):

```sh
go test ./internal/service ./internal/repository ./internal/handler -run 'Test(Scheduler|ScanLibrary|StartLibraryRootScan|ScanAdmission|IngestPath|Watcher|NFORepository|NFOScheduler|NFOWatcher|NFOTasks|MediaUpsert|HongGuoBinding|STRMRefresh|TaskDefinitionRunHandler.*(Scan|MediaLibraries))' -count=1
go test -race ./internal/service ./internal/repository ./internal/handler -run 'Test(LocalScanAdmission|SchedulerScan|ScanAdmissionHTTPAndBatches|MediaUpsertConcurrentPath|MediaUpsertDoesNotIgnoreOtherInsertErrors|STRMRefresh)' -count=1
go test -race ./internal/service -run 'Test(Scheduler|Watcher)' -count=1
go vet ./internal/service ./internal/repository ./internal/handler
git diff --check
```

- Concurrent ordinary/HongGuo/NFO reuse; both directions of all source conflicts; primary-key error preservation; HongGuo binding preservation.
- HTTP manual/root/task-center/scheduler rejection without executions; task-create failure release; actual scheduler shutdown release; multiple new roots retained; failed first STRM target does not suppress later targets.
- Two independent read-only reviews (scan admission and persistence): no confirmed implementation findings. Added NFO-NFO concurrency coverage requested by persistence review, then reran focused and race regressions successfully.

## Limits and handoff

No production rescan, deployment, process restart, browser UI run or full-repository test run. Admission is per application instance, not a distributed lock; shared database path uniqueness remains authoritative. Existing caller-owned scan lifetimes are unchanged. User approved the single repair commit with “ok”; archive only after that commit. No push or deployment is authorized.

## Cleanup

Temporary PostgreSQL container `mediastation-scan-test-0924` was stopped and automatically removed after final verification; its absence was confirmed. No existing application services were stopped. No credentials, runtime logs, copied media or screenshots were added to the repository.
