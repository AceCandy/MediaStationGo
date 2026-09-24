# Background Task Execution Contract

## Scenario: Startup Readiness and Directory Registration

### 1. Scope / Trigger

Applies to asynchronous `Container.Boot`, watcher initialization, and task-center
execution/configuration. HTTP listening does not imply that tasks are ready.

### 2. Signatures

- Administrator-only `GET /api/tasks/startup` returns `StartupStatus` with
  `Cache-Control: no-store`.
- `Container.StartupStatus()` reads an independent in-memory snapshot; it never
  queries task history or takes the watcher's traversal lock.

### 3. Contracts

- Fields: `state` (`starting`, `ready`, `failed`), fixed-label `stage`, integer
  `elapsed_seconds`, `stage_elapsed_seconds`, `directories_found`,
  `directories_watched`, and safe `warnings` (always an array).
- Register/start the scheduler last, after all synchronous initialization.
  `ready` does not wait for asynchronous backfill tasks to finish. Task recovery
  and path normalization errors prevent readiness; existing recoverable worker
  errors produce warnings. Log each initialization stage's duration.
- Production containers always initialize `StartupState`. Explicit standalone
  containers without it retain their ready behavior. `Close` cancels and joins
  Boot before releasing services; canceled Boot must not report ready.
- Watcher discovery uses `filepath.WalkDir` without explicit ordinary-file
  `Info` calls. Preserve hidden-directory exclusion, explicit hidden/symlink-root
  registration, failed-root recovery and cancellation. Do not change scanner
  fingerprints or file-stat behavior. Report bounded directory counters.
- The retired HongGuo pending-directory migration never runs at startup. Existing
  downloads keep persisted placements; new download directory generation stays
  unchanged. Runtime settings load once during service construction.
- The task page polls readiness independently of history, without overlapping
  requests and with unmount cancellation. Unknown, failed or unavailable status
  hides execution/configuration controls and labels idle tasks as not ready.
  Loss of readiness closes action dialogs without reopening them on recovery;
  historical logs remain accessible.

### 4. Validation & Error Matrix

- Anonymous/non-admin status request: existing administrator middleware rejects
  it with 401/403. No path, credential or raw error is exposed in the snapshot.
- Known definition run/schedule or shared scheduler trigger before readiness:
  HTTP 503 with `code=startup_not_ready`, without executing or saving anything.
- Unknown task definition: retain 404 even during initialization.

### 5. Good / Base / Bad Cases

Good: blocked history still allows directory-progress updates. Base: successful
startup removes its warning-free banner and enables actions. Bad: expose a run
button merely because definitions loaded, or treat a failed status read as ready.

### 6. Tests Required

`TestStartupProgressDuringBlockedStep`, `TestWatchDirectoryTraversal`,
`TestBootRegistersSchedulerLast` (including shutdown),
`TestBootCanceledDoesNotRegisterScheduler`, and
`TestTaskStartupGuardsAndLightweightStatus` cover progress isolation, safe
snapshots, directory coverage, ordering/cancellation and 503/404 boundaries.
Run watcher regressions with isolated PostgreSQL and `-race`.
`web/scripts/check-task-startup.mjs` covers slow history, hidden controls,
readiness loss/recovery, stale dialogs, responsive themes and request cleanup;
keep `check-task-log.mjs` passing with explicit readiness in its mocks.

### 7. Wrong vs Correct

Wrong: start the scheduler early to make buttons work, or add startup status to
the expensive task-history query. Correct: keep scheduler-last ordering and
gate actions on the independently readable startup snapshot.

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
- Claim indexes are `idx_media_scrape_pending_pick` (movie-first ordering),
  `idx_media_scrape_group` (trimmed group identity), and
  `idx_media_scrape_running` (active ordinary-source rows).
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
- TMDb first-download work shares `runTMDbArtworkLocalRepair` and its mutex with
  manual/scheduled repair. Display `TMDb 图片下载与修复`, retaining execution name
  `TMDb 图片本地化修复`, definition key and scheduler settings for log/history mapping.
  Event runs consume only durable catalog image due work; manual/scheduled runs
  additionally scan existing selections. The configurable schedule controls only
  full repair scans, not initial downloads. The service-owned worker restores due
  work at startup and cancels/joins at shutdown; it never reads history as a queue.
  See the asynchronous handoff contract in `shared-media-metadata.md`.
- TMDb artwork repair jobs append sanitized details only for an actionable image
  type: missing file, repair, no-image result, concurrent skip, or failure.
  Normal local files contribute only to summary metrics. Action details contain
  title, metadata kind, TMDb ID, image type, action, and result; the jobs use
  artwork business state and in-memory keyset pagination and never derive work
  from task history or catalog hydration jobs.
- TMDb missing-artwork recheck scans only Movie/Series metadata with direct media or playable
  Episode descendants. Metadata without media is outside this task, and the
  same execution continues by metadata ID until all current candidates are scanned.
- TMDb Episode metadata recheck consumes persistent due jobs for Episodes with
  direct media and missing overview, release date, or still. Successful checks
  use the business checkpoint for a season-air-date cooldown; ordinary executions do not
  rerun the global candidate query. See the persistent queue contract below.
- The same job handles Seasons with direct media or playable Episode
  descendants and missing fields, poster, own TMDb ID or snapshot. Seasons use
  `tmdb_season_checked_at` for their own cooldown under the same season-air-date policy. Display the definition
  as `TMDb 季/集信息补全/复查`, preserving its old key, settings and execution-name
  filter so historical executions and daily logs remain attached.
- Season/Episode titles never trigger recheck or count as remaining gaps. A
  request triggered by another gap may still update the title from TMDb.
- Season/Episode recheck workers process at most three Seasons concurrently, with
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
  the batch failed. Retry uses the existing pending map: 30-second exponential
  backoff capped at five minutes, at most five retries. A newer event wins its
  deadline/attempt count while retaining sidecar/directory intent. After the
  bound, the next filesystem event or library scan is required; history is not
  a durable event queue.
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
- One update must not place the same text in both `Message` and `Details`:
  `Message` is the progress line and `Details` are separate operator records.
  A notice with no separate detail passes `Message` only; regression tests count
  the resulting daily-log line exactly once.
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
- Claim reads one ordered seed (`LIMIT 1`), then its complete pending group.
  Both reads exclude running groups, including NULL/space-padded identities;
  a running claim may commit between the reads. The final conditional UPDATE
  rechecks ordinary source and pending status; partial success rolls back.
  Cancellation returns only `group.MediaIDs`, never a newly inferred group.
- Explicit rescrape resets the selected business objects to `pending` and wakes
  the worker. Catalog checkpoints remain the catalog recovery authority.
- Manual `media_scrape` resets only NULL/empty, `pending`, `error`, and
  `no_match` rows in the selected supported libraries. It clears old errors,
  sets trigger `manual`, preserves `matched`/`running`, and reuses the existing
  worker. All-library scope loops through the same per-library operation and
  skips music, adult, and unknown types.
- The manual library reset wakes media workers only when affected rows are
  nonzero; it does not directly wake catalog work. Its response `count` counts
  files, not grouped executions. The Web action displays this count and reports
  zero work explicitly, without creating an empty execution/log.
- Catalog work is displayed as `作品资料补全` for discover and ingested works.
  Preserve `catalog_scrape` and the `发现目录刮削：` execution-name filter for
  history/log compatibility. Catalog executions enter `waiting` before taking
  the shared write lock, enter `scrape` after acquiring it, and restore active
  progress after yielding to media. Both desktop and mobile task status render
  the current execution's waiting message rather than a generic running label.
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
- Good: a library scan keeps aggregate metrics and writes `timestamp ❌ /media/a.strm: permission denied`; successful per-file changes are not expanded.
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
- `TestClaimPendingGroup*` checks bounded reads and all three actual PostgreSQL
  index plans over 10,000 pending files, NULL/space identities, source isolation,
  three independent-connection claim races, between-read arrivals, source-change
  rollback and complete cancellation reset.
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
  registration, candidate filtering/keyset pagination, season-air-date cooldown, and
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

## Scenario: Global Library Scan Admission

### 1. Scope / Trigger

All manual, scheduled, automatic-root and STRM-refresh full scans in one
application instance. File-watcher single-file ingestion remains independent.

### 2. Signatures

`ScannerService.TryBeginLocalScan() (release func(), admitted bool)` reserves
one process-local slot. Call exactly one release for every successful admission.
`ErrLocalScanAlreadyRunning` is the shared busy error.

### 3. Contracts

Reserve before creating an execution or launching a goroutine. Reject busy
requests immediately; do not queue or wait. Scheduler `beginRun` reserves the
same slot, and `runReserved` releases it even on failure/cancellation. Hold the
slot across an entire multi-library/root batch, not once per target. Added-root
and STRM batches create/run their per-target executions sequentially, continuing
after an individual target failure. A task-create failure must not start
untracked scanning. New scan entry points must use this admission boundary.

### 4. Validation & Error Matrix

Busy direct scan/task/scheduler API -> HTTP 409 with a user-facing error and no
execution; direct scan responses set `queued=false`. Busy automatic follow-up
-> log rejection without undoing the successful library/root save. Busy STRM
follow-up -> `Queued=false` and a reason without undoing generated files.
Task creation failure -> release immediately. Periodic busy tick -> skip that
run, retaining its ordinary schedule, not a hidden retry queue.

### 5. Good / Base / Bad Cases

Good: one admitted batch scans every selected target in order. Base: a later
request succeeds after completion. Bad: globalizing the lock while acquiring
it separately for each spawned target, thereby dropping later targets.

### 6. Tests Required

`TestLocalScanAdmissionIsGlobal`, `TestSchedulerScanSharesGlobalAdmission`,
`TestSchedulerScanReleasesSlotOnShutdown`, and `TestScanAdmissionHTTPAndBatches`
cover admission, busy APIs/no executions, error/create-failure/shutdown release,
sequential roots and STRM continuation. Run the database cases on isolated
PostgreSQL and include `-race`. Path-level writer safety is still required for
watcher concurrency; see `database-guidelines.md`.

### 7. Wrong vs Correct

Wrong: maintain separate library/root keys or rely only on the scheduler's
per-job `running` flag. Correct: all scan admissions share the ScannerService
slot; a batch owns it until all targets and task finalization finish.

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
- `RemovePath(ctx, path)` returns access failures without deleting rows; its
  missing-file path also requires an accessible owning library root.
- Missing, malformed, or non-current `media_probe_metadata` is the durable
  backfill state; task executions and in-memory wake flags are not checkpoints.

### 3. Contracts

- A library scan discovers and persists media only. It never calls ffprobe or
  enqueues per-file probe work in a hidden queue.
- Enabled roots are scanned serially in configured order. Each root flushes its
  pending writes before pruning missing media. A failed root is not pruned and
  does not stop later roots. `walk` propagates directory/file-info errors;
  whole-library scans return the root error even after another root succeeds.
- Directory events reconcile only that subtree, registering watches before
  reading files. Watcher errors coalesce root recovery; missing-file checks use
  200-row keyset pages. Failed refreshes preserve only the affected roots'
  watches, not disabled libraries. Stale events cannot revive a disabled library.
- Ordinary-library NFO events refresh stored local hints even for unchanged
  media. They retain confirmed identity and probe documents; unbound media
  return to pending with current hints. Running scrapes defer the refresh to
  retry. Invalid NFO retains accepted hints. NFO/HongGuo catalogs keep their
  own ingestion paths. Full scans still skip unchanged ordinary media without
  rereading sidecars; ordinary image-only event refresh is outside this contract.
- Root start, bounded running progress, root finish, and root failure update the
  existing scan task. Library scan logs omit successful per-file changes and
  include sanitized path/error details on both completion and failure paths.
  Error details retain at most 20 rows and report the omitted count without
  changing aggregate metrics. Other consumers retain bounded change records.
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
| Watcher stat fails with permission/I/O error, or the owning root is missing | Preserve media and retry; do not interpret the failure as a confirmed deletion |
| NFO changes during an active scrape | Preserve hints and identity until bounded retry; do not silently consume the update |
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
- `TestScanWalk*`, `TestScanUnreadable*`, `TestRemovePath*` and `TestWatcher*`
  cover traversal/access errors, offline roots, moved/deleted directories,
  coalesced recovery, retry bounds, disabled-library protection and NFO refresh
  without identity/probe invalidation. Run with PostgreSQL and include `-race`.
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

## Scenario: Persistent TMDb Season/Episode Recheck Queue

### 1. Scope / Trigger

Applies only to `tmdb_episode_metadata_recheck`, not People, Douban or other artwork tasks.

### 2. Signatures

- `tm_db_recheck_jobs`: one row per metadata ID, status, due time, attempts and private lease token/deadline.
- `tm_db_recheck_changes`: persistent revisions, pending flag and descendant cursor.
- `tm_db_recheck_asset_changes` and `tm_db_recheck_scans`: bounded asset expansion and file reconciliation checkpoints.
- `TMDbRecheckPassBoundary(ctx)` returns a database timestamp after local reconciliation. `ListTMDbRecheckSeasonOrder(ctx, cutoff)` returns ordered due-season IDs; `ClaimTMDbRecheckSeasonByID(ctx, id, cutoff)` conditionally leases one of them. `ClaimTMDbRecheckSeasonPage(ctx, lease, cutoff)` leases up to 200 targets. `CountTMDbRechecksDue(ctx, cutoff)` counts queue rows only, including outstanding leases. The original global claim remains a compatibility path.
- `CommitTMDbRecheck(ctx, job, snapshot, save)` serializes its short result transaction with `pg_advisory_xact_lock(hashtextextended(series_id, 0))` before taking row locks.
- `tm_db_recheck_season_leases` stores Season metadata ID, token and expiry; registered through `AllModels`. The partial target-token index supports batch renew/release. `RecordTMDbInventoryMissing` registers file-backed inventory gaps after ingestion without taking over an active target lease.
- Admin-only `GET /api/tasks/definitions/tmdb_episode_metadata_recheck/pending?status=&keyword=&page=&page_size=` returns `items`, `counts`, `changes`, `total`, `page`, `page_size`.
- The same route accepts `view=summary` for global `counts`/`changes` only, or `view=items` for paginated rows and the current filter's exact `total` without global counts/change queries (`counts={}`, `changes=0`). Omitted `view` keeps the complete legacy response; unknown views return 400. Summary ignores status/keyword and never hydrates list rows.
- Admin-only `GET /api/tasks/definitions/tmdb_episode_metadata_recheck/pending/:metadataID/files?page=&page_size=` returns `items` (`media_id`, local `path`, `library_id`, `can_preview`), `page`, `has_more`; only current `not_found` Season/Episode jobs qualify.

### 3. Contracts

- PostgreSQL triggers register media binding/deletion and relevant metadata, TMDb identity/snapshot and selected-artwork changes in the business transaction. No network or descendant scans in triggers. Snapshot payload-only updates do not register or convert JSON.
- Parent/asset expansion and initial file reconciliation process at most 200 source rows per transaction. Persist cursors even if no candidates result. Reconciliation repeats at most every seven days after a completed pass; its total work still scales with files.
- Build the distinct due-season order once per fixed-cutoff pass, sorting by cooldown (1/3/10/20 days), earliest due time and metadata ID. Do not repeat whole-queue date aggregation/sorting on each claim. Three workers consume that order and conditionally upsert Season leases; current due/target-lease checks precede acquisition, and pages retain `FOR UPDATE SKIP LOCKED`. Same-Season selection materializes target IDs before primary-key queue probes. Renew the five-minute Season lease and unfinished page targets together. All provider/image work stays outside result transactions. Verify generic plans for ordered-pass preparation, ID-based acquisition, compatibility lookup and page claims. No schema or priority column is required; pass-order memory scales with due Seasons (date projections are discarded after classification).
- Each Season execution owns its response cache and shares the execution's HTTP counter. Successful responses and failures (including request timeouts) remain reusable until that Season finishes; another Season cannot evict them. Responses exceeding 16 MiB become a shared retryable error. Existing language supplementation/HTTP retry rules still apply, so one cache fill need not mean one HTTP request. Cancellation stops the Season and requeues only its unfinished leased targets for five minutes later without increasing attempts; expired leases recover naturally.
- Freeze the cutoff after local reconciliation so newly materialized jobs participate, but retries scheduled during provider work wait for a later execution. No new scheduler or perpetual retry loop. A season acquired by another execution can be skipped until a later pass; ordering never authorizes early target requests.
- Capture initial `total`; refresh `remaining` at pass start, at most every 30 seconds while collecting results, and at the end. Counts query only `tm_db_recheck_jobs.due_at <= cutoff`, without metadata/media membership joins. They include in-flight jobs and can lag between samples; on refresh failure log a sanitized warning and retain the last sample. A nonzero terminal remainder may represent outstanding leases, not lost jobs. The task table renders recheck-specific scanned/remaining/failed metrics; older executions without `remaining` keep their previous presentation.
- Accumulate worker-local `details_ms`, `image_ms`, `save_ms` and corresponding `_failed` counters. Save timing includes initial database state validation and conditional state/result commits; image timing includes selected-artwork download and local preparation. Error details identify the precise stage and elapsed time while preserving the original error chain until existing log sanitization. Provider 404/inventory absence, cancellation and snapshot contention do not increment stage-failure counters. These are summed worker durations, not wall-clock run duration.
- Result transactions first take a transaction-level advisory lock keyed by Series ID, then lock and validate the Season lease before metadata/change/target rows and validate target token, live lease and metadata/revision snapshot. Acquire the advisory lock before setting the transaction-local 100ms lock timeout: commits for different Seasons of the same Series wait for each other's short result transaction instead of being misclassified as changed, while different Series remain concurrent. Finish/retry also locks the Season lease, so an expired Season owner cannot write even while its target lease remains valid. Metadata/change lock contention from business writers still uses NOWAIT and yields; another episode's detail update must not dirty the common season revision.
- After a successful result save, consume only the current target's ordinary change registered by that same transaction: its revision remains incremented, but `pending` is cleared only when the revision advanced after the pre-save lock and `expand=false`. Preserve descendant expansion, failed-save rollback and external changes serialized before or after the result transaction; never clear parent/Series changes or suppress the registration triggers globally.
- `TMDbRecheckTiming.Cooldown(now)` uses the target's own Season, never the Series' newest Season. Combine local Episode dates with matching positive-ID/season-number TMDb Season snapshot dates; reuse current whole-Season response dates after a request without adding network calls. Preserve known local dates when upstream omits them. Use the latest already-aired Episode, falling back to the Season release date; any Episode within the past or next 30 UTC calendar days makes the Season recent. Recent uses 1 day; 31–365 days uses 10 days; older uses 20 days. Unknown/invalid dates and distant-future-only dates without an aired fallback use 3 days. Boundaries use UTC dates, not month arithmetic. Success checkpoints enforce this policy even on manual maintenance runs. Complete/no-file targets have no due time; incomplete success schedules the computed cooldown. Existing due timestamps are not bulk reset: apply the policy when they next become due or receive a new result. Ordinary failure backoff (five minutes to 24 hours) and invalid-identity blocking (seven days or until change) are unchanged.
- Automatic Season recheck identifies upstream data by the unique positive Series TMDb ID plus `season_num`; the stored Season TMDb ID is replaceable provider state, not a request key. Require the returned `season_number` to match and the returned child ID/raw JSON to be valid, then replace the Season identifier and provider snapshot together. Keep `(provider, entity_kind, external_id)` uniqueness: an ID owned by another metadata item fails without changing either identifier or snapshot. Episode coordinate lookup and manual child-identity refresh retain their existing behavior.
- A typed detail HTTP 404 or a valid Season inventory without the target Episode becomes `not_found`, increments the separate metric and uses the same season-air-date cooldown. Describe inventory absence separately from 404 and log the actual delay. Image/download/save failures remain ordinary retries. Commit under the same snapshot/token protection as successful results; never classify historical rows by parsing logs. Detail logs include Series title, S/E coordinates and Series TMDb ID.
- Ingestion returns a validated inventory map (empty is valid; missing/null list is not), retains placeholders and Media attachments for unlisted Episodes, and registers their shared metadata issue under current identity/revision and Season snapshot checks. New inventory issues use `TMDbRecheckTiming.Cooldown`; repeat registration preserves the same identity's existing due time. Local field completeness alone must not erase an inventory issue; successful upstream revalidation clears it. No valid inventory means no inventory-mismatch classification.
- Private `not_found_identity` records request identity. Merging identity changes or no-file state wakes local validation; ordinary metadata/artwork events preserve cooling, even for locally complete metadata. The worker also checks identity directly before honoring the old marker. Business success cooldown remains authoritative.
- Explicit Season/Episode refresh, including each existing Episode successfully saved from a Season response, clears prior not-found evidence after its snapshot is saved. `ConfirmTMDbRecheckFound` updates only the existing primary-key job, resets it to ordinary pending work and invalidates any old target lease; missing fields do not imply upstream absence. Failed or unlisted targets retain their state. Ordinary local edits and historical snapshots are not proof of upstream existence. Completion remains the existing worker's responsibility.
- The only write action exposed by the pending panel is explicit STRM local-target cleanup through the existing admin preview/delete endpoints and `STRMDeleteDialog` (see `strm-target.md`). Select a concrete version from an on-demand paginated file list; Season scope includes own files and direct Episode files. Never pass metadata IDs as media IDs or expose cached target URLs. No automatic deletion, renumbering, direct-video deletion or bulk Season/404 deletion; keep STRM/database records. Parent removal defaults off and requires explicit confirmation of all contents.
- Events never directly start network work. Existing default-off schedule and manual task controls remain authoritative. No new perpetual worker/timer.
- The Web pending dialog opens from the button beside the Season/Episode task name. The `上游未收录 / 待核对` category includes inventory absence and 404, sharing the scrape-issues card, Select and icon-button styling. Switching categories or submitting a keyword resets pagination; closing aborts requests and stale responses cannot replace the current view. Pages join titles and sort globally by Series title, Season/Episode numbers and metadata ID before pagination. Load items and summary independently: paging/filtering requests only items, while opening/explicit refresh reloads summary. Summary loading/failure never blocks rows; its exact counts describe its last load, whereas each page computes its own current filtered total. Never return lease credentials or raw provider errors; row reasons are fixed safe messages and task errors use `sanitizeTaskLogError`.
- `tmdbRecheckCountsSQL` separates Episode and Season scopes with UNION ALL. Episode existence can use a bulk semi-join; the Season subquery joins jobs before OFFSET 0, preventing file checks for Seasons absent from the queue. Preserve that boundary and verify actual PostgreSQL plans, not merely SQL text or index presence. Counts remain proportional to the selected scope; no constant-time guarantee or stale server cache.
- Pending lists, keyword results and status counts include only Season/Episode jobs with associated media, using the same scope as the file list: an Episode's own media, or a Season's own media and direct Episode media. Apply existence filtering before pagination without multiplying jobs by file versions. After scanner/watcher removal of the last media row, the next read hides that job even before change expansion or a task run. GET requests never inspect disk or mutate jobs, leases or cooldowns; restoring media restores visibility. Local-target cleanup that retains STRM/media does not remove the job from the list.

### 4. Validation & Error Matrix

- Anonymous / non-admin: 401 / 403. Unknown definition: 404. Invalid status, page outside 1..1000000 or page size outside 1..100: 400.
- Lost lease, changed snapshot or contended result locks: no callback writes; retain/requeue for a later task run.
- Same-Series result commits wait on the Series advisory lock and then validate fresh row state; only external row contention or an actually changed snapshot is requeued as changed.
- A successful result triggers an ordinary target change: keep its new revision but clear its pending flag. If the trigger requests descendant expansion, the save fails, or an external transaction registers a later change, retain pending work.
- Interrupted process: outstanding claims recover after expiry without startup resetting active leases.
- A job due after the fixed cutoff is excluded even if wall-clock time has passed its due time; the next pass can claim it. Deleted targets use their own ID as a temporary lease key for cleanup. A held target lock is skipped; the rest of its Season can proceed.
- A stale, nonnumeric or missing Season TMDb ID does not invalidate an otherwise valid coordinate response. A mismatched returned Season number, nonpositive returned ID, invalid raw JSON or child-identifier uniqueness conflict rejects the save and preserves the prior persisted state.
- File-list bounds match the pending-list bounds; non-404/missing targets return an empty page. Relative/non-local paths do not expose URLs or permit preview. `can_preview` only indicates local STRM syntax: the existing preview/delete service must resolve and validate the actual target each time.

### 5. Good / Base / Bad Cases

- Good: a media merge rolls back both graph mutations and change registration.
- Base: a future-due idle queue uses the due index before metadata/lease lookups; it never joins Media.
- Good: interleaved Episodes from more than 32 Seasons reuse whole-Season responses when claimed by Season. Bad: keep extending the pass cutoff or run media-membership counts for every progress update.
- Good: an Episode detail save consumes its own ordinary registration, while a Season identity save retains `expand=true` and a later external edit registers pending work again. Bad: disable triggers for the result transaction or unconditionally clear the target/parent change rows.
- Good: two workers saving different Seasons of one Series serialize only their result transactions. Bad: let both reach the shared Series `FOR UPDATE NOWAIT` lock and repeatedly reschedule unchanged Episodes.
- Bad: read task logs for retry state, clear another worker's lease, or update stale provider results after a user edit.
- Good: a 404 remains cooled through an ordinary overview edit but revalidates a changed Season number. Bad: infer that a file is wrong from 404 alone or delete all versions implicitly.
- Good: Series 42 Season 1 replaces a stale or nonnumeric Season child ID with the valid ID returned by `/tv/42/season/1`. Bad: reject the coordinate response only because it differs from the stored child ID, or take an ID already owned by another Season.

### 6. Tests Required

- `TestTMDbRecheck*`: transactional registration/merge rollback, distinct-connection SKIP LOCKED and commit contention, stale/expired tokens, empty-batch resume, asset/descendant batches and future-due index plan.
- `TestTMDbRecheckCommitConsumesOnlySelfRegistration` verifies failed-save rollback, ordinary self-registration consumption, later business-change preservation and descendant-expansion preservation on PostgreSQL.
- `TestTMDbRecheckSerializesCommitsForOneSeries` uses distinct PostgreSQL connections to prove same-Series commits wait and different-Series commits remain concurrent; `TestTMDbRecheckConcurrentConnections` continues to prove external row contention yields without executing the callback.
- `TestTMDbRecheckSeasonPagesAndRecovery` verifies distinct-connection Season mutual exclusion, 205-target paging, group renewal, expired-owner rejection and cancellation-style release. `TestTMDbRecheckSeasonSharesFailuresAndReleasesCancellation` covers shared HTTP/timeout failures and cancellation. `TestTMDbRecheckInventoryIssuePreservesCompleteTargetsAndLeases` and `TestSeriesInventory*` cover valid-empty/malformed inventories, active-lease protection, unchanged cooling, retained files and recovery.
- `TestTMDbRecheckPassGroupsSeasonsAndExcludesLaterRetries` checks Season grouping, fixed-cutoff counts and next-pass recovery. `TestTMDbRecheckSeasonClaimDoesNotScanOtherJobs` runs the production page and lookup SQL with generic prepared plans against 30,000 jobs and checks primary-key/due-index use. `TestTMDbRecheckPassReusesScatteredSeasons` verifies exactly 36 requests and zero remainder for 72 Episodes across 36 Seasons with complete Chinese text; `TestTMDbRecheckFailuresReportActualStage` and `TestTMDbRecheckStagePreservesClassificationAndRedaction` verify actual failure paths, retries, wrapped errors and redaction. `web/scripts/check-task-log.mjs` verifies Season count, metadata count and legacy progress rendering.
- `TestTMDbRecheckListTracksMediaDeletion`: last-version deletion, Season own/direct Episode files, restored media, keyword/status filters, page boundaries and matching counts on PostgreSQL, without running the worker or changing cooldown state.
- `TestTMDbRecheckListCountsAvoidPerEpisodeProbes` uses 30,000 Episodes plus unqueued Seasons to reject per-Episode/catalog probes, verifies missing/wrong-kind exclusion and proves items work when the summary change table is unavailable. `check-recheck-dialog.mjs` verifies independent summary loading/failure, no summary request on pagination/filtering, refresh retry and stale/unmount cancellation.
- `TestTMDbRecheckTimingCooldown` covers day/year boundaries, unknown/invalid dates and near/distant future dates. `TestTMDbRecheckSeasonTimingAndPriority` verifies same-Series Season isolation, matching snapshots, local Episode dates, order, lease exclusion and future-due exclusion. `TestTMDbRecheckAirdateCooldown` verifies all four delays for incomplete success, HTTP 404 and inventory absence, fresh-response dates, known local date preservation, delay logs and success-checkpoint guards on PostgreSQL. The 30,000-job plan test also exercises the new acquisition and one-time ordering SQL.
- `TestFetchTMDbMetadataRecheckUsesSeasonCoordinates`: stale/nonnumeric child IDs are ignored for lookup while wrong Season numbers, nonpositive IDs and invalid JSON remain rejected. `TestTMDbMetadataRecheckRepairsSeasonsAndEpisodes`: identifier replacement, uniqueness-conflict rollback, service persistence, retry backoff and checkpoint cooldown behavior on real PostgreSQL.
- `TestTMDbRecheckListAccessAndValidation`: real auth middleware, parameter rejection, pagination and no lease leakage. Web lint/build plus isolated panel interactions and responsive screenshots.
- `TestTMDbRecheckNotFoundCooldownAndIdentity`, `TestTMDbRecheckOtherFailuresRemainRetry`: real PostgreSQL and mock HTTP prove unknown-date 3-day scheduling, due reclaims, ordinary-change preservation, identity wake-up and stale-404 rejection. `TestTMDbRecheckFilesVersionsAndPagination` proves concrete versions, kind constraints and URL redaction; existing STRM safety tests plus `TestFileManagerRevalidatesSTRMTargetAfterPreview` cover temporary-file deletion and revalidation. Browser checks verify cancel sends no DELETE and explicit parent confirmation addresses only the chosen media ID.

### 7. Wrong vs Correct

Wrong: rerun `ListTMDbSeasonMetadataRecheckAfter` over the full catalog on every maintenance execution, reject an automatic Season coordinate response because its child ID differs from stale local state, disable transaction triggers while saving a recheck result, or globally serialize all Series to hide shared-row contention.

Correct: transactional change registration → bounded expansion → indexed due claim → Series-keyed result serialization → coordinate validation with atomic child-identity replacement → conditional result commit that consumes only its newly registered ordinary target change.

## Scenario: Task-Center Pending Search and Indicators

### 1. Scope / Trigger

Applies to the Season/Episode recheck and media-scrape issue dialogs shown from task names.

### 2. Signatures

- Recheck pending list accepts optional `keyword`; it matches item/Series titles, metadata ID and rendered S/E coordinates.
- `GET /api/media/scrape-issues` accepts optional `keyword`; it matches scan title, path, library name and stored scrape error.

### 3. Contracts

- Keyword search is server-side and combines with status/library filters before count and pagination. Escape LIKE metacharacters so input is literal.
- Search runs only on explicit form submission. Empty keyword retains the existing bounded fast path.
- Recheck keyword results use one materialized match set for exact total and page selection; do not repeat the metadata joins and LIKE predicates for each. A LEFT JOIN from the total preserves out-of-range page totals without fabricating an item. Matching still scales with eligible jobs; this is not an indexed substring-search guarantee.
- Task-center indicators refresh once on page entry and again after closing a pending dialog. Recheck requests use `view=summary`; media-scrape issues retain page size 1. They do not join the three-second task snapshot poll. Recheck badges exclude `done`; non-zero counts use the gold warning treatment and show a capped `999+` label.

### 4. Validation & Error Matrix

- Empty/whitespace keyword: same result and plan shape as no keyword. No match: HTTP 200 with empty items and zero filtered total.
- A failed indicator request leaves the neutral button available; it must not block task-center rendering.

### 5. Good / Base / Bad Cases

- Good: a title match on a later page is returned on page 1 of filtered results. Base: opening the task center requests the recheck summary and one scrape-issue row. Bad: filter only the currently rendered browser page, hydrate a sorted recheck page just for its badge, or run counts every three seconds.

### 6. Tests Required

- PostgreSQL repository tests cover recheck title, coordinate and no-match searches; service tests cover scrape issue library/path and no-match searches.
- Web checks cover keyword forwarding, task-name indicator styling, count capping, lint and build.

### 7. Wrong vs Correct

Wrong: debounce every keystroke into count-plus-page SQL or add pending counts to the frequent task snapshot.

Correct: submit keyword explicitly, keep recheck items and summary independent, and refresh indicators only at task-page/panel lifecycle boundaries.
