# Validation

## Change boundary

Only ordinary Movie/Series sorting that originally contains MAX(media.created_at) changes pagination strategy. It keeps original candidate/order and exact eligibility SQL, materializes both once, then reuses workCandidatePage with an explicit count switch. Name/recent/rating/random sorts retain batches. The existing HongGuo call explicitly retains count=true. No API payload, global JIT, index, cache, schema or deployed-service change.

## Regression evidence

TestEmbyFileDateSortQualifiesOnce failed on both old Movie/Series paths because a counted request issued two candidate statements instead of one. The new regression compares complete selected views/summaries with old file-group oracles for default/DateCreated/PremiereDate, both directions, count on/off, offsets including empty pages, 150 empty candidates before 10 file-backed works, two file versions, hidden-library newer files and favorites.

Initial PostgreSQL checks passed: TestEmbyItemsCountModes, TestEmbySeriesDenseCountPlan, TestEmbyWorkBatchContinuesAndCounts (all four sources), TestEmbyKnownMovieMembershipSkipsFileQualification, TestEmbyMetadataWorkPageMatchesFileGrouping and the initial sparse sort regression. Expanded checks include three Movie-library semantics tests, Series grouping/version counts, catalog probe plans, optional HTTP totals/cache/lookahead and the expanded regression. Existing count instrumentation required recognizing the new count statement; the DateCreated favorite plan expectation changes from two statements to one. Database tests use isolated random schemas; no skipped tests count as validation.

## Actual production data, read-only

Before: foreign Movie uncounted default 1252.7–1338.9 ms (five batches), DateCreated 2057.4–2101.5 ms (nine batches). Recent 186.8–204.3 ms. These are direct service methods, not HTTP latency.

After, same request shape (explicit Movie/Series, descending, page size 50, no whole-page cache). Repeated warm method calls after each initial measurement:

| Library | Default, no count | DateCreated, no count | Default, accurate count | DateCreated, accurate count |
| --- | --- | --- | --- | --- |
| Foreign Movie | 462.0–470.9 ms | 456.9–471.6 ms | 458.0–466.6 ms | 456.6–457.6 ms |
| Small Movie | 141.2–148.3 ms | 171.2–179.6 ms | 135.7–141.0 ms | 162.5–168.2 ms |
| Series | 279.6–294.5 ms | 282.5–290.3 ms | 284.4–292.0 ms | 282.6–303.4 ms |

Each changed request emits one candidate/page SQL. Recent remains on two batches with lookahead: foreign Movie 166.7–169.5 ms, Series 87.9–88.8 ms. Initial calls have higher cache/startup overhead; do not label warm samples as cold benchmarks.

Old source copied temporarily for a same-connection read-only repeatable-read comparison. Old/new selected Movie views or Series summaries and exact totals match. Actual old full count SQL was replayed as ordered qualified IDs and new page limit expanded using original bound parameters. Complete ID/order comparison matched both sorts in all three libraries: foreign 8499, small 1208, Series 893. The live foreign catalog earlier measured 8500; both implementations return 8499 in this later common snapshot, so do not assert a historical total stayed fixed during live ingestion.

EXPLAIN ANALYZE BUFFERS with timing and actual generated SQL: foreign default 406.556 ms / DateCreated 393.009 ms; small 74.819 / 62.175 ms; Series 243.177 / 235.913 ms. All six plans contain no JIT and costs below 11,000. No forced planner setting needed. Plan measurements include instrumentation overhead and separate cache conditions from method timings.

## Limits and cleanup

No full repository suite, deployed HTTP or native client verification. Necessary all-candidate date sorting and qualification still scale with eligible catalog size; the remaining work is not constant-time. Temporary verification source files are removed by finally blocks; no service was started or production writes performed. Independent read-only review found no blocking issue. Final gofmt and git diff --check passed; temporary source cleanup verified. Quality gate passed; user approved the concrete commit and archive plan.

Final result: 13 distinct targeted PostgreSQL regressions passed (including the expanded sparse-sort test, catalog plan/favorite/person checks and HTTP optional total/cache/lookahead). A temporary fixture omitted required favorites.media_id and was corrected; no production code change was required for that failure. Tests tracking old SQL/count-page split were updated to recognize the new single-query shape, while preserving response, file-visit and permission assertions.
