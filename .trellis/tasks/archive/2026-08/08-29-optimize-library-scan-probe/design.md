# Technical Design

## Boundaries

- `ScannerService` owns filesystem traversal, local metadata ingestion, per-root pruning and scan progress emission.
- `MediaProbeService` remains the only writer of complete ffprobe documents through `ProbeMedia`.
- A shared probe-backfill coordinator owns automatic/manual execution orchestration, task creation, wake coalescing and the single-run invariant.
- `TaskTrackerService` remains observability only; missing or stale probe metadata in the database is the recovery authority.

## Scan Data Flow

1. The handler or scheduler creates the existing scan `TaskHandle`.
2. It calls a progress-aware scan entry point while existing callers may keep using the compatibility wrapper.
3. `scanLibrary` loads enabled root records, then processes them sequentially.
4. For each root:
   - emit root-start progress;
   - resolve the accessible path;
   - walk supported media files and update bounded counters;
   - flush pending writes at the root boundary;
   - prune missing rows only after a successful walk;
   - emit root-finish progress.
5. Reconcile media parts and run the existing post-scan hooks.
6. Finish the scan task, then request automatic probe backfill when media were added or updated.

Progress is throttled by media count and/or elapsed time. Task messages change only at meaningful boundaries; repeated counter updates persist metrics without producing duplicate message lines.

## Probe Backfill Data Flow

1. New media has no probe document. When an existing file fingerprint changes, its previous probe document is invalidated in the same ingestion flow.
2. Scan and watcher boundaries send a coalesced wake signal to the shared coordinator; no per-file ffprobe goroutine is started.
3. The coordinator starts at most one `TaskKindProbe` execution with trigger `event` and runs the existing `BackfillAll`/`ProbeMedia` path.
4. A wake received during an active run remains pending. After the run settles, the coordinator checks pending state and performs another pass only when needed.
5. The manual task-center action calls the same coordinator with trigger `manual`; an active run still returns the existing conflict behavior.
6. Missing/stale database state is sufficient for later manual or automatic recovery after process restart; task history is never a checkpoint.

The old `localMediaProbeQueue`, path-reservation map and worker startup fields are removed once all scan/watcher boundaries use the coordinator.

## Progress Contract

Introduce a small scan-progress value containing root totals/current root and the existing scan counters. It is passed through an explicit callback rather than global state. Compatibility wrappers use a nil callback.

Handlers translate progress into `TaskUpdate` values:

- stage: `scan`;
- message: semantic root start/finish or periodic progress;
- metrics: roots total/completed plus visited/added/updated/skipped/removed/errors;
- details: only new root errors or bounded semantic summaries.

## Bounded Change Details

`ScanResult` continues to retain aggregate counters but stores only a fixed number of `ScanChange` records. It separately counts omitted changes. `ChangeDetails` appends one summary line when omission occurred. The limit is server-owned and covered by a focused unit test.

## Compatibility

- Keep existing public scan methods as wrappers so organizer and other callers do not require unrelated edits.
- Keep task definition keys and HTTP routes stable.
- Update the probe task definition trigger text to include automatic event execution.
- Preserve ffprobe concurrency limits inside `FFprobeService`/`ProbeMedia`.
- Preserve the current skip rules for unchanged media, unsupported probe sources and valid probe documents.

## Failure and Recovery

- Root resolution/walk failure records an error and skips prune for that root.
- Probe failures remain visible in `ProbeBackfillResult` and leave the media discoverable by a later backfill.
- A task-execution creation failure does not roll back successful library persistence or scan ingestion; pending probe state remains recoverable.
- Rollback consists of restoring the old scan-to-probe call and removing the coordinator wiring; no schema migration is planned.

## Trade-offs

- Roots remain sequential to protect NAS and database throughput; parallelism is deliberately excluded.
- Automatic backfill may start later than the old in-memory queue, but scan completion becomes independent from ffprobe throughput and the work becomes visible/recoverable.
- True bulk media upsert is deferred because it changes repository conflict semantics and is not required to fix the current coupling.
