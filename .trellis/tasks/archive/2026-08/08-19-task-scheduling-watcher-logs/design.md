# Design: Task Scheduling Controls and Watcher Logs

## Boundary

Extend the existing task-definition center instead of creating a second settings page or scheduler framework. Keep `TaskExecution` and per-definition daily logs unchanged. Move only periodic trigger ownership into `SchedulerService`; event and manual entry points remain in their current services.

## Schedule Registry

Each periodic job owns server-side metadata: stable job name, enabled setting key and default, interval setting key and default, and allowed interval range. The initial registry is:

| Definition | Job | Enabled setting | Interval setting | Default |
| --- | --- | --- | --- | --- |
| 媒体库扫描 | `library_scan` | `scan.periodic_enabled` (false) | `scan.interval_seconds` | 24 h |
| 媒体整理 | `organize_source` | `organize.auto` (false) | `organize.interval_seconds` | 5 min |
| 人物信息补齐 | `people_backfill_periodic` | `people.backfill_periodic_enabled` (true) | `people.backfill_interval_seconds` | 10 min |
| 人物翻译 | `people_translation_periodic` | `people.translation_periodic_enabled` (true) | `people.translation_interval_seconds` | 10 min |
| 账号清理巡检 | `account_cleanup` | `device.account_cleanup_enabled` (false) | `device.account_cleanup_interval_seconds` | 24 h |

Intervals are whole seconds between 60 seconds and 30 days. Defaults preserve current behavior. `metadata.people_ai_translate` remains the separate feature capability switch. The existing account-cleanup switch remains a destructive-operation safety gate and therefore continues to block its Telegram manual action when disabled.

## Runtime Scheduling

`SchedulerService` becomes the single owner of periodic timers and reports `enabled`, `interval_seconds`, and `next_run` in `JobStatus`. A disabled job has no `next_run`. Enabling a job or changing its interval resets its countdown from the save time; it does not execute immediately.

The scheduler update service validates the definition/job allowlist and interval before persisting settings. After persistence, it updates the in-memory job and signals its loop to reset without restarting the process. Existing manual scheduler actions bypass the periodic enabled flag.

Remove periodic tickers from people backfill, people translation, and account cleanup loops. Their existing event/manual paths stay intact. Scheduler callbacks invoke explicit scheduled methods with `TaskTriggerScheduled`; run-level mutexes prevent a scheduled pass from overlapping an event/manual pass.

## API Contract

Extend each task definition with an optional `schedule_config`:

```json
{
  "enabled": false,
  "interval_seconds": 86400,
  "min_interval_seconds": 60,
  "max_interval_seconds": 2592000
}
```

Only definitions backed by a configurable periodic job expose it. Add `PUT /api/tasks/definitions/:key/schedule` with `{enabled, interval_seconds}`. The server validates the stable definition key, refuses unsupported definitions and invalid intervals, persists the settings, resets the runtime timer, and returns the refreshed definition. Existing task run/history/log APIs remain compatible.

## Task Center UI

Scheduled task rows show the effective enabled state and formatted interval. A row-local edit action opens one small schedule form containing an enabled switch plus numeric duration and unit. Values and bounds come from `schedule_config`; the frontend only converts units and displays backend errors. Saving refreshes the task definitions so `next_run` changes immediately.

Event-only tasks, including the new watcher definition, have no schedule editor. Existing run and log actions remain unchanged.

## Watcher Execution Model

Add a stable `library_watch` definition and a dedicated watcher task kind/filter so its history and daily log cannot mix with full scans. Inject `TaskTrackerService` into `WatcherService`.

At each debounce tick:

1. Collect settled paths and discard directory-only or unsupported-extension events.
2. Start one event-triggered execution before processing the remaining batch.
3. If task persistence fails, return candidates to `pending` for retry instead of losing the file events.
4. Process every path even when one fails, appending only new semantic detail lines and updating batch metrics.
5. Finish failed when any path failed; otherwise finish completed. Errors are sanitized before entering task logs.

Existing scanner results provide add/update details. Removed rows log `🗑️`; unchanged candidates log `⏭️`; failures log `❌`. Pure directory registration and irrelevant files create no execution.

## Compatibility and Rollback

Existing settings keep their meanings. New setting keys use defaults matching current fixed intervals and worker behavior. No database migration or historical-log rewrite is required. Rollback restores local tickers/fixed scheduler intervals and omits the new definition fields; persisted unknown settings are harmless.

## Risks

- Timer reset and concurrent manual/event execution can race; scheduler state mutation and worker passes require explicit synchronization tests.
- Watcher observability must not silently drop file events when task persistence fails; requeue is mandatory.
- Account cleanup is destructive, so its existing safety gate remains stricter than the general “periodic switch does not affect manual execution” rule.
