# Implementation Plan

## Success Criteria

- Two independent, default-off 24-hour tasks replace the old mixed task.
- Old-link success performs no TMDb request; only a typed image HTTP 404 enters TMDb refresh.
- TMDb no-image state is per metadata/type and enforces a 24-hour cooldown without admitting never-hydrated metadata.
- Automatic writes never overwrite a concurrent manual/local selection and never use catalog hydration jobs.
- Per-item sanitized details and focused regression tests cover the behavior.

## Ordered Checklist

1. **Model and migration state**
   - Add `MetadataArtworkRecheck` to `internal/model/metadata.go` and `model.AllModels()`.
   - Add graph-merge handling that preserves the later per-type no-image timestamp.
   - Extend the exact retired-setting cleanup for the three old artwork-backfill keys.
   - Verify: model migration and merge/retirement PostgreSQL tests.

2. **Repository selection/state contracts**
   - Add selection-ID keyset scanning for TMDb current selections.
   - Add metadata-ID keyset scanning for hydrated, entity-owned recheck candidates and their cooldown/current-selection context.
   - Add recheck upsert/delete and conditional dangling-selection repair writes.
   - Verify: repository tests for provider/type filters, nil hydration exclusion without state, task-1 state admission with nil hydration, pagination, per-type timestamps, and manual-selection race preservation.

3. **Typed image HTTP status and safe repair storage**
   - Introduce the minimal ImageProxy HTTP status error/helper; preserve all existing proxy/cache behavior.
   - Add an ArtworkStore conditional remote repair path using existing fetch, validation, hash storage, and the repository compare-and-swap write.
   - Verify: 404 is detectable; 403/429/5xx remain non-404; a concurrent selection is not overwritten.

4. **Replace the mixed service runner**
   - Rework `internal/service/artwork_backfill.go` into the two bounded keyset runners and one shared kind-specific TMDb image-source helper.
   - Keep provider/download failures item-local; update in-memory keysets, metrics, and newly appended sanitized details.
   - Remove selection invalidation, catalog root discovery, job enqueue/requeue, and catalog worker wake calls from this feature.
   - Verify: old-link success/no-TMDb, 404 refresh, 404/no-image handoff, non-404 matrix, cooldown before/after 24 hours, one-request multi-type handling, never-hydrated exclusion, and partial-failure continuation.

5. **Register two jobs and retire the old one**
   - Replace the old task-definition constant/spec with two exact-name definitions.
   - Replace the old scheduler job with two new default-off 24-hour configured jobs and local job methods.
   - Preserve generic task-center API/UI behavior; do not add frontend special cases.
   - Verify: definitions/scheduler expose both new jobs, old key is absent, schedules are independent, and manual runs retain correct trigger attribution.

6. **Independent review and contract updates**
   - Run `trellis-check` after implementation and address only findings in this task's scope.
   - Update `.trellis/spec/backend/shared-media-metadata.md` and `.trellis/spec/backend/background-task-execution.md` to replace the retired mixed-job contract with the two new task/state/error rules.
   - Confirm no URL, query, credential, local path, temporary file, or generated artifact was added to tracked output.

## Validation Commands

Run focused checks rather than a full repository build:

```bash
go test ./internal/repository -run 'Artwork|MetadataMerge'
go test ./internal/service -run 'Artwork|ImageProxy|TaskDefinitions|Scheduler'
go test ./internal/database -run 'Artwork|RetiredSetting|MetadataSchema'
git diff --check
```

Database-backed tests may skip when `MEDIASTATION_TEST_POSTGRES_DSN` is absent; report that explicitly. No frontend build is required unless implementation unexpectedly changes Web files.

## Risky Files and Rollback Points

- `internal/service/image_proxy_remote_fetch.go`: shared download boundary; keep the change limited to error typing and cover non-404 behavior.
- `internal/repository/artwork_repository.go`: selection race safety and candidate cardinality; focused PostgreSQL tests must prove one row per keyset identity.
- `internal/repository/metadata_repository_merge.go`: new state must move before source deletion.
- `internal/database/schema_migration.go`: delete only the three exact retired keys; old task rows/log files are out of scope.
- `internal/service/artwork_backfill.go`: ensure no remaining call reaches `EnsureCatalogArtworkJob`, `wakeCatalogHydration`, or selection invalidation.

Rollback is code-first: disable/remove the two new scheduler definitions and runners. The new state table can remain because it is isolated and unused; do not restore the retired mixed behavior automatically.
