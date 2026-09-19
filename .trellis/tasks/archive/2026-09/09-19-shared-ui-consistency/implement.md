# Execution

1. Add a runnable browser regression for cross-page tab styles and input/button aliases; establish the current failure.
2. Add shared tab CSS and replace local styling in Settings, DiscoverHub, Me, Libraries, Tasks, AutoOrganizeSettingsPanel and DownloadSpace.
3. Reuse `.data-table` in AdminUsersTable, FileEntriesTable, SchedulerPage, StoragePage, TasksPage, APIConfigsPanel and ManualOrganizePanel; preserve specialized layouts and semantic colors.
4. Repair dark-theme compatibility alias selectors and record reuse contracts in the frontend design system.
5. Run `npm run lint`, `npm run build`, `node scripts/check-ui-primitives.mjs`, existing settings/discovery/download checks and `git diff --check`. Independently review all changes and fix confirmed regressions.
6. Stop the local preview and browsers, remove temporary screenshots, and record results/limitations. Leave unrelated task files and commits untouched.

Rollback unit: presentation-only CSS/JSX and associated checks/spec. Do not alter server code, dependencies or user data.

## Validation results

- Completed: shared styles for seven category/switcher surfaces and all nine HTML data tables; removed three hardcoded white button backgrounds in file organization; corrected active input/button alias theme selectors.
- Baseline regression reproduced the Settings/Discover active-style mismatch (background, radius, text color and padding).
- Passed `npm run lint`, `npm run build`, `git diff --check` and the new `check-ui-primitives.mjs` against the production preview. Coverage includes 390, 768, 1023, 1024 and 1440 widths, both themes, native semantics, keyboard focus, input focus/button hover alias parity and table content/status colors.
- Passed existing `check-settings-tabs.mjs`, `check-hongguo-discover.mjs`, `check-download-space.mjs`, `check-task-log.mjs` and `check-storage-loading.mjs`.
- Reviewed dark/light mobile/desktop screenshots for Settings/Discover and file organization. Removed all 28 temporary screenshots and stopped both task-owned Web services; verified ports 4191/4192 no longer respond.
- Two independent read-only reviews found no confirmed introduced P1/P2 regressions. The suggestion to add button types elsewhere concerned unchanged controls and a hypothetical future form wrapper; it was excluded from this presentation-only task. The final three button-class removals were separately reviewed in the final diff.
- Intentional differences: filtering chips, ranking-period choices, season/episode selectors, compact preview spacing and per-page pagination content/layout. Existing `Select`, `ModalShell`, button and input primitives remain the reuse point.
- Limitation: browser checks use mocked APIs; real backend workflows and every possible production table payload were not exhaustively tested. No backend code or dependencies changed. Committed as `b2b76b7`; archived at the user's request on 2026-09-20. Tests were not rerun for this bookkeeping-only archival.
