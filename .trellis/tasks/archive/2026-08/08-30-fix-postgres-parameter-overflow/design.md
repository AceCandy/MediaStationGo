# Design: PostgreSQL Parameter Overflow Protection

## Problem

PostgreSQL's extended protocol allows at most 65,535 bind parameters per execution. GORM expands a slice passed to `IN ?` into one parameter per element, so production collections derived from full tables, background work groups, stored filters, or request filters can exceed the protocol limit.

The current incident occurs before the People translation pass limit is applied. A fix must preserve the existing post-grouping 1,000-item contract rather than move that limit earlier.

## Design goals

- Keep each affected operation as one logical SQL statement where its current semantics depend on global ordering, pagination, counts, or atomic whole-group updates.
- Reduce unbounded collection bindings from O(N) parameters to O(1) parameters.
- Preserve result shape, order, deduplication, error propagation, transaction scope, and row-count conflict detection.
- Avoid new dependencies, database protocol changes, generic batching frameworks, and unrelated SQL refactors.

## Selected mechanism

### Collection filters

Use PostgreSQL array membership with a pointer to the existing Go slice:

```go
// Before: GORM expands ids to N bind parameters.
q.Where("id IN ?", ids)

// After: GORM binds one pointer value; pgx encodes the dereferenced []string as an array.
q.Where("id = ANY(?)", &ids)
```

Negative membership uses:

```go
q.Where("library_id <> ALL(?)", &hiddenLibraryIDs)
```

The code does not add an array helper. The pointer is the smallest mechanism supported by the pinned GORM/pgx stack:

- GORM v1.25.7 expands slice/array values but does not expand a pointer value.
- pgx v5.4.3 accepts arbitrary named values, dereferences pointers during encode planning, and encodes `[]string` as a PostgreSQL array.
- The affected identifiers are `varchar`/text values, so PostgreSQL infers compatible array types from the compared column.

The existing `len(slice) > 0` guards remain. Where a method already defines empty-list behavior, that behavior is preserved.

### Bulk inserts

Replace unbounded identifier inserts with GORM's installed batching support:

```go
tx.CreateInBatches(&identifiers, 500)
```

No batch-size setting or helper is introduced. The value follows existing project database batches and leaves a wide margin below 65,535 parameters.

## Affected boundaries

### Required incident path

- `PersonRepository.ListPersonWorkContexts`
- `PersonRepository.ListTranslationCaches`

Both switch collection predicates to array membership. The service's grouping, negative-cache filtering, 1,000-item pass limit, and 100-item AI windows remain untouched.

### Database/full-table derived operations

- Telegram duplicate/inactive binding deletion.
- Media scrape group claim, reset, and post-scrape synchronization.
- Refresh-token excess revocation.
- Organizer destination-path existence checks.
- Metadata identifier bulk creation/upsert.

### External or persisted filters without a hard count bound

Switch the shared SQL boundaries used by:

- Emby `Ids` / `PersonIds` filtering.
- Playback-statistics `library_ids`.
- Allowed/hidden library visibility filters.
- Media/metadata/person/library ID filters that receive the above collections or full-table/background collections.

Implementation uses a final caller audit before changing each occurrence. A method with both bounded and unbounded callers is fixed once at the shared SQL boundary. Literal enum lists and collections protected by a local fixed page/batch remain unchanged.

## Semantic preservation

### People translation

`= ANY(array)` changes only parameter transport. The SQL retains its `ORDER BY`, so `personKnownFor` still receives each person's newest works first. `ListTranslationCaches` retains its existing independent kind/context/source filters and negative-cache behavior.

### Media scrape claim

The complete group remains one conditional `UPDATE` inside the existing transaction. `RowsAffected == len(group.MediaIDs)` remains the conflict check. Per-batch claim loops are intentionally rejected because they could partially claim a group or expose it to another worker.

### Visibility and pagination

Array membership stays inside the original query, before its original `ORDER`, `LIMIT`, and `OFFSET`. Splitting a paginated query into multiple SQL calls is intentionally rejected because merging pages would change results.

### Telegram and refresh-token updates

Each cleanup remains one update/delete statement. Returned row counts and existing error handling remain unchanged.

## Alternatives rejected

### Move the People pass limit before repository queries

Rejected because the established limit applies after grouping, deduplication, and negative-cache filtering. Moving it can repeatedly select negative-cache entries and starve later pending work.

### Hand-written 500-item loops everywhere

Rejected for selectors, pagination, and atomic claims because loops require result merging, global re-sorting, deduplication, or transaction choreography. Array binding is smaller and preserves one-statement semantics.

### Switch runtime PostgreSQL to simple protocol

Rejected because project database contracts require prepared runtime statements, the extended-protocol limit would only be hidden, and very large interpolated SQL would remain.

### Add a generic array/batch utility

Rejected because `&slice` and `CreateInBatches` already provide the complete mechanism. A wrapper would add a new concept without reducing the changed logic.

## Compatibility and migration

- PostgreSQL is the only supported runtime database, so `ANY`/`ALL` do not introduce a cross-dialect compatibility requirement.
- No schema or data migration is required.
- No API response, task status, scheduler, or configuration contract changes.
- Rollback is a direct reversal of the query expressions and batch insert calls.

## Verification strategy

1. Add a real PostgreSQL regression that sends more than 65,535 person IDs through `ListPersonWorkContexts`; it must complete without the pgx parameter-limit error.
2. Exercise the unbounded pre-pass `ListTranslationCaches` shape and retain cache/negative-cache functional tests.
3. Exercise a metadata identifier insert spanning more than one 500-row batch.
4. Run existing focused tests for People translation, media scrape atomic group claim/reset, visibility, Emby filters, playback stats, organizer path checks, and Telegram commands; add the smallest missing focused tests for changed behavior.
5. Statically list remaining production `IN ?` / `NOT IN ?` expressions. Each non-literal collection must have a local fixed upper bound recorded in the audit; fixed enum lists remain.
6. Perform an independent review after implementation, with special attention to empty arrays, `NOT IN` equivalence, prepared pgx encoding, and scrape-group atomicity.

## Deferred risk

`applyMetadataSearchLIKEFilter` creates explicit scalar predicates from free-text variants rather than expanding one collection placeholder. Addressing its theoretical parameter growth requires a product decision about maximum search input or a different search-expression model. It remains documented but is not mixed into this collection-binding repair.
