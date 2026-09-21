# 优化剧集分页与来源存在性查询

## Goal

基于真实执行计划优化剩余 Emby 浏览慢查询，保持筛选、计数、排序和权限语义

## Requirements

- Reduce ordinary series intermediate rows by grouping visible files by season.
- Share the series count/page scan; preserve sorting, permissions, favorites and empty-page totals.
- Keep the fixed NFO source visible to PostgreSQL's prepared query planner.
- No schema changes, caches or changes to hierarchy payload semantics.

## Acceptance Criteria

- [x] Real PostgreSQL pagination and NFO regressions pass.
- [x] One count/page query, selective favorites path retained.
- [x] NFO existence query uses its existing index on a skewed fixture.
- [x] Separate diff review and targeted go vet pass.

## Notes

- PostgreSQL 17 read-only sample: old count alone 756 ms; combined count/page 514 ms. Different samples are not a stable benchmark. Intermediate visible rows dropped from about 188,000 to 4,189 seasons; total remained 3,875 series.
- PostgreSQL 15 isolated fixture: generic source parameter scanned 200,000 unrelated rows; fixed NFO literal used the existing index. This demonstrates a risk, not proof of the historical production latency cause.
- Passed targeted series pagination, NFO startup, hierarchy/state isolation and HasMedia tests on a real temporary PostgreSQL instance; go vet passed for repository/service.
- NFO hierarchy count/list and current-page series summaries remain unchanged; no claim that all logged slow queries are resolved. End-to-end production endpoint latency is not verified.
- No production writes or schema changes. Existing unrelated worktree changes preserved. Pending user-requested commit; do not archive before committing.
