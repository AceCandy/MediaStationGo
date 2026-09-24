# Verification

## Changes

- Web keyword pages and suggestions run HongGuo and ordinary/NFO recall concurrently. The shared repository also overlaps ordinary recall/revalidation with NFO lookup, so NFO does not wait for either OpenSearch backend.
- Request-local `context.WithCancelCause` preserves the originating failure, cancels siblings and joins started work before returning. Successful empty index results do not become PostgreSQL fallback.
- Ranking, visibility, index contents, source identities, final-page hydration and existing source-specific candidate/page limits remain unchanged. Only the Emby test query counter needed atomic access; Emby-specific orchestration was not changed.
- No dependency, cache, schema, index rebuild, production connection-pool or frontend change.

## Automated checks

- Isolated PostgreSQL 17: the final targeted repository/service/handler selection passed with `-race`. It covers the three new Web concurrency/cancellation/pagination tests, existing three-source Web and Emby behavior, played filtering, HongGuo index visibility and alias scope, ranking/cap/page behavior, large/empty-query Web pages, NFO projection, HongGuo library/HTTP presentation and the prior bounded-file execution plan.
- `TestWebSourceSearchParallel` first failed on the fully serial implementation in all three modes (indexed, backend fallback, PostgreSQL), observing only HongGuo before the barrier timeout.
- Repeating the test with the new Web service but the old repository through a temporary Go overlay also failed in all three modes: ordinary and HongGuo entered, NFO did not. This verifies that merely parallelizing two outer branches is insufficient.
- `TestWebSourceSearchParallelPagination` passed both the original two production files through an overlay and the new implementation. It checks 100-result HongGuo merging, preserved 200-result ordinary/NFO-only pages, empty/no-hit queries and successful empty ordinary-index behavior.
- `TestWebSourceSearchParallelCancellation` covers parent cancellation and a terminal NFO database failure while both index backends are blocked; it asserts the original error and both backends stopped before return.
- The final non-race run of the same targeted repository/service/handler selection also passed. `go vet ./internal/repository ./internal/service ./internal/handler`, `node web/tests/search-source-cards.mjs`, gofmt and tracked/untracked diff whitespace checks passed.

## Independent review

- Separate read-only reviews checked production concurrency/cancellation/pagination semantics and test isolation/assertions. No blocking production issue was found.
- The test reviewer noted unbounded cleanup waits. These now have explicit timeouts, and barrier sends respect cancellation; the final targeted race run passed after the changes.
- The main thread rechecked the final production diff, originating-error propagation, no-HongGuo page slicing and the real handler outputs independently of the reviewers.

## Real backend read-only comparison

Executed the real `searchMediaHandler` using an in-process router/recorder and the first stored administrator's actual visibility. PostgreSQL enforces `default_transaction_read_only=on`, statement/lock timeouts on every connection, and a separate pool of at most three connections. No application startup, migration or background worker was invoked. Both ordinary and HongGuo OpenSearch backends made 16 successful calls per build, with zero errors/fallbacks.

For each case, one warmup and three measured requests; median elapsed seconds:

| Case | Serial baseline | Parallel | Result comparison |
| --- | --- | --- | --- |
| 航海王, page 1 / 30 | 1.201 | 1.162 | Identical ordered IDs and total |
| 天下, page 1 / 30 | 1.781 | 1.307 | Identical ordered IDs and total |
| 天下, suggestions / 8 | 1.236 | 1.049 | Identical ordered IDs |
| No-match keyword, page 1 / 30 | 0.966 | 0.923 | Identical empty result |

The baseline is the committed serial orchestration, including the prior HongGuo file-query fix (`45ee9d1`). The two baseline source files were selected with a Go build overlay; the shared working tree and running application were never rolled back or restarted. Samples ran sequentially, parallel build first and baseline second, under ongoing external load; this is not a controlled production-load comparison. Improvements in the first and last cases are small relative to observed sample variability. No universal speedup is claimed.

## Bug analysis and prevention

### 1. Root cause category

Implicit dependency and test-coverage gap: unrelated sources were invoked sequentially, including NFO lookup nested after ordinary recall. Correct result tests did not establish overlap. The earlier file-query optimization addressed a different cost and remains necessary.

### 2. Validation issues encountered

The new bulk fixture initially omitted required `source`, binding `fingerprint` and embedded binding `title` values. This was a fixture construction error, not a production search failure. Checking the complete model, including embedded fields, resolved it; hand-written fixture inserts must not assume GORM's zero-value writes happen automatically.

### 3. Prevention mechanisms

The permanent three-mode barrier test detects both serial layers; cancellation tests cover join and error provenance. Schema-pinned connections survive reconnects, and the existing query counter is atomic. The HongGuo search spec now records these contracts and the previously implicit distinction between the two pagination caps.

### 4. Systematic expansion

Shared ordinary/NFO callers, including Emby, were regression-tested. Emby-specific source orchestration, index permission fields, duplicate candidate-detail hydration and production resource tuning remain outside this task; they are not silently redesigned here.

### 5. Knowledge capture

Updated the existing independent-search scenario in `.trellis/spec/backend/hongguo-catalog.md`. This application has no corresponding specification template tree to synchronize. Spec changes remain uncommitted with the task, respecting the request boundary.

## Unverified and remaining risk

- No full Go suite or frontend build/lint/browser run. Frontend code is unchanged.
- No deployed-process, network/authentication-middleware or sustained-load performance verification. The HTTP handler used real visibility/data but no external HTTP listener or login flow.
- Increased simultaneous database work can affect throughput or latency under contention. This implementation does not eliminate remaining visibility enumeration, candidate revalidation or metadata hydration costs.
- No service restart/deployment, remote push, commit or archive was performed. The new code still requires normal deployment to affect the running process.
- Temporary diagnostic sources and overlay files were removed; the task-owned PostgreSQL container was stopped and auto-removed. No task-owned service remains running, and the existing application was left untouched.
