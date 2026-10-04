# Design

Use `hong_guo_download_works` as the work candidate set, with an indexed EXISTS over episode tasks (optionally constrained by status). Materialize candidates once in one statement for exact count and paging; order page candidates by a lateral indexed earliest current task query; keep the existing outer full-status aggregation limited to selected source IDs. Do not substitute placement timestamps for task timestamps or maintain new summary counters.

The existing enqueue/replacement transactions establish placements before task insertion. Tests that directly insert episode tasks must include matching placements; empty placements are explicitly excluded. Runtime consistency was verified before implementation.

Add `idx_hg_download_work_created_c(source_id COLLATE "C", created_at)` and `idx_hg_download_status_work_c(status, source_id COLLATE "C")` in the existing performance-index migration, conditional on the episode table existing. Preserve current claim indexes. Validate in isolated schemas/temp tables; indexes on running application tables are not created during this task. Startup index building is an operational risk to report.

The materialized candidate set includes oldest task timestamps, shared by count and paging. `totals LEFT JOIN summaries` returns an exact total even on empty/out-of-range pages. ID probes use C bytewise matching; final page ordering retains the original collation. The eight fixed known statuses use separate literal SQL templates to avoid generic prepared-plan selectivity degradation; unknown statuses and pagination remain bound parameters.

Visibility listeners in the two existing effects clear pending timers on hide and trigger a guarded immediate load on show. An in-flight request may finish; a single-flight guard prevents overlap and completion only schedules while visible. Cleanup aborts requests, removes listeners and clears timers. No shared generic polling abstraction.

Rollback is reverting these scoped changes; added indexes can remain harmlessly or be dropped separately after rollback.
