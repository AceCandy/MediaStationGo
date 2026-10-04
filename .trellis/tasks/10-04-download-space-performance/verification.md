# Verification

## Checks completed
- Targeted Go checks: `go test ./internal/service ./internal/handler ./internal/database -run '^TestHongGuoDownload|^TestEnsurePerformanceIndexesCreatesHotPathIndexes$' -count=1` passed using isolated PostgreSQL schemas (service 50.110s, handler 0.175s, database 3.523s). Opt-in upstream/GPU live checks were not enabled.
- Web `npm run lint`, `npm run build` and `git diff --check` passed.
- `node scripts/check-download-space-polling.mjs` passed against an independent preview. The browser test drives visibility properties/events and holds a request to prove single flight; it also checks collapsed listener cleanup.
- A separate read-only reviewer found no material issues. Main review confirmed candidate equivalence, earliest-current-task ordering, whitelist/binding safety and effect cleanup.

## Final plan regressions and snapshot
The final targeted Go service/handler/database checks passed after replacing fixed status parameters with eight whitelisted SQL templates. The 300,000-task/3,000-placement fixture covers every filter with nonempty results under ordinary and forced generic prepared plans, plus empty queued. It requires one statement, no episode sequential scan, at most 20,000 episode visits, and no oldest-task probe for an empty status. An additional zero-episode-visit assertion was added following independent review and rerun separately.

Copied 1,332,641 episode rows and 16,024 placement IDs into an isolated UNLOGGED schema, with original source/episode indexing plus the proposed C indexes. Vacuum/analyze preceded measurements. Three EXPLAIN ANALYZE samples per filter and plan mode; medians below are database execution time with warm buffers, not HTTP/browser latency. No application rows/indexes were modified; the schema was dropped in finally.

| Filter | Custom plan ms | Generic plan ms |
| --- | ---: | ---: |
| All | 150.86 | 157.44 |
| Completed | 155.78 | 162.78 |
| Failed | 12.26 | 11.52 |
| Downloading (empty) | 0.14 | 0.16 |
| Verifying (empty) | 0.13 | 0.12 |
| Publishing (empty) | 0.11 | 0.12 |
| Waiting verify (empty) | 0.13 | 0.13 |
| Queued (empty) | 0.12 | 0.13 |
| Cancelled (empty) | 0.12 | 0.17 |

For every filter, exact totals, full summary rows (bidirectional EXCEPT), and source order matched the original global-grouping query on the same snapshot. Current data has only completed and failed tasks; nonempty coverage for the other six statuses comes from the synthetic regression fixture.

Independent read-only review found no production correctness issue. It requested stronger empty-plan assertions; main review added zero episode-row visits, supplementing the existing no-oldest-probe and no-sequential-scan checks. Bytewise equality is compatible with validated numeric IDs and the inspected deterministic database collation; visible ordering is unchanged.

## Remaining operational limits
- Application backend was not restarted and its indexes were not changed; the running backend still uses the previous query.
- Index creation occurs in the existing startup migration and can delay startup/block writers temporarily. This must be accounted for before applying to the running database.
- No production end-to-end page timing, fresh-disk benchmark, upstream download or physical GPU verification was performed.
- The user approved committing and archiving the completed implementation. Deployment remains a separate action.

## Default-filter follow-up
Absent Web status now defaults to downloading from the first request. Explicit empty status represents all states and is retained through normalization, pagination and reload; existing named status URLs retain their selection. Source-based parameter checks cover absent, empty, failed and completed values; Web lint/build and diff checks passed again. Production browser timing remains unverified.
