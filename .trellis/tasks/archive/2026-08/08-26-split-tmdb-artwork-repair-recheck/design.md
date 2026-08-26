# Technical Design

## 1. Boundaries

This change replaces one mixed scheduler pass with two ordinary `ScraperService` jobs. It does not add a queue, goroutine worker, endpoint shape, or frontend-specific rendering path.

```text
Task 1: scheduler -> selection keyset scan -> local file check
                  -> old URL download
                  -> only HTTP 404: TMDb image lookup -> conditional repair / no-image state

Task 2: scheduler -> metadata keyset scan -> per-type cooldown
                  -> one TMDb detail request per metadata -> insert-if-absent / conditional repair / no-image state
```

Both jobs reuse:

- `SchedulerService` for timer/manual execution and per-job concurrency.
- `TaskTrackerService` for execution summaries and daily definition logs.
- the existing `TMDbProvider` detail methods for Movie, Series, Season, and Episode.
- `ImageProxy` plus `ArtworkStore` for validated local persistence.
- `ArtworkRepository` for keyset reads and transactional selection writes.

The task-center Web page already renders server-provided definitions generically. No Web code or API payload change is required.

## 2. Persistent State

Add `model.MetadataArtworkRecheck` using `PermanentBase`:

| Field | Contract |
| --- | --- |
| `MetadataID` | required FK to `metadata_items`, cascade on metadata deletion |
| `ArtworkType` | required `poster`, `backdrop`, or `still` |
| `LastNoImageAt` | UTC time of the last successful TMDb response that had no image for this type |

Use a unique index on `(metadata_id, artwork_type)` and an index on `last_no_image_at`. Register the model in `model.AllModels()`.

The row means only “TMDb successfully answered and this type was absent”. Provider/network/download failures never upsert it. A successful image repair deletes it. Historical rows without this state use `MetadataItem.CatalogArtworkHydratedAt` as the initial cooldown baseline, so no data backfill is needed.

Metadata graph merge must merge these rows before deleting the source metadata. For a duplicate `(metadata_id, artwork_type)`, retain the later `LastNoImageAt`; otherwise move the row to the target.

Task history and log files remain observability only. Each execution starts from
an empty in-memory keyset position and pages until the current candidate set is
exhausted; no setting owns cross-execution sweep continuation.

## 3. Repository Contracts

Keep the new queries and state writes on `ArtworkRepository`; do not create another repository container member.

### 3.1 Local repair candidates

`ListTMDbArtworkSelectionsAfter(ctx, afterSelectionID, limit)` returns selection ID, metadata identity/display fields, artwork type, source URL, asset ID/storage key, and the entity's TMDb ID. It must:

- keyset by `metadata_artworks.id` ascending;
- require `source_provider='tmdb'` and a non-empty source URL;
- preserve one row per selection even if historical duplicate identifiers exist;
- avoid `media`, candidates, and catalog jobs.

The service validates HTTP(S), resolves the managed path, and calls `localArtworkFileAvailable`. A permission/path/non-regular-file error is a per-item retryable failure; only `os.ErrNotExist` enters repair.

### 3.2 Missing-image recheck candidates

`ListTMDbArtworkRecheckMetadataAfter(ctx, afterMetadataID, limit)` returns one metadata row with the context needed for its entity-owned types, current selection/asset data, per-type `LastNoImageAt`, and the Series TMDb ID needed by Season/Episode endpoints. It must:

- keyset by `metadata_items.id` ascending;
- require valid TMDb identity plus either `catalog_artwork_hydrated_at IS NOT NULL` or an existing per-type recheck row;
- exclude `catalog_artwork_hydrated_at IS NULL` rows when they also have no recheck state, preserving the first-scrape boundary;
- exclude unsupported kinds and metadata without a directly linked playable file or a playable Episode descendant;
- read `media` only for this eligibility filter; it never owns image state or provider identity;
- include a required type when its valid selection-to-asset join is absent, or when a recheck row exists for a TMDb/dangling selection;
- keep Movie/Series poster and backdrop on one candidate so one detail request serves both.

For a type without a state row, cooldown uses `catalog_artwork_hydrated_at`; otherwise it uses `last_no_image_at`. A state row written by task 1 is therefore sufficient handoff evidence even when the legacy entity checkpoint is nil. The repository returns candidates regardless of whether the timestamp is due so the service can emit explicit cooldown-skip details.

### 3.3 State and race-safe writes

Add small repository methods to upsert/delete `MetadataArtworkRecheck`.

Add a conditional repair write for an existing dangling TMDb selection. It inserts/deduplicates the downloaded asset, then updates `metadata_artworks` only when the original selection ID, asset ID, provider, and source URL still match the scanned snapshot. Zero updated rows mean a concurrent selection won; the downloaded asset may remain unselected, matching existing catalog race behavior.

For a type with no selection, reuse `ArtworkStore.importCatalogRemote`/`SaveCatalogSelection`, whose insert-if-absent behavior already preserves manual/local races. Do not call overwrite-oriented `SaveSelection` from either automatic task.

## 4. Remote Download Error Contract

Replace the string-only HTTP status error in `ImageProxy.fetchRemoteImageOnce` with a minimal typed error containing only `StatusCode`. Its error text contains status, not URL or query. Add an `errors.As` helper used by task 1.

- exactly status 404 permits TMDb image lookup;
- 403, 429, other 4xx, 5xx, timeout, connection, invalid content, and a fresh negative-cache marker remain retryable failures;
- no generic ImageProxy browser behavior changes.

The conditional `ArtworkStore` repair method continues to use ImageProxy fetch and existing format/size/hash validation. After a 404 produces a refreshed TMDb source, remove only that source's failure marker before downloading, reusing the catalog retry convention.

## 5. Shared TMDb Image Lookup

One unexported helper in the artwork task service file resolves image URLs without persisting the rest of the response:

| Metadata kind | Existing provider call | Owned fields |
| --- | --- | --- |
| Movie | `GetMovieMatch` | poster, backdrop |
| Series | `GetTVMatch` | poster, backdrop |
| Season | `GetTVSeasonDetails(seriesTMDbID, seasonNum)` | poster |
| Episode | `GetTVEpisodeDetails(seriesTMDbID, seasonNum, episodeNum)` | still |

A nil provider/detail response is a retryable dependency failure, not a confirmed no-image result. A non-nil successful response with an empty owned field is the only no-image result. Returned title, overview, rating, credits, snapshots, identifiers, and checkpoints are ignored.

Task 1 consumes only the failed selection's type. Task 2 calls the helper once per due metadata and consumes only due types.

## 6. Job Flows

### 6.1 TMDb image localization repair

1. Start from an empty in-memory selection keyset and scan all selections in 200-row pages.
2. Available files update summary metrics without emitting per-item details.
3. For a missing file, conditionally repair from the saved source URL.
4. If download succeeds, delete stale recheck state.
5. If and only if the error is typed 404, request the current TMDb image source for that one type.
6. If TMDb supplies a source, conditionally repair it; if TMDb confirms empty, upsert `LastNoImageAt=now`; otherwise record retryable failure.
7. Continue with the last selection ID in memory until the query reaches the end.

### 6.2 TMDb missing-image recheck

1. Start from an empty in-memory metadata keyset and scan all media-backed metadata rows in 200-row pages, with no per-execution TMDb request cap.
2. For each required type, emit cooldown or existing-selection skip details before any provider call.
3. If at least one type is due, perform one kind-specific TMDb detail request.
4. Per due type: save a returned image and delete recheck state, or upsert `LastNoImageAt=now` when the successful response is empty.
5. A provider/download failure leaves that type's timestamp unchanged and does not prevent later candidates from running.
6. Continue with the last metadata ID in memory until the query reaches the end.

Candidate-level failures are isolated so one bad item cannot hide later detail records. The execution continues, then finishes failed with a sanitized count summary when any retryable failures occurred; otherwise it finishes completed.

## 7. Task Definitions, Logs, and Retirement

Register two definitions and scheduler jobs:

| Definition/job key | Name | Enabled | Interval | Settings prefix |
| --- | --- | --- | --- | --- |
| `tmdb_artwork_local_repair` | TMDb 图片本地化修复 | false | 24h | `metadata.tmdb_artwork_local_repair_*` |
| `tmdb_artwork_missing_recheck` | TMDb 无图复查 | false | 24h | `metadata.tmdb_artwork_missing_recheck_*` |

Both use `TaskKindArtwork` and their exact names as definition filters. Manual execution continues through the generic task-definition scheduler action.

Each `TaskUpdate.Details` call contains only newly produced lines and uses existing semantic markers. Details contain title, kind, TMDb ID, type, action, and result; `sanitizeTaskLogError` redacts URLs before errors enter details or `Finish`.

Remove the old definition, scheduler registration, job method, and mixed runner. During `AutoMigrate`, delete only these retired setting keys:

- `metadata.artwork_backfill_enabled`
- `metadata.artwork_backfill_interval_seconds`
- `internal.metadata_artwork_integrity_cursor`
- `internal.tmdb_artwork_local_repair_cursor`
- `internal.tmdb_artwork_missing_recheck_cursor`

Do not delete `task_executions` rows or `<data_dir>/task-logs/**/metadata_artwork_backfill.log`. Since the old key is no longer a valid definition, its definition-specific history/log endpoint may return not found; the files and rows remain intact.

## 8. Compatibility, Rollback, and Risks

- External artwork routes and projections remain unchanged because successful work still ends in `ArtworkAsset` plus `MetadataArtwork`.
- No catalog checkpoint is cleared or advanced, and no catalog job is created.
- No frontend change is expected; backend definition tests are the contract check.
- Two jobs may run concurrently. Conditional repair and insert-if-absent writes, rather than a shared mutex, prevent them from overwriting a newer selection while preserving the requirement that they do not wait on each other.
- Rollback can disable/remove the two new scheduler definitions while leaving `metadata_artwork_rechecks` harmlessly unused. Retired old setting values are intentionally not recoverable; a rollback to the old job therefore returns to its default-disabled configuration.
