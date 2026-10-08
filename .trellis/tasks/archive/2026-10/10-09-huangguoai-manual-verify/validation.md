# Validation

- PostgreSQL 16 isolated schemas, `go test -race ./internal/service ./internal/handler ./internal/repository -run ^TestHuangGuoAI -count=1`: passed (51.058s / 5.400s / 14.678s).
- Additional final manual review/PIN-profile regressions: passed (4.860s / 4.596s).
- Final HTTP acceptance endpoint/audit regression: passed (4.361s).
- Go vet service/handler/repository: passed. Web lint/build: passed. Diff whitespace check: passed.
- Synthetic agent-browser suite: passed, including review preview controls, declined confirmation, version-bound acceptance payload and 390/640/768/1440 layout; existing discovery/download/mixed library suite retained. Browser mocks validate UI/API behavior, not actual media playback.
- Independent read-only review found silent candidate cleanup failure; fixed by cleanup under row lock with recoverable path on failure; regression covers unavailable root and recovery. Main agent reviewed the final flow again.
- Baseline HEAD reproduced the pre-existing unexpected-error regression failure. Resume checkpoint now returns a fixed safe category. Moved error injection to final transfer handoff to keep testing raw database error privacy; full suite now passes.
- No source downloads, production migration, deployment, process restart or queue mutation. Existing failed files already removed cannot be recovered automatically. Live playback/full-story completeness and external upload were not tested.
- Cleanup after deletion followed by DB commit failure may leave a stale pending row referencing a missing file; another retry/cancel resolves it, and confirmation fails safely. Rollback to older binaries must resolve pending_review rows first.
