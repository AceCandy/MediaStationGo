# Design

Use a deterministic UUID from task kind and source ID, protected by the existing task primary key. Existing SourcePath stores the source reference hongguo://ID / huangguoai://ID for restart reconciliation; no schema change or secondary index, preserving runtime prepared statements. Serialize each work refresh using a PostgreSQL transaction advisory lock, then read only that work's queue and upsert the same summary. Pending or active episodes mean running; all completed means completed; terminal failures mean failed; cancellation/removal means interrupted.

Refresh after committed queue operations and worker entry/exit. Summary errors are logged without affecting business results. Per-episode TaskHandle writes safe file logs only: no persistence, active entries or per-episode SSE. Generic tasks remain unchanged. Restart reconciles interrupted work summaries; business queues remain the recovery authority.

Compaction scans verified directory source-ID markers once, groups primary keys and uses bounded per-work transactions with a fixed cutoff and ended legacy rows only. Existing queue determines state; queue-less history gets an explicit removed/historical summary rather than fabricated completion. Preserve running, unrecognized, unrelated and new records. Old-service writes persist until rollout; report this limitation.

HuangGuo AI reuses the same internal state reducer and advisory-lock/upsert rules, with its own indexed queue and namespaced identity. Names contain source ID only, and summary paths do not expose source titles. Both pools keep existing business semantics; startup reconciliation runs once and joins during shutdown. No HuangGuo history exists in the inspected live database.
