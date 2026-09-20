# Local NFO Catalog Boundaries

## 1. Scope / Trigger

Applies to `nfo_movie` and `nfo_tv` ingestion, independent state, Web/Emby
projections and tasks. Four models are registered in `AllModels`; startup
creates their tables. No historical migration is provided, as the user
confirmed there are no existing NFO libraries.

## 2. Signatures

- `NFORepository.Ingest(ctx, media, input) (changed bool, err error)` writes
  independent items and file snapshots transactionally; source is `nfo`, with
  NULL `media.metadata_id`.
- Scans and file events use common kinds `scan`/`watch` and definitions
  `library_scan`/`library_watch`, including NFO and HongGuo libraries.
- `POST /api/tasks/definitions/library_scan/run` requires `{library_id: string}`.
  The legacy `nfo_scan/run` action still accepts NFO targets and creates a common scan.
- Task Center redirects the legacy `system=nfo` URL to `system=common`;
  invalid/duplicate system values retain existing canonicalization behavior.

## 3. Contracts

- `nfo_items`, `nfo_media_bindings`, `nfo_user_states` and
  `nfo_playback_events` own identities, per-file snapshots and user state.
  Public logical IDs use `nfo-<uuid>`; never persist them in metadata_id.
- Movie versions require matching directory prefixes and explicit delimiters.
  Episode versions require matching filename prefixes through SxxExx and
  explicit version suffixes; equal episode numbers alone never merge.
- Each episode requires valid local NFO. Missing show/season NFO creates only
  initial placeholders and never erases accepted parent fields. Missing images
  retain accepted immutable assets. External IDs never trigger provider calls.
- Search, hierarchy, versions, favorites and history route by source. Ordinary
  scraping excludes NFO. Administrator statistics supports `system=nfo` and
  combined `system=all`; see playback-contracts.md. Playlists are not integrated.
- Series presentations retain their own SeriesID/SeriesTitle so card projection
  cannot erase hierarchy identity. Conflict-upsert reloads use a fresh GORM
  object to avoid filtering by a newly generated, unpersisted primary key.
- Each scheduled scan or watcher batch creates one common execution across
  library types. NFO scans share the common timer and library selector. Catalog
  isolation does not require separate scan/watch tasks or timers.
- Legacy `nfo_scan`/`nfo_watch` definitions are hidden from the task list, but
  their history/log APIs remain readable without rewriting stored records.
  The legacy NFO action retains its non-NFO target rejection.
- NFO network-scrape exclusion depends on library type, never task kind;
  this also applies to STRM refresh after generation.
- If watcher library lookup fails, requeue both video candidates and NFO/image
  sidecars through the existing debounce queue. Query failure must not consume
  accepted event types; directories and unsupported extensions remain excluded.
- Legacy empty-system task rows derive system from kind. The catalog fallback
  excludes both `hongguo_` and `nfo_` prefixes.
- NFO ingestion serializes a path before checking/inserting its row. Unchanged
  file facts and snapshot fingerprint do not rewrite accepted snapshots.
  File-size/mtime changes invalidate complete probe documents in the same
  transaction. Invalid/missing NFO retains the previous binding and snapshot.
- Valid unrelated XML is not accepted as an empty NFO. Explicit season zero is
  supported; missing/invalid seasons are not silently converted into specials.
- Item/ancestor NSFW flags participate both in SQL visibility and the returned
  file projection, even if the per-file snapshot is not NSFW.
- In the retained local scraping path, `applyLocalMetadataMatch` must pass the
  merged `next` object to `persistLocalMetadata`. Passing the original media
  drops newly read episode coordinates and binds the file to the Series.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| `nfo_scan` targets ordinary library | HTTP 400; no execution |
| Public scan targets an NFO library | Common scan execution; local NFO ingestion |
| Mixed ordinary/HongGuo/NFO file events | One common watch execution |
| Same file and snapshot scanned twice | Second ingest returns changed=false |
| NFO becomes invalid/missing | Preserve accepted snapshot, report file state |
| Ancestor/item is NSFW and profile disallows it | No visible file |
| `<html>` supplied as movie NFO | Reject instead of clearing metadata |
| Single-episode NFO has no valid season | Reject; no invented season zero |
| Fresh or repeated startup | Create/reuse four independent tables |

## 5. Good / Base / Bad Cases

Good: two clear movie versions share a local item but retain file snapshots.
Base: a raw episode learns S01E02 from its NFO before legacy persistence.
Bad: set `metadata_id` to an NFO UUID, infer identity from a provider ID, or
claim complete isolation while the scanner still uses the old writer.

## 6. Tests Required

`TestNFORepositoryPreservesFilesAndPreviousSnapshot` covers repeat/changed
snapshots, invalid-NFO preservation, independent views and NSFW/library filters.
`TestNFORejectsWrongDocumentAndUnknownSeason` covers document/season validation.
`TestNFOTasksUseCommonDefinitions`, `TestNFOTaskLegacySystemFilter`,
`TestNFOWatcherSharesMixedBatch`, and `TestNFOSchedulerSharesLibraries`
cover common attribution and historical access. Run PostgreSQL tests with the isolated test DSN.
`TestWatcherBatchRequeuesSidecarsWhenLibraryQueryFails` covers sidecar-only and
mixed batches on lookup failure, including every supported image extension.
`TestNFOOnlyTVPersistsShowAndEpisodeNFO` must retain its episode-title/coordinate
assertions; its fixture must include `MediaProbeMetadata` used by MediaView.
`TestNFOFreshStartupAndScan` verifies repeated startup, local image refresh,
independent state/events, search and Web/Emby reads without shared metadata.
`TestNFOSeriesHierarchyAndStateIsolation` verifies explicit versions, Web
cards/episodes/recent items, SearchHints, hierarchy, favorites, played state,
resume grouping and hidden-library filtering. Browser/device playback remains
a separate manual acceptance step.

## 7. Wrong vs Correct

Wrong: create a separate scan/watch task for each isolated catalog, or infer
whether scraping is allowed from the execution kind.
Correct: share file tasks and select ingestion/scraping behavior by library type.

Wrong: merge NFO into `next`, then call `persistLocalMetadata(ctx, m, ...)`.
Correct: pass `&next`, so metadata kind/season/episode use the merged facts.

Wrong: route all NFO library lists to tables that the application does not
create or migrate yet. Correct: finish and verify schema/upgrade/read integration
before activating that route.
