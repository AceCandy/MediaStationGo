# Background Task Execution Contract

## Scenario: Unified Task History and Scrape Scheduling

### 1. Scope / Trigger

Apply this contract when adding an administrator-visible background execution,
changing task logs, or changing library and catalog scrape scheduling. Execution
history is observability only; business object state owns retry and recovery.

### 2. Signatures

- `TaskExecution`: UUID ID, kind, trigger, status, summary fields, JSON metrics,
  error, and start/update/finish timestamps.
- `GET /api/tasks?page=&page_size=` returns `items`, `page`, `page_size`, and `total`.
- `GET /api/tasks` also returns stable task `definitions`; each definition may
  expose current state, latest terminal execution, schedule, next run, and a
  server-owned manual action.
- `GET /api/tasks/definitions/:key/executions` returns paginated execution
  history for one validated task definition.
- `GET /api/tasks/:id/log?tail_bytes=` returns `content` and `truncated`.
- Task logs live at `<data_dir>/task-logs/YYYY-MM-DD/<task-id>.log`.
- Media scrape states include `pending`, `running`, `matched`, `no_match`, and `error`.

### 3. Contracts

- Triggers are `manual`, `scheduled`, or `event`; terminal execution statuses are
  `completed`, `failed`, or `interrupted`.
- The task-center root view lists task definitions, not one row per execution.
  Current state comes from an active execution or scheduler run, while latest
  result comes from the newest terminal execution. Same-kind business tasks
  such as people backfill/translation and media/catalog scraping stay separate.
- Task-definition logs first select a related execution, then reuse that
  execution UUID's log. Execution rows and daily log files remain unchanged.
- A persisted execution must exist before its background work starts. A create
  failure aborts that execution; log append failure does not abort business work.
- Startup marks stale task executions `interrupted` and changes stale media
  scrape `running` rows back to `pending`. Neither task history nor log content is
  a resume checkpoint.
- A worker atomically claims one complete movie or series by changing every
  currently pending member to `running`. Library media is checked before catalog
  hydration, and the current work is never preempted.
- A newly arrived member of a running series stays pending until that series
  finishes; it must not be claimed concurrently by another process.
- Explicit rescrape resets the selected business objects to `pending` and wakes
  the worker. Catalog checkpoints remain the catalog recovery authority.
- Global People backfill selects TMDB movie/series metadata with missing credits
  and `people_hydrated_at IS NULL`, but only when TMDB is the metadata's current
  source and the identifier kind matches the metadata kind. A successful
  provider response sets that timestamp even when the provider returns no
  credits; task history is not the checkpoint. A credits 404 invalidates that
  exact TMDB identifier and returns associated media to the regular scrape
  queue; transient failures retain the identifier for retry. The worker releases
  the shared scrape lock after each metadata object so newly imported media can
  take priority before the next object.
- People translation creates an execution only after it finds pending names or
  roles. Disabled AI, an empty sweep, and cache-only idle checks create no task.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Invalid task UUID or client path | Reject; never resolve a client-provided path |
| Task execution insert fails | Do not start the background work |
| Task log append fails | Continue work, log the application error |
| Process exits with a media group running | Restore its rows to `pending` at startup |
| Any member loses a claim race | Roll back the whole group claim |
| Library media exists while catalog work is pending | Process one library work unit first |

### 5. Good / Base / Bad Cases

- Good: a two-episode series is claimed and completed as one unit, then the worker
  checks newly imported media before taking another catalog item.
- Base: no work exists; the worker waits for a wake signal.
- Bad: select pending rows without a conditional update, or resume from an old
  task execution/log after restart.

### 6. Tests Required

- Task start/update/finish, pagination, and `running -> interrupted` recovery.
- Per-task daily log rollover, ordered read, invalid UUID, and tail truncation.
- Atomic whole-series claim, late-series-member exclusion, and
  `running -> pending` media recovery.
- API/UI contract plus manual, scheduled, and event trigger attribution.

### 7. Wrong vs Correct

```go
// Wrong: two instances can select and process the same pending work.
db.Where("scrape_status = ?", "pending").Find(&rows)

// Correct: conditionally update the complete group and roll back a partial claim.
db.Transaction(func(tx *gorm.DB) error {
	return claimPendingGroup(tx)
})
```
