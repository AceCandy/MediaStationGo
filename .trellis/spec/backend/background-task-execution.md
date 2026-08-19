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
- `GET /api/tasks/definitions/:key/executions` returns paginated execution
  history for one validated task definition.
- `GET /api/tasks/definitions/:key/log?date=YYYY-MM-DD&tail_bytes=` returns
  `date`, newest-first `dates`, `content`, and `truncated`. Omitting `date`
  selects the newest available date.
- Task logs live at
  `<data_dir>/task-logs/YYYY-MM-DD/<task-definition-key>.log`.
- Media scrape states include `pending`, `running`, `matched`, `no_match`, and `error`.

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
- New task-center log lines never contain `[INFO]`, `[DETAIL]`, or `[ERROR]`.
  The tracker writes `🔻` first for start, `🔄` for progress, `❌` for terminal
  errors, and `🔺` last for finish. Because the UI reverses lines, `🔺` is the
  execution block's top boundary and renders as `▼`; `🔻` is its bottom boundary
  and renders as `▲`.
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
- People translation creates an execution only after it finds pending names or
  roles. Disabled AI, an empty sweep, and cache-only idle checks create no task.
- People-name translation caches by `Person.ID`. Role translation caches by the
  owning metadata context: an episode uses its season `ParentID`; movie, series,
  and season credits use their own `MetadataID`. The remaining cache identity is
  the source text, target language, and prompt version, so equal roles in one
  season share one translation while different seasons remain isolated.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Unknown task definition key | Return 404; never use it as an unchecked filename |
| Invalid or unavailable `YYYY-MM-DD` date | Return 400; never resolve a client-provided path |
| Task execution insert fails | Do not start the background work |
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
- Good: a scan change is written as `timestamp ➕ 新增 /media/a.strm`.
- Good: reverse display places the final `🔺` above all execution details and
  the initial `🔻` below them.
- Good: equal role source text in two episodes of the same season creates one
  translation group with two write-back targets.
- Base: no work exists; the worker waits for a wake signal.
- Base: an unmarked summary is written with `ℹ️`; historical labeled lines are
  displayed without their label and remain unchanged on disk.
- Base: equal role source text in different seasons remains two translation groups.
- Bad: select pending rows without a conditional update, or resume from an old
  task execution/log after restart.
- Bad: use each episode `MetadataID` as the role cache context and call AI once
  per episode for the same season role.
- Bad: emit a new `[INFO]`, `[DETAIL]`, or `[ERROR]` task-center line, depend on
  monochrome system emoji rendering, or remove bracketed text from message bodies.

### 6. Tests Required

- Task start/update/finish, pagination, and `running -> interrupted` recovery.
- Per-definition daily log rollover, same-day append order, shared-kind
  isolation, invalid date/key rejection, newest-first dates, and tail truncation.
- Lifecycle tests assert physical write order `🔻 ... details ... ❌ ... 🔺`,
  supported-marker preservation, `ℹ️` fallback, and absence of all three legacy
  level labels.
- UI lint/build checks cover all marker variants and structured legacy-label
  fallback while retaining newest-first display, refresh, and tail truncation.
- Atomic whole-series claim, late-series-member exclusion, and
  `running -> pending` media recovery.
- API/UI contract plus manual, scheduled, and event trigger attribution.
- People translation grouping asserts same-season role reuse and cross-season
  isolation without changing person-name caching.

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
