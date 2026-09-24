# Implementation and Verification

1. Add startup snapshot/stage timing, sequential Boot, scheduler-last readiness, and shutdown coordination.
2. Optimize watcher traversal/counters; remove retired download migration and duplicate settings load.
3. Expose startup status, guard task/scheduler entry points, and render independent readiness polling.
4. Add focused startup/handler/walker regressions and isolated browser checks for delayed history, transitions, failure, responsive layouts, and cancellation.
5. Run focused Go tests/race, Web lint/build, browser checks, and diff validation. Use isolated PostgreSQL test schemas if configured; report skips.
6. Independently review, fix findings, update the contract, and stop test servers/browsers.

Rollback: revert task-owned changes together. No data/schema migration or stored-path mutation.

## Completed Implementation

- Added independently locked startup status and admin endpoint, safe warnings,
  stage timing, directory counters, scheduler-last ordering and Boot/Close joining.
- Hid execution/configuration until ready; guarded task and scheduler write
  entry points; kept logs readable and status/history polling independent.
- Replaced watcher-only traversal to avoid ordinary-file Info, retaining explicit
  hidden/symlink roots, hidden-child exclusion and failed-root recovery.
- Removed retired HongGuo pending-directory migration and duplicate settings load.
- Recorded the cross-layer startup contract in the background-task spec.

## Verification

- Passed isolated PostgreSQL tests with `-race` for Startup, Boot, WatchDirectory,
  Watcher, Scheduler, TaskStartup, TasksHandler, TaskDefinition and
  HongGuoDownloadDirectory across service/handler/server packages. Re-ran the
  startup/watcher subset after adding the explicit symlink-root regression.
- Passed five non-live HongGuo download regressions: directory identity, grouping
  and retry, placement/cancel/recovery, publication conflicts, and validation.
- Passed Web lint and production build; task-log checks and isolated startup and
  HongGuo supplement browser scripts. Startup script covers history held pending,
  hidden controls, readiness loss/recovery, stale configuration dialogs and aborts.
- Dark/light snapshots and overflow checks cover 390, 768, 1024 and 1440 pixels;
  inspected mobile/desktop snapshots. No browser runtime errors.
- Independent backend/frontend review completed. Follow-up fixes: explicit
  readiness in old test mocks, discard action dialogs on readiness loss, and
  preserve the original explicit symlink-root watch. Added shutdown-during-Boot
  verification and re-ran relevant checks. `git diff --check` passed.

## Limitations and Handoff

- No production restart or startup-duration benchmark; no promised number of
  seconds saved. Directory enumeration/registration remains proportional to the
  library tree. Use new stage-duration logs for a later production comparison.
- No production path/data rewrite and no real external download test. The user
  confirmed retirement of the legacy migration; its completion was not separately
  audited against production data.
- The optional broader `check-ui-primitives.mjs` run passed its task-page section
  but failed the later unchanged file-manager page's light-theme button-color
  equality assertion. Only its startup mock was adapted; unrelated styling was
  not changed. This is not a full-site UI quality pass.
- The user approved committing and pushing this completed scope; preserve the
  validation limitations above in the task archive and session journal.
