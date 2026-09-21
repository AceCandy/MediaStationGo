# Implementation

1. Add failing state/reconciliation tests on isolated PostgreSQL.
2. Add nullable resume state and shared read/write normalization; retain events.
3. Wire every playback-state consumer, preserve pagination and source isolation.
4. Add stale-source report protection, update Web gates and Emby catalog semantics.
5. Run focused Go tests, SQL-plan checks, Web checks and independent review.
6. Record verified/unverified scope; do not deploy or mutate production history.
