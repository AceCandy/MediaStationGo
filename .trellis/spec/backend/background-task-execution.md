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
- `POST /api/tasks/definitions/media_scrape/run` accepts exactly one target:
  `{ "library_id": "<library UUID>" }` or `{ "all_libraries": true }`.
- `GET /api/media/scrape-issues?library_id=&status=error,no_match&page=&page_size=`
  returns administrator-visible unresolved raw media; an empty `status` uses
  both supported states. `POST /api/media/:id/scrape` resets one item for retry.
- `GET /api/tasks/definitions/:key/executions` returns paginated execution
  history for one validated task definition.
- `GET /api/tasks/definitions/:key/log?date=YYYY-MM-DD&tail_bytes=` returns
  `date`, newest-first `dates`, `content`, and `truncated`. Omitting `date`
  selects the newest available date.
- Task logs live at
  `<data_dir>/task-logs/YYYY-MM-DD/<task-definition-key>.log`.
- Media scrape states include `pending`, `running`, `matched`, `no_match`, and `error`.
- Automatic scrape execution uses exactly three media workers and one serial
  catalog worker. `scrapeRunMu` is an `RWMutex`: automatic media groups hold a
  read lock, while manual, whole-library, catalog, and people work hold the
  write lock.
- Watcher batches use `TaskKindWatch`, `TaskTriggerEvent`, and the stable
  `library_watch` definition; they never reuse the full-scan kind.
- TMDb maintenance definitions `tmdb_artwork_local_repair`,
  `tmdb_artwork_missing_recheck`, and `tmdb_episode_metadata_recheck` use
  separate default-off 24-hour scheduler jobs, settings, histories, and daily logs.

### 3. Contracts

- `series_local_correction` (剧集本地资料纠正) is a separate definition/kind;
  `POST /api/tasks/definitions/series_local_correction/run` returns 202, 409
  when already running, or 503 when unavailable. It reuses administrator task
  authorization, history, daily logs and the generic action UI. No scheduler
  toggle or network provider is involved.
- Startup invokes `StartSeriesLocalCorrection(ctx, true)` independently of
  ingestion. The setting `internal.series_local_correction_version` skips a
  successful rules version; manual runs ignore it. Only an entirely successful
  pass writes the version. Failure/cancellation remains retryable, and the
  existing worker wait group joins the correction on shutdown.
- Correction progress includes total, processed, succeeded, failed, remaining,
  updated and skipped. Succeeded includes unchanged/concurrently skipped rows;
  messages distinguish actual corrections from skips. Counts reflect the live
  candidate set, with the final total reconciled after keyset exhaustion.
- Regression: `TestSeriesLocalCorrectionProtectsConcurrentEdits` asserts source
  and timestamp conflict protection. `TestSeriesLocalCorrectionTaskVersionAndRetry`
  asserts failed-version retry, completion gating, manual execution, duplicate
  rejection and per-definition logs. A stale snapshot must not overwrite a Web
  save; writing only after a source/timestamp comparison in SQL is required.

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
- TMDb artwork repair jobs append sanitized details only for an actionable image
  type: missing file, repair, no-image result, concurrent skip, or failure.
  Normal local files contribute only to summary metrics. Action details contain
  title, metadata kind, TMDb ID, image type, action, and result; the jobs use
  artwork business state and in-memory keyset pagination and never derive work
  from task history or catalog hydration jobs.
- TMDb missing-artwork recheck scans only metadata with direct media or playable
  Episode descendants. Metadata without media is outside this task, and the
  same execution continues by metadata ID until all current candidates are scanned.
- TMDb Episode metadata recheck scans only Episodes with direct media and a
  missing overview, missing release date, or missing
  still. It keyset-pages the full current candidate set without a persisted
  cursor; successful checks use the business checkpoint for a 72-hour cooldown.
- The same job independently scans Seasons with direct media or playable Episode
  descendants and missing fields, poster, own TMDb ID or snapshot. Seasons use
  `tmdb_season_checked_at` for their own 72-hour cooldown. Display the definition
  as `TMDb 季/集信息补全/复查`, preserving its old key, settings and execution-name
  filter so historical executions and daily logs remain attached.
- Season/Episode titles never trigger recheck or count as remaining gaps. A
  request triggered by another gap may still update the title from TMDb.
- Season/Episode recheck pages run at most three items concurrently, with
  worker-local counters and a single task-progress collector. Cancellation
  stops queued work and joins active workers; individual failures stay isolated.
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
- Each automatic worker atomically claims one complete movie or series by
  changing every currently pending member to `running`. At most three distinct
  groups run concurrently. The serial catalog worker must not claim a new job
  while any media is `pending` or `running`. Running series catalog work releases
  its write lock at season/episode boundaries until pending/running media clears,
  then resumes the same durable job. In-flight entity requests are not interrupted.
  Media execution records enter `waiting` before acquiring the read lock, so
  contention is visible in task logs. Cancellation restores pending media work.
- Each completed automatic media group emits one structured timing record with
  candidate generation, provider lookup, metadata persistence, artwork, TMDb
  extended details, and total milliseconds. It must not contain a media path,
  request URL, or API key.
- A newly arrived member of a running series stays pending until that series
  finishes; it must not be claimed concurrently by another process.
- Explicit rescrape resets the selected business objects to `pending` and wakes
  the worker. Catalog checkpoints remain the catalog recovery authority.
- Manual `media_scrape` resets only NULL/empty, `pending`, `error`, and
  `no_match` rows in the selected supported libraries. It clears old errors,
  sets trigger `manual`, preserves `matched`/`running`, and reuses the existing
  worker. All-library scope loops through the same per-library operation and
  skips music, adult, and unknown types.
- Scanner and watcher changes wake media scraping by default. The retired
  `scrape.auto_on_scan` setting has no runtime reader; organizer retains only
  its independent `organize.scrape_after` policy.
- Media scrape success details use one history definition and identify exactly
  one source: existing canonical metadata, a named network provider, or local
  NFO. Scanner direct binding emits the existing-metadata detail only on the
  first exact binding; an already matched rescan emits no duplicate execution.
- Scrape issue remediation queries `media.scrape_status` and sanitized
  `scrape_error`, never task logs. Network libraries may use provider manual
  matching for `no_match`; NFO-only libraries require sidecar repair and retry.
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
  roles without a negative cache. Disabled AI, an empty sweep, and cache-only
  idle checks create no task.
- People translation runs through its configured periodic schedule or the same
  job's manual action. Each execution processes at most the first 1,000
  deduplicated translation groups; remaining pending groups stay in business
  state for later executions.
- People-name translation caches by `Person.ID`. Role translation caches by the
  owning metadata context: an episode uses its season `ParentID`; movie, series,
  and season credits use their own `MetadataID`. The remaining cache identity is
  the source text, target language, and prompt version, so equal roles in one
  season share one translation while different seasons remain isolated.
- A successful AI request without a valid Chinese result saves an empty cache
  for that prompt version. Later sweeps skip it; request errors are not cached.

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
| Library media is `pending` or `running` while catalog work is pending | Keep the catalog job unclaimed until all active media work drains |
| An episode role has no season parent | Fall back to the episode `MetadataID`; never merge unrelated orphan records |
| Manual media scrape supplies both or neither target | Return 400 and queue nothing |
| Manual media scrape names a missing library | Return 404 and queue nothing |
| Manual media scrape names an unsupported type | Return 400; all-library mode skips it |
| Scrape issue status is omitted or empty | Query both `error` and `no_match` |
| Scrape issue status contains another value | Return 400 |

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
- Good: all-library media scrape loops through supported libraries while an
  existing matched item and a running group remain unchanged.
- Good: task details distinguish `命中已有元数据`, `网络刮削（TMDB）`, and
  `本地 NFO 入库` under the same media scrape definition.
- Base: an unresolved row remains recoverable from the scrape-issues endpoint
  even when it is excluded from `MediaView`.
- Bad: parse task log text to discover retry candidates or create a second
  scrape queue for manual execution.
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
- Three-worker overlap with a maximum concurrency of three; atomic whole-series
  claim, late-series-member exclusion, catalog deferral, worker cancellation,
  and `running -> pending` media recovery.
- Automatic scrape timing logs contain all six durations, keep every duration
  between zero and total, and omit paths, URLs, and API keys.
- API/UI contract plus manual, scheduled, and event trigger attribution.
- Media scrape action tests cover single/all scope, target exclusivity,
  supported-type filtering, and the NULL/empty/pending/error/no_match versus
  matched/running state matrix.
- Scrape issue tests cover status/library filters, pagination/count parity,
  empty status defaults, safe error redaction, and NFO-specific reasons.
- Media scrape detail tests cover existing metadata, each provider label, local
  NFO, no-match, and sanitized error output without duplicate matched rescans.
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
  cross-season isolation, and invalid-result negative caching without changing
  person-name caching.
- Episode metadata recheck tests cover default-off scheduling, definition
  registration, candidate filtering/keyset pagination, 72-hour cooldown, and
  non-empty TMDb field projection including release date.

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

```go
// Wrong: task history becomes retry state and matched rows are reset incidentally.
ids := parseFailedMediaIDsFromTaskLog(log)

// Correct: business state selects unfinished rows and the shared worker owns retry.
scraper.ResetLibraryScrape(ctx, libraryID, false)
```

## Scenario: Library Scan and Media Probe Backfill Separation

### 1. Scope / Trigger

Apply this contract when changing local library scans, watcher ingestion, scan
progress, complete probe-document invalidation, or automatic track backfill.

### 2. Signatures

- `ScanLibraryWithProgress(ctx, libraryID, ScanProgressFunc)` and
  `ScanLibraryRootWithProgress(ctx, libraryID, rootID, ScanProgressFunc)` expose
  `roots_total`, `roots_completed`, `visited`, `added`, `updated`, `skipped`,
  `removed`, and `errors` through task metrics.
- `MediaProbeService.StartBackfill(trigger, name, sourcePath, libraryID, limit)`
  starts the shared visible probe execution.
- `MediaProbeService.WakeBackfill()` requests a coalesced automatic pass.
- Missing, malformed, or non-current `media_probe_metadata` is the durable
  backfill state; task executions and in-memory wake flags are not checkpoints.

### 3. Contracts

- A library scan discovers and persists media only. It never calls ffprobe or
  enqueues per-file probe work in a hidden queue.
- Enabled roots are scanned serially in configured order. Each root flushes its
  pending writes before pruning missing media. A failed root is not pruned and
  does not stop later roots.
- Root start, bounded running progress, root finish, and root failure update the
  existing scan task. Per-file change details retain at most 200 rows and add an
  omitted-count summary without changing aggregate metrics.
- A changed local file deletes its previous complete probe document before the
  media fingerprint update. If deletion fails, that media update is rejected so
  stale tracks are never presented as current.
- Scan, watcher, STRM refresh, and organizer batches request probe backfill only
  after their ingestion work settles and only when at least one media row was
  added or updated. Partial-success error paths still request backfill.
- Automatic and manual backfill share `TaskKindProbe` and the same executor.
  Automatic executions use trigger `event`; a wake received while probe work is
  active is coalesced and checked again after the active execution settles.
- Automatic probe checks, counts, and pages exclude episodic libraries
  (`tv`, `anime`, `variety`, `show`, `shows`, `nfo_tv`), scanned episode numbers,
  and series/season/episode metadata. Manual backfill still includes these rows.
- Startup may wake the coordinator, but the database query decides whether work
  exists. Valid current documents are never probed again. ISO images and STRM
  rows without a supported local or HTTP(S) target are skipped without invoking
  ffprobe or consuming a positive probe-attempt limit.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Root cannot be resolved or walked completely | Record a path failure, flush successful writes, preserve existing rows for that root, continue later roots |
| Probe document invalidation fails | Record a scan error and do not update that media row |
| Scan partially persists media and then returns an error | Finish the scan error path and still wake probe backfill for the persisted additions/updates |
| Another probe task is active | Manual start returns `ErrMediaProbeBackfillRunning`; automatic wake remains pending without parallel work |
| Automatic wake finds no missing/outdated document | Create no probe execution |
| ffprobe fails | Record one bounded failure detail; leave database state eligible for a later explicit wake |
| STRM has no supported target | Count it as skipped; do not call ffprobe and do not consume `limit` |

### 5. Good / Base / Bad Cases

- Good: two roots scan in order; root A writes are visible before root A prune,
  then root B starts, and one event probe task handles the resulting missing rows.
- Base: all media have current documents; startup or scan wakes settle without a
  probe task.
- Bad: starting an untracked goroutine per media file, pruning an incompletely
  walked root, or using task history as the probe retry queue.

### 6. Tests Required

- Assert serial root start/finish order, write visibility at root finish, and no
  prune after a failed root.
- Assert file-size/mtime changes remove the old probe document while unchanged
  files retain it.
- Assert automatic execution is visible as one event task, duplicate/running
  wakes coalesce, and manual/automatic executions cannot overlap.
- Assert scan details are capped with an omitted count while final metrics stay
  exact, including `skipped` and `errors`.
- Assert unsupported STRM rows are skipped without a probe call or limit use.

### 7. Wrong vs Correct

```go
// Wrong: scan completion depends on an invisible per-file ffprobe queue.
queueLocalMediaProbe(path)

// Correct: persist scan results, finish the batch, then request shared work.
scanner.WakeProbeBackfill()
```

```go
// Wrong: prune after an incomplete walk and delete media that may still exist.
pruneMissingMediaForRoot(seen)

// Correct: flush first and prune only after a successful complete walk.
writeBatch.Flush()
if walkErr == nil {
	pruneMissingMediaForRoot(seen)
}
```
