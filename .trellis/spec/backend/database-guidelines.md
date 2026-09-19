# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

<!--
Document your project's database conventions here.

Questions to answer:
- What ORM/query library do you use?
- How are migrations managed?
- What are the naming conventions for tables/columns?
- How do you handle transactions?
-->

(To be filled by the team)

## Scenario: Database-Backed AI Configuration

### 1. Scope / Trigger

Use this contract when adding AI provider options that must be editable from the admin API and survive restarts.

### 2. Signatures

- `model.APIConfig`: `Model string`, `WebSearchEnabled bool`.
- `service.APIConfigPatch`: pointer fields `model` and `web_search_enabled` for partial updates.
- `service.Resolved`: decrypted runtime projection carrying both fields.

### 3. Contracts

- `PUT /admin/api-configs/openai` accepts `model` and `web_search_enabled`.
- `GET /admin/api-configs` returns `model` and `web_search_enabled`; API keys remain masked.
- A non-empty database `model` overrides `cfg.AI.Model`; an empty value keeps the file fallback.
- `web_search_enabled=true` applies only to AI chat and sends Responses API `tools: [{"type":"web_search"}]`.

### 4. Validation & Error Matrix

- Empty model -> retain the runtime fallback.
- Responses API HTTP error or unsupported upstream -> return the error; never claim a web-backed answer.
- Disabled/unconfigured AI -> return the existing offline reply without an external request.

### 5. Good/Base/Bad Cases

- Good: database model `gpt-5.6`, search enabled, `/responses` returns message output text.
- Base: search disabled, `/chat/completions` remains unchanged.
- Bad: search enabled against a provider without Responses support; surface the upstream error.

### 6. Tests Required

- Assert API config update/public/resolve round-trip for model and search flag.
- Assert enabled chat requests `/responses` with model, input, and `web_search` and extracts `output_text`.
- Assert disabled chat requests `/chat/completions`; People translation requests `/responses` without tools.

### 7. Wrong vs Correct

Wrong: add `web_search` to People translation just because it uses Responses.

Correct: use Responses for People translation without tools; reserve `web_search` for `Chat` when its toggle is enabled.

---

## Query Patterns

### Hard-Delete Tables in Hand-Written SQL

Models embedding `PermanentBase` have no `deleted_at` column. Before adding a
raw SQL or string-based GORM condition, verify the model base type; joins to a
hard-delete table must not copy the `deleted_at IS NULL` filter used by soft-delete
tables. When a model changes to `PermanentBase`, search production SQL for both
the table name and its aliases. Compatibility migrations may reference the
retired column only behind an explicit `HasColumn` guard.

```go
// Wrong: metadata_items uses PermanentBase.
Joins("LEFT JOIN metadata_items AS mi ON mi.id = m.metadata_id AND mi.deleted_at IS NULL")

// Correct: row absence already represents deletion.
Joins("LEFT JOIN metadata_items AS mi ON mi.id = m.metadata_id")
```

PostgreSQL regression tests for hand-written queries count as verified only when
`MEDIASTATION_TEST_POSTGRES_DSN` is configured and the test is not skipped.

### Integer Duration Aggregates

PostgreSQL `SUM(bigint)` returns `numeric`. Before scanning a duration aggregate
into a Go integer, cast the coalesced millisecond sum to `bigint` before integer
division: `COALESCE(SUM(duration_ms), 0)::bigint / 1000`. When fixing this
pattern, search every equivalent aggregate and use a non-whole-second value in
the PostgreSQL regression test so an uncast expression fails.

### Rebuild GORM Statements After Aggregation

Do not reuse a `*gorm.DB` statement after adding `Select`, `Group`, `Order`,
`Limit`, or `Offset` for one result shape. Those clauses can remain on sibling
queries and produce invalid SQL or silently wrong results. Keep shared filters
in a small query-builder method and call it separately for counts, aggregates,
details, and rankings. A real PostgreSQL test must execute every branch.

### Emby Series Pagination Query Boundary

`seriesMetadataPageWithCount` materializes the visible file scope with
`WITH scoped_media AS MATERIALIZED (...)` before joining seasons and series.
A lateral season lookup with `OFFSET 0` does not prevent PostgreSQL from
scanning the whole metadata catalog and probing `media` once per episode.
Keep library, visibility and played filters inside the file scope; apply
person and favorite filters through `applySeriesPageFilters` after the series
alias exists, consistently for count, page and summary queries. Preserve
`created_at` in the CTE for every supported sort. Latest skips the count.

For `IsFavorite`, use `AS NOT MATERIALIZED` for both count and page so the
planner can start from the user's favorites and look up only their files.
Unconditional materialization makes a small favorites page scan the entire
visible file scope. Keep ordinary browsing materialized; do not generalize
this exception to other filters without execution-plan evidence.

Library-scoped `LatestItems` must not apply a raw `LIMIT` before logical work
grouping. Keyset-page file candidates by `(created_at, id)`, resolve metadata
or series IDs, and scan through the selected page's final `created_at` boundary
before applying the ID tie break. This keeps same-time ordering exact while
letting the existing `(library_id, created_at DESC)` index avoid full-library
aggregation. `TestEmbyLatestItemsPaginatesMetadataBeforeLoadingVersions` and
`TestEmbyLatestSeriesItemsContinueThroughCandidateTimeTie` cover the batch and
tie boundaries.

`TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog` must execute on
PostgreSQL and check plan loops, totals, empty pages, sorting and filtered
summaries. SQL shape alone is insufficient evidence of the join order.
For plans involving PostgreSQL array parameters, retain SQL placeholders and
bound values; GORM's interpolated log text (for example `'[hidden]'`) is not
an executable PostgreSQL array literal and must not be replayed as SQL.

### Favorite Cards and Season Recheck Scopes

`ListFavoriteCards` expands each active user's Movie/Series favorite through a
lateral UNION ALL of the work itself, its seasons and their episodes before
joining files. Keep kind checks, file/library and favorite visibility before
ranking by `(m.created_at DESC, m.id DESC)`. Direct Series/Season files are valid
representatives. `TestListFavourites*` covers presentation, direct files, ties
and hidden libraries. Do not restore a whole-catalog CASE-based favorite join.

`ListTMDbSeasonMetadataRecheckAfter` uses a non-correlated membership set of
file metadata IDs and file-backed episode parent IDs. The `OFFSET 0` media
projection prevents per-episode file probes; this still scans the media scope
per page. Preserve cooling time, missing-field checks, identity multiplicity,
ID pagination and deduplication. `TestListTMDbSeasonMetadataRecheckAfterFiltersAndPages`
must run on PostgreSQL, including seasons with metadata but no files.

The runtime Season/Episode maintenance worker now uses the persistent recheck
queue described in `background-task-execution.md`; the candidate methods remain
for focused compatibility tests. Migrations register queue models before installing
idempotent transaction triggers, and never run file reconciliation.
Filter changed fields inside the trigger function, not through `UPDATE OF`:
column-specific trigger dependencies block the existing repeated GORM type migration.
Keep the due predicate nonvolatile (`statement_timestamp()`), otherwise PostgreSQL
may filter the whole future queue instead of applying an index condition.

Change expansion uses `idx_tmdb_recheck_changes_pending_id` on
`tm_db_recheck_changes(metadata_id) WHERE pending`. A partial index on the boolean
`pending` alone does not satisfy `ORDER BY metadata_id LIMIT 1`: PostgreSQL may
repeatedly scan the processed primary-key prefix, making a draining queue slower.
Create the replacement before dropping `idx_tmdb_recheck_changes_pending`.
`TestTMDbRecheckPendingIndexSkipsProcessedPrefix` checks repeated migration and
the generic prepared plan with `FOR UPDATE SKIP LOCKED` on a mostly processed
queue. Existing deployments can build the new index concurrently before upgrade;
startup model migration uses ordinary index creation.

### HongGuo Download Claim Indexes

1. Scope: `claimHongGuoDownload` and startup performance indexes.
2. Signatures: `idx_hg_download_transfer_claim` and
   `idx_hg_download_verification_claim` index `(created_at, id)` for the five
   queued/waiting/active states, partitioned by the exact raw/hash predicates.
3. Contract: express eligibility as the five fixed literal states AND
   `(status IN ('queued', 'waiting_verify') OR lease_until < ?)`. Keep the
   transaction, ascending order, `FOR UPDATE SKIP LOCKED`, and checkpoint routing.
4. Boundaries: active rows with NULL/unexpired leases remain ineligible;
   failed/cancelled/completed rows never enter either claim. NULL SHA256 still
   behaves as empty. Migration errors abort startup; repeated creation is safe.
5. Cases: an expired transfer resumes; a raw/hash checkpoint uses verification;
   scanning and sorting the whole pending queue for LIMIT 1 is a regression.
6. Tests: `TestHongGuoDownloadClaimOrderAndPlan` runs real PostgreSQL, repeated
   migration, state/order assertions and the actual repository-generated SQL
   under `force_generic_plan`. Assert the respective index is used with no Sort
   or Seq Scan. Startup creation can block writers; deploy existing databases
   with separately reviewed `CREATE INDEX CONCURRENTLY` before restarting.
7. Wrong: merely add an index while keeping the two status branches in a top-level
   OR; this can retain BitmapOr and sorting. Correct: factor out the literal
   five-state predicate. Parameterizing that predicate can prevent generic
   plans from proving partial-index eligibility.

### Search Index Backfill Batches

For library membership in `metadataSearchDocuments`, count at most 8,193
candidate episodes before choosing the media join. Up to 8,192 episodes keeps
indexed point lookups; larger scopes use an `OFFSET 0` media projection to
avoid thousands of failed per-episode probes. This projection scans the media
index, so do not apply it unconditionally to small incremental updates. Recheck
the crossover when media volume changes substantially. Preserve UNION pair
deduplication and all movie/season/episode kind checks in both paths.
At roughly 375k media rows, the former 1,024 threshold forced a full index
scan for batches of 1,026–1,181 episodes (190–238ms versus 12–14ms with
point lookups). An 8,001-episode sample also favored lookups (122ms versus
286ms). These are sampled plans, not a universal crossover guarantee.
`TestMetadataSearchDocumentsWithManyUnplayableEpisodes` covers both sides
of the 8,192 boundary and identical document output with sparse files.

`metadataSearchDocumentIDs` pages movie/series candidates by ID without the
global playable `EXISTS` filter. `metadataSearchDocuments` validates playable
library membership for those bounded IDs and omits empty works. Advance the
cursor and decide completion using candidate IDs, never emitted documents:
an entire candidate batch can have no files while later batches are playable.
Do not reuse this unfiltered candidate enumeration for user-facing search.

Startup warmup passes `search.index_warmup_pause_ms` to `BackfillSearchIndex`
(default 2000 ms, minimum 250 ms). Waiting must honor cancellation, discard
the unfinished index and retain the existing activation/dirty-update protocol.
Tests cover empty batches, movie versions, series, inter-batch waiting and
cancellation without activating the incomplete index.

### Background Translation Candidate Queries

`idx_people_pending_translation` indexes `people(id)` only for live rows with
non-empty, unchanged, non-Chinese original names. Its predicate must match
`ListPendingPeopleTranslations`; cache language/version remain runtime filters.
Verify index use with a generic parameterized LIMIT and a mostly ineligible
population. Startup creation is idempotent and may briefly block people writes.

Filter untranslated names/roles and active negative translation caches in SQL
before applying limits. Chinese detection must match `containsChinese` exactly
(U+4E00 through U+9FFF), not the broader title-localization regex. Cache matching
includes kind, context, source, target language and prompt version; deleted
caches do not suppress work and positive caches remain eligible for write-back.

People are selected first, up to 1,000 groups. Roles keyset-page by
`(metadata_id, id)` with only page-local metadata context hydration. The limit
counts groups, not rows: keep collecting targets for selected season-role groups
after the group limit, including later episodes/pages. This still scans the
eligible role set; bounded result hydration does not guarantee bounded table
scanning without a matching index.

Role context hydration must include the current metadata and at most two parent
levels. Season and episode roles send `series title / season title` to both the
translation request and task detail, while their cache context remains the season
ID. Missing parents fall back to the current metadata title. Regression coverage
must include credits attached directly to a season and through an episode.

Startup `ensurePerformanceIndexes` creates
`idx_metadata_credits_type_pending_translation` on `(type, metadata_id, id)` where
`original_role <> '' AND role = original_role AND original_role !~ '[一-鿿]'`.
Do not put parameterized Actor/GuestStar values in the index predicate: generic
prepared plans cannot prove that bound values imply that predicate. Do not add
unbounded role text to B-tree keys or INCLUDE columns. Creation is idempotent;
the initial non-concurrent startup build can delay startup and block concurrent
credit writes. `TestEnsurePerformanceIndexesCreatesHotPathIndexes` verifies
repeat migration and actual index use with `force_generic_plan`.
Create the type-leading index before dropping the superseded
`idx_metadata_credits_pending_translation`. Keep the mixed Actor/GuestStar/
Director/Writer fixture: omitting type from the index key scans unrelated crew
roles even when the partial index is used. The generic plan must use an index
condition, not scan all entries and discard Director/Writer via a filter.

`TestPendingPeopleTranslationsFiltersBeforeLimit` and
`TestPendingRoleTranslationsCollectsTargetsAcrossPageAndGroupLimit` must run on
PostgreSQL. Do not replace group-aware collection with a raw row LIMIT.

### Douban Enrichment Snapshot Reuse

`ListDoubanMovieEnrichmentAfter` relies on the unique `(metadata_id, provider)`
snapshot constraint to replace repeated correlated EXISTS with one LEFT JOIN.
Keep `degraded IS NOT TRUE` so absent snapshots remain eligible. A lateral
`SELECT payload #> '{}' AS payload OFFSET 0` preserves the whole JSON value and
lets field checks reuse its detoasted representation; plain aliasing can inline
the expression and repeat decompression. Verify this boundary with real plans.

JSON key presence is not the same as a non-null value: `pic: null` still counts
as a present key. Preserve scalar/array/JSON-null behavior, stale cutoff, unique
same-kind Movie/Series identifier counting and artwork asset existence checks.
`TestDoubanEnrichmentCandidateJSONAndPagination` covers these JSON boundaries,
pagination, cancellation and the snapshot uniqueness precondition.

### Storage and Missing-Snapshot Statistics

`StorageService.Compute` counts distinct metadata IDs within each library/kind/
parent group, not files. Keep `COUNT(DISTINCT m.metadata_id)` if the input media
projection is not deduplicated; a plain COUNT would count multiple versions.
Season and series sets must include direct attachments and episode ancestors.
Capacity remains a separate per-file probe sum, including unmatched files;
unprobed files contribute counts but no bytes. Preserve disabled/empty libraries
and exclude deleted libraries. `TestStorageBreakdownCountsLibraryMetadata`
covers these cases, including duplicate direct series and season files.

`missingTMDbSnapshotQuery` filters through same-kind TMDb identifiers with no
TMDb snapshot before resolving candidate metadata. Keep the existing lateral
selection of the shortest numeric identifier, its CASE-guarded bigint cast,
int32 bound and ID cursor ordering. EXISTS must not multiply rows for aliases;
these catalog queries must work without a media table. Do not force the missing
identifier set to materialize: leave cursor pushdown and join choice available
for both a new catalog and a mostly completed backfill.

`TestListMissingTMDbSnapshotsWithoutMediaTable` covers aliases, wrong provider/
kind, malformed/overflow IDs and pagination. Identifier fixtures must respect
the global provider/kind/external-ID uniqueness constraint, even across metadata.
`TestMissingTMDbSnapshotCountScopesMetadataProbes` ANALYZEs its isolated fixture
of 10,000 unrelated metadata rows and one valid identifier, then checks the
actual PostgreSQL plan does not scan the unrelated metadata. Re-evaluate that
performance guard on PostgreSQL upgrades; do not replace it with a wall-clock
threshold that fluctuates with test-machine load.

### Recent Logical Works and Library Season Sets

For an explicit series ID, `libraryMetadataScope` must restrict library files
through `logicalMetadataCandidates` before the CASE-based ancestor joins.
The same scope feeds card pagination and episode details. Preserve work-level
filters and direct series/season attachments; movie and unspecified-series
paths retain their existing scopes. Candidates are used through `IN`, not a
multiplying join, so overlapping IDs in `UNION ALL` need no extra DISTINCT.
`TestLibrarySeriesEpisodesScopesProjectionBeforeJoins` checks metadata traversal
as well as identifier hydration against unrelated catalog fixtures; do not only
bound the final projection while leaving its ID subquery to scan the catalog.
When replaying array-bearing fixture SQL for EXPLAIN, restore the placeholder
and bind the actual array instead of executing the logger's Go-slice text.

1. Scope: `ListRecentLogicalWorks`, ordinary series library pagination and
   `ensurePerformanceIndexes`.
2. Signatures: `idx_media_recent_metadata` indexes `(created_at DESC, id DESC)`
   with the literal predicate `metadata_id IS NOT NULL`. Existing library-leading
   indexes do not supply global time order. Creation is idempotent; use separately
   approved `CREATE INDEX CONCURRENTLY` before upgrading an existing database.
3. Contract: resolve batches of 128 file candidates with the original visibility
   and missing-field filters. Stop only after crossing the last selected work's
   timestamp, then break ties by logical ID. Hydrate complete work versions.
4. Boundaries: NULL timestamps or 16 unresolved batches fall back to the original
   aggregate; they never truncate results. Query errors propagate. Startup index
   creation can block writers, and deployment still requires runtime verification.
5. Cases: same-time works across batches retain exact ranking; selective filters
   may require fallback. Ordinary series browsing shares a narrow season set with
   `OFFSET 0`; explicit-work, movie and missing-field paths retain point lookups.
6. Tests: `TestRecentLogicalWorksPreservesBatchTiesAndFilters` compares complete
   old/new results, NULL/fallback and index plans. Migration tests assert repeated
   creation. `TestLibrarySeriesPageReadsSeasonSetOnce` verifies one season-table
   read for 2,000 episodes; direct-file, tie, empty-page and stale-statistics tests
   preserve pagination semantics. Run on PostgreSQL, not skipped.
7. Wrong: LIMIT files once and treat them as the complete recent-work page.
   Correct: advance a keyset until the time boundary is cleared, or use the exact
   aggregate fallback. Do not remove scoped pagination materialization: a measured
   NOT MATERIALIZED candidate timed out at 10 seconds.

### Nullable PostgreSQL Aggregates

PostgreSQL aggregates such as `MIN` return SQL `NULL` when no row matches.
Repository methods must scan nullable aggregate results into `sql.Null*` or an
equivalent nullable type and translate the empty result at the repository
boundary. Do not use an arbitrary `COALESCE` sentinel.

```go
var value sql.NullTime
err := db.Select("MIN(next_attempt_at)").Scan(&value).Error
if err != nil || !value.Valid {
	return nil, err
}
return &value.Time, nil
```

Regression tests must assert that an empty matched set returns `(nil, nil)` and
that a populated set returns the expected aggregate value.

### Nullable Scalar Projections

Do not scan a nullable scalar column into a non-nullable Go slice such as
`[]string`. When collecting non-null `media.metadata_id` values for downstream
invalidation, add `metadata_id IS NOT NULL` to the projection query without
narrowing the separate media update or delete query. If callers need to retain
null rows, scan into `sql.Null*` values instead.

Regression tests must include a row whose projected column is SQL `NULL` and
assert that the primary update or delete still succeeds.

## Scenario: PostgreSQL Collection Parameter Limits

### 1. Scope / Trigger

Apply this contract when a production query binds a collection whose size can
grow with database state, persisted configuration, or request input.

### 2. Signatures

- Positive string membership: `column = ANY(?)` with `&values`.
- Negative string membership: `column <> ALL(?)` with `&values`.
- Unbounded multi-row inserts: `CreateInBatches(&rows, 500)`.

### 3. Contracts

- Runtime prepared statements remain enabled; do not switch to simple protocol
  to avoid PostgreSQL's 65,535 extended-protocol parameter limit.
- Pass a pointer to the Go slice so GORM binds one value and pgx encodes the
  dereferenced slice as a PostgreSQL array.
- Preserve existing empty-slice guards, ordering, pagination, transaction
  boundaries, and row-count conflict checks.
- Fixed enum lists and collections already bounded by a local page or batch may
  continue using `IN ?`.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Unbounded string collection | Bind one array parameter with `ANY` or `ALL` |
| Empty collection with defined no-op behavior | Return before executing SQL |
| Unbounded bulk insert | Use a fixed batch size far below 65,535 parameters |
| Fixed enum or locally bounded batch | Existing `IN ?` is allowed |

### 5. Good / Base / Bad Cases

- Good: 65,536 IDs execute through one array parameter without changing result order.
- Base: a 500-row scanner batch keeps its existing bounded `IN ?` query.
- Bad: truncate an unbounded filter or move a worker limit earlier, changing
  selection, pagination, cache, or atomic claim semantics.

### 6. Tests Required

- Assert in GORM dry-run mode that a slice pointer produces one bind variable.
- With `MEDIASTATION_TEST_POSTGRES_DSN`, execute a collection larger than
  65,535 values and assert the real pgx query succeeds.
- Exercise any batched insert across more than one batch and verify the final row count.

### 7. Wrong vs Correct

```go
// Wrong: GORM expands ids into one bind parameter per element.
db.Where("id IN ?", ids)

// Correct: pgx receives one PostgreSQL array parameter.
db.Where("id = ANY(?)", &ids)
```

## Runtime Configuration Defaults

- Define each runtime default once in the owning config package. Constructors
  may use that exported constant as a defensive fallback, but must not repeat a
  numeric default independently.
- When changing a default, update and test the final `config.Load()` projection.
  A constructor-only test is insufficient because Viper defaults populate the
  field before the constructor runs.
- Preserve explicit file and environment overrides unless the product contract
  explicitly requires a fixed value.

---

## Migrations

- Store provider/NFO/AI free-form text as `text` unless the upstream contract
  defines a real maximum. Do not infer `varchar(255)` from typical samples.
- When multiple GORM models map to the same table, every shared column must use
  the same type tag. Search `AllModels()` and both explicit and default table
  names before changing one model; a later model can otherwise undo the first
  model's migration in the same `AutoMigrate` call.
- When changing a PostgreSQL column type, include an idempotent compatibility
  statement and a PostgreSQL schema assertion.

### Scenario: Local Media Scan Fingerprints and Hard Delete

#### 1. Scope / Trigger

Use this contract when changing local media discovery, media deletion, or the `media` schema.

#### 2. Signatures

- `media.scan_file_size_bytes bigint` stores the size of the scanned path itself.
- `media.scan_file_mtime_ns bigint` stores `os.FileInfo.ModTime().UnixNano()` for that path.
- `DELETE /media/:id` permanently deletes the media row but never deletes the disk file.

#### 3. Contracts

- A local file is unchanged only when both scan fingerprint fields match and path-derived metadata needs no refresh.
- Both direct and snapshot scanner reads include the stored season number.
  A negative season with an explicit filename SxxExx marker bypasses unchanged
  fingerprint skipping. Reparse the coordinates (including Season 0); never
  coerce every negative value to 0 or infer a correction from an unknown name.
  Updating a negative season on an error row to valid coordinates returns it
  to pending and clears the stale scrape error, unless already matched by the
  incoming canonical attachment. `TestScanRepairsExplicitNegativeSeasonWithoutFileChanges`
  verifies both read paths, persisted repair, unknown-name preservation and
  subsequent unchanged-scan skipping against PostgreSQL.
- Scrape retries do not run the scanner. Shared enrichment and Series sibling
  binding also call `repairInvalidScrapeSeason` before creating Season metadata.
  It reparses explicit SxxExx only for negative stored seasons, and conditionally
  updates the original ID/path/coordinates to avoid overwriting a concurrent
  edit. Unknown filenames and nonnegative seasons remain unchanged. Regression:
  `TestScrapeRetryRepairsSpecialSeasonWithoutRescan` covers library and single
  retries binding S00E01/S00E25 to canonical Special Episodes without scanning.
- For `.strm`, the fingerprint belongs to the local sidecar; playback-target size belongs to `media_probe_metadata.size_bytes` and must not be reused as the scan fingerprint.
- The unchanged return occurs before media Upsert and probe scheduling, preserving `media.updated_at` and probe data.
- Post-scan automatic STRM generation skips media whose source path or container is already `.strm`; skipped source paths remain protected from overwrite cleanup.
- `Media` uses `PermanentBase`; every media deletion is physical and the `media` table has no `deleted_at` column.
- Hard delete retains shared metadata and user state while the existing foreign key cascades `media_probe_metadata`.

#### 4. Validation & Error Matrix

- `scan_file_mtime_ns = 0` -> process once and persist the fingerprint.
- Size or mtime differs -> process and update the fingerprint.
- Size and mtime match -> skip unless path-derived metadata changed.
- Historical `deleted_at IS NOT NULL` row -> purge during migration; a database error aborts migration.

#### 5. Good/Base/Bad Cases

- Good: an unchanged `.strm` is skipped without reading or probing its target.
- Base: a legacy row is updated once, then skipped on the next unchanged scan.
- Bad: comparing `.strm` sidecar size with `media_probe_metadata.size_bytes`, causing every scan to update.

#### 6. Tests Required

- Assert unchanged regular files and `.strm` files are skipped and keep `updated_at` unchanged.
- Assert a size or nanosecond-mtime change updates the row.
- Assert hard delete removes probe metadata but retains shared metadata and user-state rows.
- Run migration twice and assert historical soft-deleted media stays absent, active media remains, and `media.deleted_at` is removed.

#### 7. Wrong vs Correct

Wrong: call scoped `Delete(&model.Media{})` or special-case `.strm` to update on every scan.

Correct: compare the dedicated scan fingerprint and use `Unscoped().Delete(&model.Media{})` for every media deletion path.

### Scenario: Retired Setting Cleanup

1. Scope / Trigger: when a removed feature owns keys in the shared `settings` table, clean up only those retired keys during `AutoMigrate`.
2. Signatures: add an unexported `func remove<Feature>Settings(db *gorm.DB) error` and call it from `AutoMigrate`.
3. Contracts: use exact key names; keep the shared `settings` table and every unrelated row.
4. Validation & Error Matrix: a database error aborts migration; absent keys are a successful no-op.
5. Good/Base/Bad Cases: delete all owned keys; repeated execution succeeds; prefix-wide deletion that can remove another feature's keys is invalid.
6. Tests Required: run the full `AutoMigrate` path twice on PostgreSQL, then assert retired keys are absent and an unrelated setting remains unchanged.
7. Wrong vs Correct: wrong is leaving unused settings indefinitely or deleting with a broad prefix; correct is an idempotent exact-key deletion covered by a PostgreSQL test.

### PostgreSQL Prepared Plan Safety

- PostgreSQL schema migrations must use a dedicated connection with GORM
  `PrepareStmt=false` and pgx `PreferSimpleProtocol=true`.
- After migration, close that connection and open the direct PostgreSQL runtime
  connection with prepared statements enabled.
- Do not enable runtime prepared statements through a transaction pooler unless
  the pooler explicitly supports them and schema-change invalidation is tested.
- Clearing GORM's cache after `AutoMigrate` is insufficient because a stale-plan
  error can occur inside `AutoMigrate` itself.
- Any change to the PostgreSQL dialector must retain a focused test proving the
  migration/runtime protocol split.
- When a schema change touches a table queried during startup, verify one real
  PostgreSQL/pooler startup and the first business query after startup. Unit
  migration tests alone do not cover pooled server-session state.

## Scenario: PostgreSQL-Only Database Runtime

### 1. Scope / Trigger

- Applies to database configuration, startup, migrations, repositories, and database-backed tests.

### 2. Signatures

- `database.Open(*config.Config, *zap.Logger) (*gorm.DB, error)` opens PostgreSQL.
- `database.OpenForMigration(*config.Config, *zap.Logger) (*gorm.DB, error)` opens PostgreSQL with migration-safe protocol settings.
- `MEDIASTATION_DATABASE_TYPE=postgres` and `MEDIASTATION_DATABASE_DSN=<postgres dsn>` are the database inputs.

### 3. Contracts

- PostgreSQL is the only runtime and test database dialect.
- `database.dsn` is required; there is no embedded database fallback.
- Runtime connections enable prepared statements; migration connections use simple protocol and disable GORM prepared statements.
- Database tests use `MEDIASTATION_TEST_POSTGRES_DSN` and an isolated schema per test.

### 4. Validation & Error Matrix

- Missing DSN -> return `database.dsn is required` before opening GORM.
- Any non-PostgreSQL `database.type` -> return an unsupported-type error naming PostgreSQL as the only supported type.
- Missing `MEDIASTATION_TEST_POSTGRES_DSN` -> skip database-backed tests with an explicit message.

### 5. Good/Base/Bad Cases

- Good: `type=postgres` with a reachable DSN opens and migrates successfully.
- Base: `postgresql` and `pg` aliases select the same PostgreSQL dialector.
- Bad: an embedded/file database type or an empty DSN never falls back to another dialect.

### 6. Tests Required

- Assert non-PostgreSQL types are rejected.
- Assert an empty DSN is rejected.
- Assert migration and runtime dialectors retain their simple/prepared protocol split.
- Run database integration tests against isolated PostgreSQL schemas.

### 7. Wrong vs Correct

Wrong: silently fall back to a file database when the PostgreSQL DSN is absent.

Correct: fail startup with a concrete configuration error so deployment mistakes are visible.

---

## Naming Conventions

<!-- Table names, column names, index names -->

(To be filled by the team)

---

## Common Mistakes

<!-- Database-related mistakes your team has made -->

(To be filled by the team)
