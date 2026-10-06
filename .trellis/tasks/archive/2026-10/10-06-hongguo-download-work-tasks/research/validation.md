# Validation

## Code

- Isolated PostgreSQL regression across service/handler/database:
  TestHongGuoDownload*, TestTaskTracker*, TestTaskDefinition*, TestTaskLog*,
  TestTaskSystem*, TestNFOTask* (opt-in live and large WorkPagePlan excluded).
  Passed normally and with race checks; final race service duration 105.514s.
- New regressions cover per-work uniqueness, mixed states, cancel/retry/supplement,
  fresh snapshots after locks, restart after queue terminal commit, missing-summary
  table with successful publication/log retention, removed work, guarded/idempotent
  compaction and transaction rollback after rejected deletion.
- go vet on repository/service/compaction command passed.
- Compaction command compiles; git diff --check passed.
- Independent read-only reviews examined runtime and compaction separately.
  Production Boot already calls generic task recovery before downloads; standalone
  reconstruction additionally reconciles running/interrupted work summaries.

## Operational preview

- Fixed cutoff: 2026-10-06T05:02:07Z.
- Eligible legacy download executions: 2,854,356 across 17,424 works.
- Preview (including command compilation): 34.09s; child peak memory 304.1 MB.
- Before apply: total executions 3,110,453; download executions 2,854,492;
  protected older other-kind executions 255,961; work summaries zero.
- Business queue: 1,427,709 episodes, 17,424 placements.
- Real aggregate plan uses idx_hg_download_work_created_c and visits 81 episode
  rows for one work in a 1.43M-row queue; no all-episode aggregation.
  Execution 17.169ms during concurrent cleanup, dominated by physical reads.

## Limits

No production service restart/deployment or device/HTTP verification. No browser
changes; browser checks not repeated. Old running binary can continue creating
post-cutoff episode summaries until rollout. DELETE frees reusable database space;
no blocking table/index rewrite to return filesystem space.

## Apply and final audit

- Deleted exactly 2,854,356 old terminal executions, retained 17,424 unique work
  summaries. Duration 389.77s; peak child memory 426.8 MB.
- Final audit snapshot: 274,952 total executions; 18,974 download executions
  (17,424 work summaries and 1,550 protected old-format rows). Eligible old
  executions remaining zero; duplicate summary sources zero.
- Protected pre-cutoff other-kind executions remain 255,961; business episodes
  remain 1,427,709; placements remain 17,424.
- All 17,398 queue-associated work summaries match unchanged business counts;
  queue-less historical sources retain interrupted summaries.
- Manual ANALYZE was blocked by connector SQL policy and was not retried through
  another path. Read-only statistics confirm autoanalyze at 13:11:12 and
  autovacuum at 13:12:14 local time during compaction.
- Table plus indexes still about 1795 MB; no physical rewrite was requested or
  executed. Committed deletes provide reusable database space.
- No code commit/archive in this session. Other terminal changes are unrelated
  and preserved.


## HuangGuo AI follow-up

- Authorized scope now includes both catalogs. Shared repository state reduction
  preserves HongGuo C-collation access and HuangGuo's source-ID composite index.
  No additional schema change, dependencies or queue/publish refactor.
- Live read-only inspection: zero HuangGuo download executions and zero episode
  queue rows/works. No HuangGuo historical deletion is needed or performed.
- Added Movie/Series, supplement, mixed-phase states, single/work retry/cancel,
  terminal-commit recovery, concurrent upsert and cross-system-ID isolation tests.
  Missing task storage cannot prevent verified-file publication; safe logs retain
  episode identity and hide titles and synthetic signed-URL upstream errors.
- Existing transfer/verify/publish integration now asserts a running work at
  handoff, final completion and exactly one execution across both stages.
- PostgreSQL service/handler/database regressions passed normally
  (service 96.997s) and on final source with race checks (service 105.705s,
  handler 1.964s, database 7.121s). Included TestHuangGuoAIDownload* plus previous
  HongGuo/tracker/log/system/NFO regression selection; Live/WorkPagePlan excluded.
- go vet on repository/service/compaction command and git diff --check passed.
  Independent read-only follow-up review found no blocking issue; identified
  upstream-error test gap was filled and passed under race.
- No deployment, production restart or new production HTTP/browser verification.
  Unrelated concurrent working-tree changes remain untouched.


## Post-restart historical cleanup

- User reported restarting the service and explicitly authorized cleanup of
  intervening old-format records. Fixed cutoff: 2026-10-06T07:03:17Z.
- Read-only database scope and CLI dry-run agreed: 12,862 ended legacy HongGuo
  episode executions across 109 works (12,851 completed, 11 interrupted).
  No running legacy episode record existed in the inspected scope.
- Guarded compaction completed successfully, deleting exactly 12,862 records.
  Final audit: legacy HongGuo episode executions zero; 17,534 work summaries;
  duplicate source summaries zero; total execution rows 273,725.
- Business queue count remained 1,434,463 and placements remained 17,524.
  Other-kind records created before cutoff remained 256,191.
  All 112 queue-associated work summaries refreshed since cutoff matched queue
  metrics in the final audit snapshot. Live service may continue normal work.
- HuangGuo AI download executions remained zero; no cleanup needed.
  No service restart, queue deletion, media-file deletion or physical database
  rewrite was performed by this cleanup command.

## Authorized removal of legacy empty-system history

- User explicitly authorized deleting the previously inspected empty-system
  execution history. Fixed cutoff: 2026-10-06T07:11:07Z.
- Preflight found 151,039 rows: 146,103 completed, 4,815 failed and 121 interrupted.
  Every row had finished_at; there were no running empty-system records.
- One atomic DELETE with RETURNING removed only live empty-system rows whose
  status was completed/failed/interrupted and created/finished timestamps both
  preceded cutoff. Grouped returned counts matched the 151,039-row preview.
- Post-delete verification: empty-system history zero; pre-cutoff executions
  with a nonempty system preserved. Download queues, placements, media files and
  independent log files were not modified by this operation.
