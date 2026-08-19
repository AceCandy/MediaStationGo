# Implementation Plan

1. Add schedule metadata and runtime reset support to `SchedulerService`.
   - Register all five periodic jobs with server-owned setting keys, defaults, and bounds.
   - Remove redundant per-job periodic gates once scheduler enabled state owns automatic execution.
   - Verify disabled jobs have no next run, manual runs bypass periodic state, and runtime updates reset countdown.

2. Route independent periodic workers through Scheduler.
   - Remove backfill/translation local tickers while retaining startup/event/manual wakes.
   - Add scheduled entry points and run-level serialization.
   - Move account cleanup periodic invocation into Scheduler and add tracked execution details/metrics without weakening its safety gate.
   - Verify event/manual behavior remains unchanged and scheduled triggers are attributed correctly.

3. Expose schedule configuration through task definitions and API.
   - Extend task definition/job status DTOs with `schedule_config`.
   - Add allowlisted update handler with backend validation, persistence, runtime reset, and refreshed response.
   - Verify unknown/event-only definitions and invalid intervals are rejected without partial runtime changes.

4. Add watcher batch executions.
   - Add the stable watcher definition and isolated task kind/filter.
   - Inject the tracker, prefilter settled paths, process one execution per batch, record semantic details/metrics, continue after per-path failures, and requeue when execution creation fails.
   - Verify full-scan and watcher histories/logs remain isolated.

5. Add task-center schedule controls.
   - Extend typed API contracts and add the schedule update call.
   - Add a compact row editor using backend-provided values/bounds; retain current run/log controls and responsive layout.
   - Verify loading, saving, validation errors, disabled state, and immediate next-run refresh.

6. Run focused validation and independent review.
   - Backend: targeted Scheduler, task-definition, worker, watcher, and handler tests only; do not run a full Go build unless separately approved.
   - Frontend: `npm run lint` and `npm run build` in `web/`.
   - Repository: `git diff --check` and a separate `trellis-check` review of spec compliance, data flow, task-log isolation, and unrelated changes.

## Rollback Points

- Schedule registry/API changes can be reverted without deleting persisted setting rows.
- Worker timer migration is independently reversible because event/manual entry points remain.
- Watcher task tracking is isolated from scanner persistence and can be reverted without changing media rows or historical logs.
