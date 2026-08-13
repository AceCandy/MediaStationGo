# Design: Task Definition Center

## Boundary

Keep `TaskExecution` as the immutable summary of one run. Add a separate read model for stable task definitions; do not replace, merge, or migrate execution rows and log files.

## Task Identity

Use stable server-owned definition keys rather than grouping by display name. The initial registry contains:

- `organize`
- `library_scan`
- `probe_backfill`
- `media_scrape`
- `catalog_scrape`
- `people_backfill`
- `people_translation`
- `recycle_purge`

Execution-to-definition matching uses existing structured fields where they are stable. People tasks use exact fixed names; scrape tasks use the existing server-owned catalog name prefix so legacy rows remain classifiable. New catalog executions also persist `source_path=catalog` as an explicit diagnostic marker. Dynamic execution names otherwise remain unchanged.

## API Read Model

`GET /api/tasks` returns a fixed `definitions` collection. Each row includes identity, display name, trigger/schedule description, current status, latest execution summary, scheduler timing, and an optional action key. Existing execution pagination fields remain temporarily compatible for callers outside the new page.

Add a definition-history query accepting only a validated server-owned definition key. It returns paginated matching execution summaries; the existing `/api/tasks/:id/log` endpoint continues to read the selected execution log.

## Runtime Composition

The handler/service composes:

1. Static task definition metadata.
2. Active and latest matching `TaskExecution` rows.
3. Scheduler status for the three registered periodic jobs, including next run.

Current status is derived from active execution or scheduler state. Latest result is derived only from the latest terminal execution. Thus an idle task may show an earlier failed result without being labelled currently failed.

## Actions

- Scheduler-backed definitions reuse `RunNowAsync(jobName)`.
- `people_backfill` reuses `TriggerPeopleBackfill` through the existing handler path.
- Scope-dependent or event-only definitions have no run action.
- The frontend dispatches only known action types returned by the API; disabled/running state prevents duplicate clicks.

## Compatibility And Rollback

No database schema or historical-data migration is required. Existing execution/log endpoints remain available. Rollback consists of restoring the previous page and omitting the new definition/history fields.

## UI

Replace the execution table and scheduler modal with one responsive task table. Each definition is one row with current state, trigger/schedule, latest result/time, next run, and icon actions. The log dialog first shows related executions and then reads the selected execution's log. Empty history is explicit.
