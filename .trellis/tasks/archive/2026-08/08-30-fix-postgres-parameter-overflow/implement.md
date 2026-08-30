# Implementation Plan

## Preconditions

- Task remains in `planning` until the user approves the final planning summary.
- Before product edits, load `trellis-before-dev` and the backend database/background-task specs.
- Preserve any user changes if the worktree is no longer clean.

## Implementation checklist

1. Add the focused PostgreSQL regression for the reported failure.
   - Exercise `ListPersonWorkContexts` with more than 65,535 string IDs and assert no pgx parameter-limit error.
   - Keep the test data insertion minimal; nonexistent IDs are sufficient to exercise parameter encoding.
   - Add a focused pre-pass translation-cache assertion if the existing worker tests cannot observe the unbounded cache query.

2. Fix the People translation repository boundary.
   - Replace the unbounded `person_id IN ?` collection with one `ANY` array binding.
   - Replace the three translation-cache collection expansions with array bindings.
   - Do not change grouping, ordering, negative-cache filtering, pass limit, or AI window code.

3. Fix confirmed database/full-table collection paths.
   - Telegram duplicate/inactive binding deletes.
   - Media scrape group claim/reset/synchronization ID predicates.
   - Refresh-token excess revocation.
   - Organizer path existence count.
   - Preserve single-statement updates and existing transaction/row-count checks.

4. Fix external/persisted filters with no hard count bound at their shared SQL boundaries.
   - Emby ID/person filters.
   - Playback-statistics library filters.
   - Allowed/hidden visibility filters and affected media-search predicates.
   - Other media/metadata/person/library/path collection predicates only when the caller audit shows no fixed local upper bound.
   - Use `= ANY(?)` / `<> ALL(?)` with a slice pointer; do not add a helper or input truncation.

5. Bound metadata identifier inserts.
   - Change unbounded `Create(&identifiers)` calls to `CreateInBatches(&identifiers, 500)`.
   - Add or extend one PostgreSQL test so more than one batch is inserted and transaction behavior remains intact.

6. Run a final production-query audit before formatting.
   - List all remaining `IN ?` / `NOT IN ?` and bulk slice `Create` occurrences outside tests and `docs/cankao`.
   - For each remaining non-literal slice, verify a local fixed page/batch/input bound from current code.
   - Do not change fixed enum lists or already bounded 100/500-row paths.

7. Format and run focused verification.
   - Run `gofmt` only on changed Go files.
   - Run focused repository, service, and handler tests covering the changed contracts.
   - If `MEDIASTATION_TEST_POSTGRES_DSN` is absent, record PostgreSQL regressions as skipped rather than claiming they ran.

8. Run the broader affected-package checks.
   - Run `go test ./internal/repository ./internal/service ./internal/handler` if the focused checks pass.
   - Do not start a server; no service shutdown should be necessary.

9. Independent review and convergence.
   - Load `trellis-check` and perform a review separate from implementation.
   - Re-check empty-list behavior, `NOT IN`/`ALL` equivalence, pgx prepared encoding, People ordering/cache semantics, and atomic full-group scrape claims.
   - Repeat focused checks after any review fix.

## Planned validation commands

```bash
rg -n --glob '*.go' --glob '!**/*_test.go' --glob '!docs/cankao/**' '(IN|NOT IN) \?|Create\(&[A-Za-z_][A-Za-z0-9_]*\)' internal

go test ./internal/repository -run 'Test(ListPersonWorkContexts|TranslationCache|Metadata.*Identifier)'
go test ./internal/service -run 'Test(ScheduledPeopleTranslation|MediaScrape|ResetMediaScrape|Telegram|MediaVisibility|EmbyItems)'
go test ./internal/handler -run 'Test(PlaybackStatsFilter|ParseEmbyItemsParams)'

go test ./internal/repository ./internal/service ./internal/handler
```

Test names added during implementation will replace placeholder regex fragments where necessary.

## Risky files and rollback points

- `person_repository.go`: highest-priority incident path; rollback independently if pgx array encoding fails.
- `scrape_worker.go` and `scraper_library.go`: whole-group atomicity and `RowsAffected` checks must not change.
- `media_view_repository.go` and `media_search_repository.go`: visibility and pagination must remain in one SQL query.
- `metadata_repository.go`: batching stays inside the existing transaction; rollback only the two insert calls if batch behavior differs.
- Service/handler direct SQL files: query expression changes must not alter response codes or payloads.

No schema migration or irreversible operation is planned. Every code change is directly revertible.
