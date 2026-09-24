# Optimize HongGuo web search queries

## Goal

Align HongGuo search with work-level visible-file existence and current-page representative queries, preserving three-source search behavior

## Requirements

- Preserve ordinary, NFO and HongGuo search, current matching/ranking, 100-candidate cap, pagination and suggestion behavior.
- HongGuo candidates must check current library visibility through indexed file existence, without expanding every episode/file.
- Load representative files only for final-page source works, preserving official album/first-season presentation and file identity.
- Retain independent OpenSearch, pre-limit permission scope, PostgreSQL revalidation and fallback.

## Acceptance Criteria

- [x] Real PostgreSQL regressions cover candidate visibility, album/standalone/movie presentation, pagination and backend fallback.
- [x] An execution-plan regression fails the former whole-file queries without depending on wall-clock thresholds.
- [x] Real-database read-only comparisons preserve results and demonstrate reduced file work; verify the Web search handler with real queries where safe.
- [x] Independently review the diff; remove temporary diagnostic files and stop any temporary services.

## Notes

- User approved the final proposed scope with “ok” on 2026-09-24, including task creation and implementation.
- Prior evidence: visible-ID queries took 1.45–1.70 s in application logs; a three-card representative query took 3.03 s and processed about 516k bindings.
- Out of scope: frontend changes, index/schema redesign, caches, connection-pool tuning, background worker changes, production restart, commit or archive without further request.
