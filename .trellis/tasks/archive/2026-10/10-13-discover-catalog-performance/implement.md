# Execution
1. Establish generated projections and indexes; replace only list/category query expressions. Test existing rows, updates, repeated schema setup and 10k large-payload plans in isolated PostgreSQL.
2. Share bounded continuation for TMDb/Douban category grids; preserve exact API paging/permission contracts; compact source mode nav.
3. Browser regression, lint/build, focused Go tests, independent read-only review; update contracts and record limitations.


## Verification results
- Independent backend/frontend read-only review found no confirmed defect; added dialog-node preservation browser assertions and prepared-runtime write verification in response to coverage notes.
- PostgreSQL tests passed: generated projection existing rows/writes, actual index plans (10k large payloads), repeated full current-schema migration, migration/runtime connection split, all matching repository and handler Douban/catalog tests. Focused service poster/read regression passed.
- Broader service Douban regression has one pre-existing failure: TestDoubanDataRefreshOnlyChangesSharedData expects TaskSystemCommon although current source task belongs to TaskSystemDouban. Reproduced using the HEAD test file via a temporary Go overlay without this task's schema setup. Left unrelated assertion unchanged.
- Browser checks passed: check-catalog-continuation (both sources, people, 1→2 failure→2 retry→3, dedup, stale abort, dialogs preserve list nodes, compact tabs, mobile/desktop themes); check-discover-centers (viewer/admin permissions, source isolation, details); check-douban-data-center (posters, refresh, failure, session isolation, 390–1440px themes).
- Typecheck, lint, production build and whitespace checks passed.
- Same 8670-record snapshot copied read-only into an isolated temporary PostgreSQL: all six result sets identical. Before/after SQL ms: ordinary page 279.012/0.064; genre count 584.695/1.253, page 611.247/1.113; title search count 424.072/3.749, page 429.290/4.120. Initial generated columns/indexes took 1.715 seconds in isolation. These are SQL timings, not end-to-end HTTP guarantees.
- No user service restart, production schema write or upstream refresh. Real deployment/startup on the user's service is pending their restart. User requested commit and archive after reviewing these results.
