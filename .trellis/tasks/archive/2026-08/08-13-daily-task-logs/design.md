# Daily Task Logs Design

## Boundaries

The change owns task log persistence, the administrator task-log HTTP contract,
and the task log dialog. Execution summaries, scheduler behavior, and task
business logging remain unchanged.

## Storage Contract

New entries are appended to:

```text
<data-dir>/task-logs/YYYY-MM-DD/<definition-key>.log
```

The date uses the task log store's configured local timezone. The definition
key is resolved centrally from the same `Kind` and `Name` rules used by task
definitions; this prevents shared raw kinds from merging people or scrape
definitions. The existing process-wide mutex continues to serialize appends.

Files retain the existing line format and chronological append order. A daily
read tails one file with the existing default and maximum byte bounds. Legacy
UUID-named files are ignored and left untouched.

## Service And HTTP Contract

The service exposes one definition-level daily-log operation. It validates the
definition key and optional strict `YYYY-MM-DD` date, enumerates only safe
`.log` filenames under server-owned date directories, selects the newest date
when none is requested, and returns:

```json
{
  "date": "2026-08-13",
  "dates": ["2026-08-13", "2026-08-12"],
  "content": "...",
  "truncated": false
}
```

The authenticated administrator route is
`GET /tasks/definitions/:key/log?date=YYYY-MM-DD&tail_bytes=N`. Unknown
definitions return 404; malformed or unavailable dates return 400. An empty
task has an empty date and content with an empty dates array.

The execution-history API is independent and may remain available, but the new
dialog no longer calls it. The execution-ID log route is removed because there
is no released compatibility contract.

## Frontend Flow

Opening a task log dialog requests the definition log without a date and uses
the returned newest date. A compact month calendar is rendered from native
`Date` operations; dates absent from `dates` are disabled. Month arrows are
icon buttons. Selecting an enabled day reloads that day's content while keeping
the calendar visible.

On narrow screens the calendar stacks above the log; on desktop it occupies a
stable left column. Loading, empty, read-error, and truncated states are shown
without changing modal dimensions.

## Risks And Rollback

- Definition resolution must share the task-definition matching rules; tests
  cover the two shared-kind pairs.
- Date validation occurs before path construction to prevent traversal.
- Large daily files remain bounded on read and may show only their tail.
- Rollback is a code revert. New definition-key files are plain text and do not
  affect database state.
