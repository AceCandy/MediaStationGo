# MSGo regression repair

## Goal
Restore service/handler regressions under the existing series-only HongGuo contract.

## Scope
- Task definitions endpoint includes the new HuangGuo AI supplement task (27 visible definitions).
- Counts fixtures create only HongGuo series, retaining fileless, hidden, version and cross-library cases.
- Parallel payload tests use the current album identity.
- Investigate and fix HongGuo page hydration work reads without relaxing plan limits or changing payload semantics.

## Acceptance
- Previously failing tests pass against isolated PostgreSQL, including race checks.
- Real hydration plans stay within existing limits and match hierarchy payloads, including hidden first-season titles.
- Complete handler/repository regressions and focused service regressions cover all affected state-query consumers. Record any broader service failures separately.
- No production database changes, deployment or push. The user authorized committing and archiving this repair after reviewing the results.
