# Background Task Execution Contract

## Scenario: Unified Task History and Scrape Scheduling

### 1. Scope / Trigger

Apply this contract when adding an administrator-visible background execution,
changing task logs, or changing library and catalog scrape scheduling. Execution
history is observability only; business object state owns retry and recovery.

### 2. Signatures

- `TaskExecution`: UUID ID, kind, trigger, status, summary fields, JSON metrics,
  error, and start/update/finish timestamps.
- `TaskUpdate.Details`: append-only operator-facing lines. The tracker preserves
  an existing semantic marker and prefixes an unmarked line with `ℹ️`.
- `GET /api/tasks?page=&page_size=` returns `items`, `page`, `page_size`, and `total`.
- `GET /api/tasks` also returns stable task `definitions`; each definition may
  expose current state, latest terminal execution, schedule, next run, and a
  server-owned manual action.
- Configurable periodic definitions expose `schedule_config` with `enabled`,
  `interval_seconds`, `min_interval_seconds`, and `max_interval_seconds`.
- `PUT /api/tasks/definitions/:key/schedule` accepts `enabled` and
  `interval_seconds`, then returns the refreshed task definition.
- `POST /api/tasks/definitions/library_scan/run` requires
  `{ "library_id": "<library UUID>" }`; this target applies only to the manual
  execution and is not persisted into the periodic schedule.
- `GET /api/tasks/definitions/:key/executions` returns paginated execution
  history for one validated task definition.
- `GET /api/tasks/definitions/:key/log?date=YYYY-MM-DD&tail_bytes=` returns
  `date`, newest-first `dates`, `content`, and `truncated`. Omitting `date`
  selects the newest available date.
- Task logs live at
  `<data_dir>/task-logs/YYYY-MM-DD/<task-definition-key>.log`.
- Media scrape states include `pending`, `running`, `matched`, `no_match`, and `error`.
- Watcher batches use `TaskKindWatch`, `TaskTriggerEvent`, and the stable
  `library_watch` definition; they never reuse the full-scan kind.

### 3. Contracts

- Triggers are `manual`, `scheduled`, or `event`; terminal execution statuses are
  `completed`, `failed`, or `interrupted`.
- The task-center root view lists task definitions, not one row per execution.
  Current state comes from an active execution or scheduler run, while latest
  result comes from the newest terminal execution. Same-kind business tasks
  such as people backfill/translation and media/catalog scraping stay separate.
- Every execution of the same stable task definition appends to one local-day
  file. Resolve the definition key with the same kind/name filters used by the
  task center; raw kinds cannot distinguish people backfill from translation or
  media scraping from catalog scraping.
- The task-log UI selects an available date and displays the complete bounded
  tail of that definition's daily file. It does not select an execution row.
- Manual log refresh reloads the currently selected date without closing the dialog.
- `SchedulerService` owns all configurable periodic timers. The initial jobs are
  library scan, source organization, people backfill, people translation, and
  account cleanup. Their intervals are whole seconds from 60 seconds through 30
  days. Saving a schedule persists both settings and resets the live countdown;
  a disabled job has no next run and does not block its server-owned manual action.
- Every administrator-visible scheduler job exposes a manual task-definition
  action. `RunNowAsync` bypasses the schedule's enabled flag, preserves the
  scheduler's per-job concurrency guard, and marks executions as `manual` via
  the scheduler context; timer-driven runs remain `scheduled`.
- A manual `library_scan` task scans only the selected library's enabled roots.
  Timer-driven `library_scan` runs carry no target and continue scanning every
  enabled library. Creating a library starts an `event` whole-library scan;
  adding enabled roots to an existing library starts one `event` root scan per
  newly added root. Scan startup failure is logged after persistence and never
  rolls back or changes the successful library/root save response.
- The task center shows a danger confirmation before manually running account
  cleanup. Canceling sends no request, and the complete confirm/run flow admits
  only one pending action so rapid clicks cannot create duplicate dialogs or requests.
- Schedule setting keys, defaults, and bounds are server-owned. The task-center UI
  only converts seconds into a numeric duration and unit and displays server errors.
- A watcher debounce window creates at most one execution after directory and
  unsupported-extension events are removed. Every remaining path is processed
  even after another path fails. A task-execution create failure requeues the
  complete candidate batch; individual business failures are recorded and make
  the batch failed without creating a retry loop.
- New task-center log lines never contain `[INFO]`, `[DETAIL]`, or `[ERROR]`.
  Manual and scheduled executions write `🔻` first for start, `🔄` for
  progress, `❌` for terminal errors, and `🔺` last for finish. Event executions
  omit the redundant `🔻`/`🔺` lifecycle lines while retaining progress,
  details, and terminal errors. Because the UI reverses lines, a retained `🔺`
  is the execution block's top boundary and renders as `▼`; a retained `🔻` is
  its bottom boundary and renders as `▲`.
- Detail markers are `➕` add, `🗑️` delete/cleanup, `🔄` update/progress,
  `✅` success, `❌` failure, `⏭️` skip, `⚠️` warning/no-match, and `ℹ️`
  summary. The UI replaces them with fixed-color badges instead of relying on
  platform emoji fonts. Historical level labels are hidden only when they occur
  in the structured position immediately after the timestamp; legacy error
  lines fall back to the failure badge and other legacy lines to the info badge.
- A persisted execution must exist before its background work starts. A create
  failure aborts that execution; log append failure does not abort business work.
- `TaskUpdate.Details` are append-only log records for that update, not an
  accumulated task transcript. A batch worker must pass only newly produced
  detail lines on each `Update` and must not repeat them on `Finish`.
- Detail producers should choose the accurate semantic marker. The central
  tracker supplies `ℹ️` only as a fallback, so no task can reintroduce a bare detail
  or a bracketed level label.
- Progress messages are appended only when their text changes. Repeated updates
  still persist metrics and append new details without duplicating the same summary.
- Both `Details` and the error passed to `TaskHandle.Finish` are written to the
  per-task log. Provider errors must be sanitized before either value is passed;
  URLs and query strings are replaced with `[redacted-url]`.
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
- People backfill runs only through its scheduler job, either periodically or
  through the manual action. Service startup must not enqueue an event pass.
  Legacy manual HTTP routes resolve to the same scheduler job.
- People translation creates an execution only after it finds pending names or
  roles. Disabled AI, an empty sweep, and cache-only idle checks create no task.
- People translation runs through its configured periodic schedule or the same
  job's manual action. Each execution processes at most the first 1,000
  deduplicated translation groups; remaining pending groups stay in business
  state for later executions.
- People-name translation caches by `Person.ID`. Role translation caches by the
  owning metadata context: an episode uses its season `ParentID`; movie, series,
  and season credits use their own `MetadataID`. The remaining cache identity is
  the source text, target language, and prompt version, so equal roles in one
  season share one translation while different seasons remain isolated.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Unknown task definition key | Return 404; never use it as an unchecked filename |
| Manual library scan omits `library_id` or sends invalid JSON | Return 400; do not start the scheduler job |
| Manual library scan names a missing library | Return 404; do not start the scheduler job |
| Definition has no configurable periodic job | Reject the schedule update; do not persist settings |
| Schedule interval is below 60 seconds or above 30 days | Return 400; do not change persisted or live configuration |
| Schedule persistence fails | Keep the current live interval and enabled state |
| Schedule is disabled | Clear `next_run`; the manual task-definition action remains available |
| Account cleanup confirmation is canceled | Do not call the manual execution API |
| Invalid or unavailable `YYYY-MM-DD` date | Return 400; never resolve a client-provided path |
| Task execution insert fails | Do not start the background work |
| Watcher execution insert fails | Requeue every candidate path for a later debounce batch |
| Task log append fails | Continue work, log the application error |
| A provider error contains a URL or query token | Preserve the original business error for the caller, but write only the sanitized error to the task log |
| A batch update has old and new detail lines | Pass only the new lines to `TaskUpdate.Details` |
| A detail already starts with a supported marker | Preserve it; do not prefix another marker |
| A detail has no supported marker | Prefix `ℹ️`; never write a bracketed level label |
| A historical line has a structured level label | Hide that label in the UI and render the corresponding fallback badge without rewriting the file |
| Process exits with a media group running | Restore its rows to `pending` at startup |
| Any member loses a claim race | Roll back the whole group claim |
| Library media exists while catalog work is pending | Process one library work unit first |
| An episode role has no season parent | Fall back to the episode `MetadataID`; never merge unrelated orphan records |

### 5. Good / Base / Bad Cases

- Good: a two-episode series is claimed and completed as one unit, then the worker
  checks newly imported media before taking another catalog item.
- Good: selecting library A in the task center scans A's enabled roots only;
  the next timer-driven pass still scans enabled libraries A and B.
- Base: adding a disabled root saves it without starting an event scan.
- Bad: reuse the manual target for timer-driven runs, or fail a successful save
  because its follow-up scan task could not be created.
- Good: a scan change is written as `timestamp ➕ 新增 /media/a.strm`.
- Good: enabling a two-hour library scan persists `7200`, resets its live timer,
  and immediately returns a definition whose `schedule_config.enabled` is true.
- Good: two settled video paths produce one `library_watch` execution with two
  detail lines, while `library_scan` history remains empty.
- Good: reverse display places a manual or scheduled execution's final `🔺`
  above all execution details and its initial `🔻` below them.
- Good: an event execution writes its semantic details and terminal error, when
  present, without adding generic start or finish lines.
- Good: equal role source text in two episodes of the same season creates one
  translation group with two write-back targets.
- Base: no People work exists; scheduled passes stay silent, while a manual
  People backfill records the empty completed run.
- Base: a disabled periodic job reports no `next_run`; its manual action still runs.
- Base: an unmarked summary is written with `ℹ️`; historical labeled lines are
  displayed without their label and remain unchanged on disk.
- Base: equal role source text in different seasons remains two translation groups.
- Bad: select pending rows without a conditional update, or resume from an old
  task execution/log after restart.
- Bad: keep a second ticker inside a periodic worker, or update a setting without
  resetting the corresponding live scheduler timer.
- Bad: use each episode `MetadataID` as the role cache context and call AI once
  per episode for the same season role.
- Bad: emit a new `[INFO]`, `[DETAIL]`, or `[ERROR]` task-center line, depend on
  monochrome system emoji rendering, or remove bracketed text from message bodies.

### 6. Tests Required

- Task start/update/finish, pagination, and `running -> interrupted` recovery.
- Per-definition daily log rollover, same-day append order, shared-kind
  isolation, invalid date/key rejection, newest-first dates, and tail truncation.
- Lifecycle tests assert physical write order `🔻 ... details ... ❌ ... 🔺`
  for manual and scheduled executions, absence of `🔻`/`🔺` for event
  executions, supported-marker preservation, `ℹ️` fallback, and absence of all
  three legacy level labels.
- UI lint/build checks cover all marker variants and structured legacy-label
  fallback while retaining newest-first display, refresh, and tail truncation.
- Atomic whole-series claim, late-series-member exclusion, and
  `running -> pending` media recovery.
- API/UI contract plus manual, scheduled, and event trigger attribution.
- Library scan tests assert required target validation, manual single-library
  isolation, periodic all-library scope, and event task visibility for new
  libraries and newly added enabled roots.
- Schedule tests assert persistence, live countdown reset, disabled `next_run`,
  bounds rejection without mutation, and manual bypass behavior.
- Task-center checks assert account cleanup cancellation sends no request and
  confirmation sends exactly one request; other scheduler actions require no confirmation.
- Watcher tests assert one execution per debounce batch, semantic per-path details,
  partial-failure continuation, create-failure requeue, and scan/watch isolation.
- People backfill tests assert no startup/event worker remains, scheduled empty
  passes stay silent, and manual empty passes remain observable.
- People translation tests assert manual/scheduled trigger attribution, a
  1,000-group pass limit with the remainder left pending, same-season role reuse,
  and cross-season isolation without changing person-name caching.

### 7. Wrong vs Correct

```go
// Wrong: two instances can select and process the same pending work.
db.Where("scrape_status = ?", "pending").Find(&rows)

// Correct: conditionally update the complete group and roll back a partial claim.
db.Transaction(func(tx *gorm.DB) error {
	return claimPendingGroup(tx)
})
```

```go
// Wrong: every update writes the complete accumulated slice again.
task.Update(TaskUpdate{Details: result.Details})

// Correct: write only details created since the previous update.
task.Update(TaskUpdate{Details: result.Details[detailCount:]})
detailCount = len(result.Details)
```

```go
// Wrong: encode a legacy level or omit the semantic meaning.
task.Update(TaskUpdate{Details: []string{"[DETAIL] 新增 /media/a.strm"}})

// Correct: the producer supplies the accurate marker; the tracker owns layout.
task.Update(TaskUpdate{Details: []string{"➕ 新增 /media/a.strm"}})
```

```go
// Wrong: TaskKindPeople merges two administrator-visible tasks.
logs.append(task.Kind, level, message)

// Correct: resolve the stable definition key with the task-definition filters.
logs.append(taskDefinitionKeyForTask(task), level, message)
```

```go
// Wrong: every episode translates the same season role independently.
contextKey := role.MetadataID

// Correct: episode roles share the season context.
contextKey := role.MetadataID
if role.Metadata.Kind == model.MetadataKindEpisode && role.Metadata.ParentID != nil {
	contextKey = *role.Metadata.ParentID
}
```

```go
// Wrong: persist a new interval but leave the running timer unchanged.
repo.Setting.Set(ctx, intervalKey, value)

// Correct: validate and persist first, then mutate the live job and signal reset.
if err := scheduler.UpdateSchedule(ctx, jobName, enabled, intervalSeconds); err != nil {
	return err
}
```

```go
// Wrong: a manual RunNow execution is persisted as scheduled.
task := tasks.StartTriggered(kind, TaskTriggerScheduled, name, update)

// Correct: the scheduler context owns trigger attribution for the shared job.
task := tasks.StartTriggered(kind, schedulerTaskTrigger(ctx), name, update)
```

```go
// Wrong: a task-center manual scan silently walks every library.
scheduler.RunNowAsync(ctx, "library_scan")

// Correct: the manual target is scoped to this detached run; timers stay global.
scheduler.RunLibraryScanNowAsync(ctx, libraryID)
```
