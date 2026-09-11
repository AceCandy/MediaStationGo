# Scrape task feedback

## Requirements

- Show the actual queued file count for manual media scraping; explicitly explain zero work without creating an empty execution.
- Manual library resets wake only media workers, and only when rows were queued.
- Display catalog work as 作品资料补全 while preserving its definition key, execution-name filters and historical logs.
- Distinguish waiting for scrape resources or media completion from active catalog processing, including resumption.
- Preserve candidate selection, shared locking, retries and provider behavior.

## Acceptance

- Empty/matched-only libraries produce zero queue signals; nonempty resets signal media workers only.
- UI checks cover zero/nonzero counts, target forwarding and waiting/running/idle states.
- Catalog checks cover waiting before lock acquisition and restoration after yielding; existing history/log isolation still passes.
- Targeted Go tests, Web lint/build and diff checks pass; no live business data changes or service restart.

## Verification

- Passed targeted service/handler PostgreSQL tests for library resets, media resets, catalog hydration, waiting/cancellation, task definitions and history/log isolation using a separate temporary PostgreSQL instance.
- Passed `node scripts/check-task-log.mjs`, `npm run lint`, `npm run build`, and `git diff --check`.
- Independent read-only review found no blocking issues; final test changes reviewed by the implementer.
- Corrected two existing regression-test assumptions: event logs require details, and GORM destination objects must not retain a different row's primary key.
- No live restart, browser visual check, or full Go suite. User approved committing and archiving after review.
