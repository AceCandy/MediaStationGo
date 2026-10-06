# Implementation

1. Add stable per-work summary repository operation using the existing source-reference field.
2. Apply stable summaries to both HongGuo and HuangGuo AI (including Movie); separate episode diagnostics from history; refresh committed lifecycle and restart boundaries.
3. Run isolated PostgreSQL uniqueness/concurrency/state/retry/supplement/recovery/failure and HuangGuo safe upstream-error tests and existing download/tracker/history regressions, including race checks.
4. Prepare and verify bounded historical compaction with explicit cutoff and guarded deletion; never alter business queues.
5. Independently review, update affected specs and report validation, operational results and rollout limits.
