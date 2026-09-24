# Implementation and verification

1. Add regression checks for global admission, scanner entry rejection, sequential batches and path-insert races; demonstrate old failures where practical.
2. Replace keyed scan occupancy with one slot; wire all known production admissions, preserve multi-target loops and report busy without creating tasks.
3. Make shared path insertion conflict-safe and recheck resolved-row ownership before updates/bindings.
4. Run focused Go tests against an isolated PostgreSQL 16 instance, relevant `-race` tests, formatting, `git diff --check`, and affected HTTP/client contracts.
5. Independent read-only review, address confirmed findings, rerun affected tests; document verification, limitations and reusable contracts.
6. Stop/remove only task-owned test services and temporary artifacts. Report results and request commit approval; do not deploy or rescan production.
