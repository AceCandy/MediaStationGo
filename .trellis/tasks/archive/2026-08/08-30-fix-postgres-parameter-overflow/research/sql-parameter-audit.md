# SQL Parameter Overflow Audit

## Environment and failure mechanism

- Runtime database: PostgreSQL only; runtime prepared statements are required by `.trellis/spec/backend/database-guidelines.md`.
- GORM: `gorm.io/gorm v1.25.7`; PostgreSQL driver: `gorm.io/driver/postgres v1.5.7`; pgx: `github.com/jackc/pgx/v5 v5.4.3`.
- pgx rejects extended-protocol executions when `len(paramValues) > math.MaxUint16` in `pgconn/pgconn.go:1094-1095`.
- GORM expands slice values used by `IN ?` into one bind parameter per element.

## Confirmed incident

`people_translation_periodic` executes this sequence:

1. `ListPendingPeopleTranslations` loads all pending people.
2. `pendingPeopleTranslationGroups` builds all eligible `personIDs`.
3. `ListPersonWorkContexts` expands every ID in `mc.person_id IN ?`.
4. Only after all pending groups and caches are loaded does `translatePendingPeople` apply `peopleTranslationPassLimit = 1000`.

The failing query therefore uses approximately `len(personIDs) + 2` parameters and fails from 65,534 eligible people onward.

Historical session evidence confirms that the 1,000-item limit intentionally applies after grouping, cache-key deduplication, and negative-cache filtering. Moving the limit before these operations would change established behavior and can starve later valid groups behind negative-cache entries.

## Confirmed unbounded collection paths

### People translation

- `person_repository.go:145-156` — `ListPersonWorkContexts`: all pending person IDs enter one `IN` list.
- `person_repository.go:159-175` — `ListTranslationCaches`: the pre-pass call receives all pending groups and expands three independently deduplicated lists. The later per-window call is bounded to 100, but the earlier call is not.

### Telegram maintenance commands

- `telegram_unbind.go:68-106` — `cmdUnbindDuplicates` loads the complete binding table and deletes all duplicate/invalid IDs through one `IN` list.
- `telegram_unbind.go:109-150` — `cmdUnbindInactive` loads all users and deletes every qualifying binding through one user-ID `IN` list.
- `telegram_unbind.go:179-184` — `deleteTelegramBindings` is the shared SQL sink for both paths.

### Media scrape grouping

- `scrape_worker.go:139-169` — `claimNextPendingMediaGroup` loads all pending media, forms an unbounded group, then claims all group IDs in one update. The full-group update must remain atomic.
- `scrape_worker.go:195-204` — `resetScrapeGroupPending` uses the same unbounded ID list when no series/metadata key is available.
- `scraper_library.go:227-269` — `syncScrapeCandidateGroup` reads and updates every media ID in a logical group through unbounded `IN` lists.

### Other database-derived collections

- `refresh_token_repository.go:44-64` — `RevokeOldestActiveByUserID` loads all active tokens, then revokes the excess IDs in one statement. Normal issuance usually keeps this small, but repository behavior has no hard bound and legacy/imported state can violate the invariant.
- `organizer_directory_versions.go:54-81` — `allExistingPathsInDB` expands every collected destination path in one count query.

### Bulk inserts

- `metadata_repository.go:102-123` — `Create` inserts the complete identifier slice with `Create(&identifiers)`.
- `metadata_repository.go:127-198` — `UpsertCanonical` does the same. Production providers normally supply few identifiers, but the repository contract and upstream payload size are not bounded.

## External/configured collection paths

The following collections are not bounded by result pagination because they participate in filtering before `LIMIT`:

- Emby `Ids` and `PersonIds` parsed by `parseEmbyItemsParams` (`emby_items_handlers.go:13-51`). `Items` later clamps result `Limit` to 500, but that does not cap filter parameter count.
- Playback statistics `library_ids` parsed by `uniqueCSV` (`playback_stats.go:63-69,119-134`). Each ID is validated, but the count is not capped.
- Profile/default visibility `AllowedLibraryIDs` and derived `HiddenLibraryIDs`, used by `applyMediaViewFilter`, metadata search filters, stats, and Emby SQL.
- Repository/service methods that accept caller-built media, metadata, person, library, or path ID slices without a local bound and are reachable from the above filters or full-table/background collections.

These paths should use a single PostgreSQL array parameter at the shared SQL boundary instead of adding arbitrary request-size limits.

## Proven bounded paths

- `scanner_prune.go:126-147` — fixed 500-ID batches.
- `scanner_local_write_batch.go:26-30,101-120` — current production construction uses a 100-row batch.
- `organizer_reclassify_scanned.go:64-80` — `FindInBatches(..., 500)` bounds the downstream page.
- Search-index backfill uses cursor pages; Emby result-derived payload enrichment is bounded by normalized result limits (500 generally, 100 for detail helpers).
- Literal enum lists such as media kinds, credit types, and statuses contain a fixed small number of parameters.

These paths remain unchanged unless a shared unbounded filter is also present in the same statement.

## Selected implementation mechanism

For PostgreSQL string collections, replace slice expansion:

```go
Where("id IN ?", ids)
```

with one array parameter:

```go
Where("id = ANY(?)", &ids)
```

For negative membership, use `column <> ALL(?)` with the slice pointer.

Why this works with the pinned dependencies:

- GORM v1.25.7 expands values whose reflected kind is slice/array, but treats a pointer as one bind variable.
- pgx v5.4.3 `CheckNamedValue` passes arbitrary values through and `TryWrapDerefPointerEncodePlan` dereferences the pointer; the resulting `[]string` is encoded as a PostgreSQL array.
- All project IDs are `varchar(36)` strings and paths are text, so PostgreSQL can infer compatible text/varchar array types from `ANY`/`ALL` expressions.

This preserves one SQL statement, ordering, pagination, row-count checks, transaction scope, and atomic full-group updates while reducing N bind parameters to one. It is preferable to per-query chunk loops for selectors and claim updates.

Bulk inserts use GORM's existing `CreateInBatches(&identifiers, 500)` so each generated INSERT has a fixed parameter ceiling.

## Additional candidate outside this defect class

`applyMetadataSearchLIKEFilter` builds a variable number of explicit predicates and scalar arguments from free-text search variants. It does not expand one collection placeholder and needs a separate product decision about maximum search input or a different search-expression model. It is recorded as a remaining input-hardening risk, not mixed into the ID-collection fix.

## Planned verification

- PostgreSQL regression: call the confirmed people-context path with more than 65,535 IDs and assert it no longer returns the pgx parameter-limit error.
- Functional regression: preserve people KnownFor ordering and translation-cache hit/negative-cache behavior.
- Bulk insert regression: exercise more than one metadata-identifier insert batch.
- Focused existing tests for Telegram cleanup, scrape-group claim/reset/sync, Emby filters, visibility, playback stats, and organizer path checks.
- Final static audit: every remaining production `IN ?` / `NOT IN ?` with a non-literal slice must have a documented fixed page/batch/input bound.
