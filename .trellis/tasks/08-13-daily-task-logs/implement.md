# Daily Task Logs Implementation

1. Update the log store to append and read by definition key and calendar date.
   Verify with focused tests for shared daily files, date ordering, validation,
   timezone behavior, and tail limits.
2. Add central task-to-definition resolution and a definition-level daily-log
   service method. Route task start/update/finish entries through the resolved
   key and verify shared raw kinds remain isolated.
3. Replace the execution-ID log HTTP route with the definition/date contract
   and add handler coverage for unknown definitions and invalid dates.
4. Replace the dialog's execution list with a compact accessible month calendar
   and wire it to the daily-log API without adding dependencies.
5. Run focused Go tests, `npm run lint`, `npm run build`, and
   `git diff --check`. Independently review the final diff against this PRD,
   especially date/path validation and loading/empty states.

## Risky Files And Rollback Points

- `internal/service/task_log.go`: path and date validation are the main data and
  security boundary.
- `internal/service/task_tracker.go` and `task_definitions.go`: definition
  resolution must remain consistent with execution filtering.
- `web/src/pages/TasksPage.tsx`: calendar month arithmetic and responsive layout
  require explicit verification.

Each step is independently revertible; no database migration or destructive
filesystem operation is planned.
