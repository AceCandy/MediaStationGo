# Implementation and checks

1. Compare candidate query alternatives with temp copies of the current dataset; no application rows or indexes are changed.
2. Replace ListWorks candidates; add earliest-task and status/source indexes; adjust direct-insert test fixtures and add correctness/plan regressions.
3. Pause both page-owned polling effects on visibility changes and add a focused browser check.
4. Run targeted Go service/handler/database tests with isolated PostgreSQL schemas; run Web lint/build and browser verification against a temporary preview server.
5. Independently review diff/contracts and record measurement limits; remove temporary artifacts and stop the preview server.
