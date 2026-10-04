# Validation

## Scope

Only ordinary Web Movie candidate eligibility and the Movie count/page statement changed. Exact totals, version grouping/preference, global work-time order, Part minima, visibility and work filters remain required. No global JIT setting, index, cache, API field, deployment or service startup.

## Database regression

Real PostgreSQL, isolated random schemas in the postgres test database; no skipped database tests. The eligibility metadata-visit assertion failed with the old query (520 visits for 440 catalog entries) and passes after removing redundant eligibility joins.

Service checks passed: Series direct attachments/ties, Movie version/order/file bounds, Movie global work time and cross-work Parts, Series season-scan bounds, scoped Series episode projection, metadata pagination/filter/permission bounds, grouped visible media pagination. Handler checks for default version grouping and empty-library array responses also passed (their minimal fixtures emit expected missing-table lookup logs). New checks cover unknown membership library visibility and all eight combinations of prior JIT on/off, standalone/nested transactions and success/actual SQL failure. The callback observes JIT off at the Movie statement; subsequent queries observe the original value after success or rollback.

## Production read-only measurements

Before: foreign Movie library HTTP about 7.69 s; actual generated SQL about 7.39 s, including 7.09 s JIT. With JIT disabled, 301–307 ms, approximately 291 ms candidate eligibility and 8 ms materialization/count; total counting itself is not the principal cost.

Eligibility simplification alone: SQL 5744.594 ms, JIT 5613.443 ms; hence local JIT control remains necessary.

After both changes (actual repository call, page size 50):

| Library | Exact total | Method calls after initial plan measurement | Median |
| --- | --- | --- | --- |
| Small Movie | 1208 | 55.751 / 55.565 / 54.407 ms | 55.565 ms |
| Foreign Movie | 8500 | 149.286 / 130.398 / 131.072 ms | 131.072 ms |

EXPLAIN ANALYZE on the actual transaction connection, with generated SQL and original bound parameters: small 91.704 ms, foreign 204.400 ms; neither contains JIT. These initial EXPLAIN runs and repeated method timings have different cache conditions; do not treat them as identical benchmark samples or HTTP latency.

Before local JIT wrapping, old/new full result comparison in the same read-only repeatable-read snapshot matched all 1208/8500 work identities, order, representative file, Count, VersionCount and Total. The transaction wrapper does not alter the main SQL.

## Limits

No deployed HTTP/UI test or full repository test suite. Performance remains dependent on database load, cache and network. Accurate totals still require eligibility for all matching works. No production writes were performed; temporary source/SQL/plan files are removed at completion.

## Independent review and cleanup

Independent read-only review found no blocking issues; checked candidate/file visibility, original Part scope, PostgreSQL setting rollback and GORM nested savepoint behavior against source. gofmt and git diff --check passed. Task-created temporary source, SQL and plan files removed. No service was started. Quality gate passed; user approved the concrete commit and archive plan.
