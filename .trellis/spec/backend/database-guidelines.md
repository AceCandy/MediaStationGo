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

### Rebuild GORM Statements After Aggregation

Do not reuse a `*gorm.DB` statement after adding `Select`, `Group`, `Order`,
`Limit`, or `Offset` for one result shape. Those clauses can remain on sibling
queries and produce invalid SQL or silently wrong results. Keep shared filters
in a small query-builder method and call it separately for counts, aggregates,
details, and rankings. A real PostgreSQL test must execute every branch.

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
- For `.strm`, the fingerprint belongs to the local sidecar; playback-target size belongs to `media_probe_metadata.size_bytes` and must not be reused as the scan fingerprint.
- The unchanged return occurs before media Upsert and probe scheduling, preserving `media.updated_at` and probe data.
- Post-scan automatic STRM generation skips media whose source path or container is already `.strm`; skipped source paths remain protected from overwrite cleanup.
- Every media deletion uses `Unscoped().Delete`; `media.deleted_at` remains only as a compatibility column.
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
- Run migration twice and assert historical soft-deleted media stays absent while `media.deleted_at` remains.

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
