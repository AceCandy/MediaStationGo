# Implementation

1. Add PostgreSQL regression tests for revision mutation, tombstones, exact zero/short progress, first-write races, conflict rollback and favorite isolation.
2. Register the revision model and idempotent triggers; implement state-first CAS and coherent effective-state reads.
3. Add authenticated read/write handlers and catalog documentation, with permission/source validation tests.
4. Run targeted real PostgreSQL tests and related existing progress tests, then independent review. Continue client integration in the paired LinPlayer task.
