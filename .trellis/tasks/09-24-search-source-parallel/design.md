# Design

## Boundaries and existing behavior

- `internal/service/media_search.go:55` owns Web keyword pages and suggestions. It currently runs HongGuo first, then ordinary/NFO, and hydrates the final page. Empty-query browsing is separate and must remain unchanged.
- `internal/repository/media_search_repository.go:139` prepares ordinary/NFO visibility and chooses ordinary OpenSearch or PostgreSQL fallback. `rankMetadataSearchIDs` at line 391 revalidates ordinary candidates, then queries NFO, then performs their existing merge/rank/cap/page.
- `internal/service/emby_search_items.go:52` also consumes this repository path. Its parent/person/playback-state routing and separate HongGuo orchestration are compatibility boundaries, not new optimization scope.

## Minimal concurrency shape

1. In the shared non-empty keyword path, separate ordinary candidate recall/revalidation from NFO candidate lookup. Start those two independent operations after shared filter preparation, then retain the existing ordinary/NFO merge, ranking and candidate limits.
2. In Web search, start HongGuo recall alongside that ordinary/NFO path. After both finish, retain Web's existing final merge, ranking, total and page projection. Do not hydrate ordinary/NFO cards speculatively while HongGuo is still deciding the final page, and do not rerun ordinary/NFO search when HongGuo is empty.
   Request enough already-ranked ordinary/NFO IDs for the requested page and the existing 100-candidate HongGuo merge. Preserve the repository total and slice the requested page if HongGuo is empty; otherwise retain only the original first 100 ordinary/NFO candidates before the existing merge. Inspection found that ordinary/NFO-only totals can reach 200: the earlier shorthand "100-result cap" must not be used to change this behavior.
3. Use bounded request-local standard-library concurrency (`context` and `sync`), with separate result/error variables per branch and a join before result access. No worker pool, configurable fan-out, generic source registry, additional dependency or per-candidate goroutine is needed.
4. Resolve visibility before launching dependent work; never mutate shared filter slices, GORM statements, result slices or request state concurrently. Create independent query statements through the existing query builders.

This creates three overlapping source pipelines; dependencies within a source remain ordered. It does not change NFO into an OpenSearch-backed source or remove HongGuo's pre-limit visibility enumeration.

## Error and cancellation contract

OpenSearch failures keep their current per-source PostgreSQL fallback. A terminal database error cancels outstanding sibling work; wait for started branches to finish before returning an error. Never convert a failed source into a successful partial search. Preserve the originating failure rather than replacing it with a sibling's cancellation, and propagate parent cancellation. Final-page hydration uses the request context after recall succeeds.

## Index, presentation and rollback

No search index/schema changes or rebuild are required. HongGuo still indexes one logical official album using the earliest stored season title (`internal/repository/hongguo_groups.go:14`, `internal/repository/hongguo_search_index.go:15`); any visible member season can make it searchable. Preserve standalone/movie identities, title-only HongGuo matching, ordinary/NFO Web field matching, and current final-page file-query bounds.

Rollback is reverting the task's service/repository changes; no data migration or cleanup is needed. Do not change production connection limits to manufacture concurrency benefits.

## Verification and risks

- Deterministic overlap checks block the independent search backends and observe NFO query progress before releasing them; the old serial implementation must fail. Exercise OpenSearch and PostgreSQL-fallback orchestration. Timeouts are deadlock guards, not latency assertions.
- Use the existing functional fixtures for ID/order/total, cap/page, hidden/locked scope, first-season presentation, empty/no-HongGuo results and Emby compatibility. Add error/cancellation completion checks and run the focused suite with `-race`.
- `internal/testdb/postgres.go:40` currently limits the test pool to one connection and sets `search_path` on that session. Concurrency/cancellation fixtures need task-local schema-pinned connections, including reconnects; do not merely raise that pool limit or modify the shared test helper broadly.
- Existing test doubles and query-capture callbacks may need synchronization only where newly concurrent access is introduced. A callback race must not be dismissed as harmless test instrumentation.
- Parallelism removes serial waiting but can increase instantaneous database/CPU/I/O demand. Compare identical results and safe repeated real-backend samples if available; no fixed speedup, browser latency or production-load guarantee is implied.
