# Remaining pagination regressions

## Goal
Repair the two confirmed preexisting pagination regressions without changing API results.

## Scope
- Ordinary Series favorite query-count assertion: capture the real count/page statements and preserve strict shape, favorite and visibility checks.
- HongGuo download work list: retain filtered work totals, first-current-task ordering, 50-work pages and full eight-status summaries, while avoiding full episode-table scans.
- Do not alter parallel HuangGuo AI work, schema, user permissions or download execution.

## Acceptance
- Both previously failing tests pass with race detection on isolated PostgreSQL 16.
- Download semantic oracle, keyword/status/pagination tests and custom/generic plan checks pass without relaxed limits.
- Independent review, vet and diff checks pass; no diagnostics remain.
- No production database access, deployment or push. The user authorized committing and archiving the reviewed repair.
