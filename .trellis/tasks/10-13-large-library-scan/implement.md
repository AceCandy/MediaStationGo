# Implementation

1. Add timings and bounded fingerprint traversal with attribute reuse.
2. Page prune candidates, use bounded name cache, and narrow part reconciliation. Add index.
3. Add HongGuo short batch transaction/cache with rollback fallback.
4. Add regression/query-bound tests and a repeatable benchmark; run focused PostgreSQL tests/race and review independently.
5. Record actual validations and remaining large-library deployment limits.

Approval: user accepted the previous final plan with “可以 改吧”. Main agent implements; subagents only explore/review.

## Verification so far

- Focused PostgreSQL scan/watcher/NFO/repository/handler/index regressions passed.
- Independent review found a concurrent Added/Updated counting window; repository batch now returns actual creation flags, with synchronized multi-connection coverage.
- Attribute microbenchmark (1000 files, 3 runs): legacy 4.98–5.09 ms and ~846 KB/op; reuse 2.84–3.06 ms and ~557 KB/op. Does not measure total library scan.
- Focused PostgreSQL race regressions passed in service/repository/handler/database, including synchronized batch creation counts. `go vet` and `git diff --check` passed.
- Final independent review found no further confirmed issues. No production database or library scan performed.
- Existing unrelated dirty work was preserved; changes remain uncommitted.
