# Verification

## Changes

- Reused the library visible-file scope and correlated `EXISTS (... OFFSET 0)` in HongGuo candidate enumeration/revalidation.
- Resolved final-page logical identities to source work IDs before the existing representative query, using a single bound array for file scope.
- Added `TestHongGuoSearchBoundsFileWork`; no schema, index, frontend, cache, worker or connection-pool changes.

## Automated checks

- PostgreSQL 17 temporary Docker database, isolated per-test schemas: repository candidate lifecycle/visibility, OpenSearch alias/scope, Web ranking, Web three-source search, Emby three-source and played filtering, HongGuo library presentation, HTTP visibility, grouped search paging, and filtered library paging passed.
- The same focused repository/service/handler selection passed with `-race`; `go vet` for these three packages passed.
- `node web/tests/search-source-cards.mjs`, gofmt and `git diff --check` passed.
- The performance fixture has 10,000 works and 40,000 files. All three plan checks fail against the two pre-change repository files through a temporary Go build overlay; all pass against the final implementation. No shared working-tree rollback was needed.
- The fixture retains bounded file-name payloads. All-null narrow rows made PostgreSQL reasonably choose a cheap sequential media scan on the small fixture even with bounded bindings; realistic row widths allow the regression to assert the intended indexed work without changing database planner settings.
- Independent read-only review found no blocking visibility, grouping, ordering or fallback regression. The main thread separately checked the final diff and actual runtime output.

## Real database: read-only, same-snapshot SQL comparison

Connections enforce read-only mode, statement and lock timeouts. No migrations, startup or background workers are started.

| Query | Equal result | Old execution | New execution |
| --- | --- | --- | --- |
| Visible HongGuo identities | 4,811 identical identities | 1,146 ms | 302 ms |
| Three-card representatives | Identical logical/file IDs | 1,279 ms | 46 ms |

Old representative plans processed approximately 517,546 file bindings; new plans only process the 1,433 files belonging to the selected source works. Existence checks use the work-binding index and media primary-key probes. These SQL timings exclude source-ID resolution and the rest of the HTTP request.

## Real Web search handler and OpenSearch

Ran actual `searchMediaHandler` through an in-process HTTP router/recorder, with the current administrator's database visibility and real PostgreSQL/OpenSearch. No API response mocks. Both ordinary and HongGuo search backends made 12 successful calls per build with zero backend errors. Old code was selected using a temporary build overlay, not a deployed-process restart.

Three sequential samples per case; median elapsed seconds:

| Case | Old | New | Result |
| --- | --- | --- | --- |
| 航海王, page 1 / 30 | 1.798 | 1.157 | 14 items / total 14 |
| 天下, page 1 / 30 | 2.966 | 1.763 | 30 items / total 100 |
| 天下, suggestions / 8 | 1.095 | 1.079 | 8 items |
| No-match keyword, page 1 / 30 | 1.321 | 0.993 | Empty |

The first new-code diagnostic also passed all 12 requests. Old/new builds ran sequentially under ongoing application load; these are samples, not controlled load-test guarantees. Suggestion latency changed little in this sample. PostgreSQL revalidation remains a measurable cost.

## Root cause and prevention

- Categories: change propagation failure and test coverage gap. Work-level existence/paging optimization existed in the library path but was not propagated to the Web search path.
- Previous functional/mocked UI tests established correct results but did not bound database work. The new plan test exercises actual generated SQL, not a hand-maintained query copy or only method/result names.
- The existing HongGuo spec now requires the shared existence boundary, final-page work-ID restriction, parameterized plan testing and a distinction between mock tests, real backend measurements and deployment verification. This application repository has no corresponding specification template tree to sync.

## Boundaries, cleanup and remaining risk

- No full Go suite, frontend lint/build or deployed browser/network/authentication-path performance test was run. Frontend code is unchanged by this task.
- Existing business service was not restarted. New code requires the normal build/restart/deployment to affect that process; actual deployment latency remains unverified.
- Real-data timing used an independent read-only connection, not the live process's connection pool. Background I/O/CPU/pool contention and catalog size can still affect latency; this task does not eliminate every search cost.
- Temporary diagnostic test sources and baseline overlay copies were removed, and the task-specific PostgreSQL container was stopped/auto-removed. Permanent plan regression remains.
- Parallel task-center work was preserved. Its transient missing handler symbol initially blocked handler compilation; retry after the parallel file appeared passed without edits to that task.
- The user requested code submission and task archival on 2026-09-24; this record retains the verification evidence. Deployment, restart and remote push remain outside that request.
