# Plan and regression evidence

## Ordinary Series
Captured two distinct statements, not duplicate log matches: a `SELECT COUNT(*) FROM ... qualified` and a 50-candidate `LEFT JOIN qualified` page query. Both use persisted work time and the same favorite/user/library predicates. The stale one-query assertion is replaced by an exact two-query assertion plus explicit count/page shape checks; existing favorites, visibility, payload and plan checks remain.

## HongGuo downloads
Baseline page summary used a hash join over a sequential scan of all 300,000 task rows. Correlated page aggregation reads only 50 sources (5,000 task rows). Without a status qualification boundary, cancelled status materialized 15,000 matching tasks (23,000 total visits), and the empty-status join could probe one oldest task before finding no eligible work. Source-correlated status EXISTS with OFFSET 0 preserves short-circuit qualification before timestamp lookup.

Final PostgreSQL 16 custom and generic plans: 8,000 task visits without status, 11,000 for every fixed status, zero for queued-empty. Existing 20,000-visit/no-sequential-scan/empty-no-timestamp-probe limits are unchanged.

Targeted race checks pass for the two original failures, the original download result oracle, work grouping/retry, keyword-before-page filtering, HTTP status filtering and administrator access. Vet passes for service/handler. Full service regressions, external-provider live tests and production plans are outside this focused repair.

Independent read-only review found no issues in the two code/test diffs and the added performance contract; it checked production status/page validation, parameter safety, all-status summaries and empty-page totals. Diagnostics and temporary logs were removed, and the isolated database was stopped before handoff. The user subsequently authorized committing and archiving this repair; no deployment or push is authorized.
