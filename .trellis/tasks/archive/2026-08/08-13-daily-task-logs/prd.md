# Daily Task Logs

## Goal

Replace execution-oriented task log browsing with a date-oriented view so an
administrator can select a day and read the complete log for one stable task
definition. Persist one file per task definition per calendar day.

## Background

- The current store writes `task-logs/YYYY-MM-DD/<execution-id>.log` and the UI
  lists individual executions. Calling this "one task per day" conflated an
  execution instance with a stable task definition.
- The product meaning of "task" here is one of the eight stable definition
  keys, such as `people_translation` or `catalog_scrape`. Raw task kinds are not
  sufficient because people and scrape definitions share kinds.

## Requirements

- New log writes use `task-logs/YYYY-MM-DD/<definition-key>.log`.
- All executions of the same task definition on the same local calendar day
  append to that one file in chronological order.
- The task log dialog replaces the execution list with a calendar/date picker.
- Dates that contain logs for the selected task definition are discoverable,
  and opening the dialog selects the latest available date.
- Selecting a date displays the complete available log content for that task
  definition on that date, including all executions on that date.
- The eight stable task definitions remain isolated even when they share the
  same internal task kind.
- Existing tail-size limits and truncation feedback remain bounded and visible.
- Existing authentication and authorization requirements remain unchanged.
- Legacy per-execution log files are not migrated, aggregated, or exposed by
  the new UI because the feature has not been released.

## Acceptance Criteria

- [ ] Repeated executions of `people_translation` on one date append to exactly
      `task-logs/<date>/people_translation.log` rather than separate UUID files.
- [ ] `people_translation` and `people_backfill` never share a daily file;
      `media_scrape` and `catalog_scrape` never share a daily file.
- [ ] The dialog has no per-execution list or execution pagination controls.
- [ ] Opening the dialog selects the newest date with logs and shows every log
      line stored for that definition on that date in chronological order.
- [ ] Selecting another available date replaces the content with that day's
      log; an unavailable or invalid date cannot expose arbitrary files.
- [ ] Empty, loading, error, and truncated states are explicit.
- [ ] Existing task execution summaries and task triggering behavior are not
      changed.
- [ ] Backend focused tests, Web lint/build, and `git diff --check` pass.

## Out of Scope

- Changing task execution summary persistence or scheduler behavior.
- Adding a third-party calendar/date library.
- Searching, filtering, downloading, or live-streaming log content.
- Deleting historical log files.
- Compatibility reads for legacy `<execution-id>.log` files.
