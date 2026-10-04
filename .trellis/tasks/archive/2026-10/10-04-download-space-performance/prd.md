# Download space performance

## Goal
Reduce download-space list latency and background database load without changing visible results.

## Confirmed facts
- `internal/service/hongguo_download_works.go:28` counts distinct episodes and globally groups episodes before pagination.
- Runtime has about 1.33 million episode tasks and 16,024 placement rows; no missing or empty placements were found.
- Enqueue and episode replacement create tasks under an existing placement in the same transaction.
- The user approved optimizing work candidates, necessary indexes and hidden-page polling.

## Requirements
- Preserve 50-work pages, exact eligible work counts, earliest current episode creation descending/source-ID ties, all eight status totals, and existing API fields.
- Filter eligible works before pagination; aggregate all statuses only for current-page works.
- Exclude placements with no episode tasks.
- Pause work and expanded-episode polling while hidden; resume immediately without overlapping requests or stale-session updates.
- Keep changes limited to list queries, supporting indexes, visibility polling and their regression checks.

## Acceptance criteria
- Existing grouping, status-filter, retry and HTTP tests retain their assertions and pass with valid placement fixtures.
- Regression checks cover mismatched placement/task creation times, empty placements, deleting earliest tasks, status changes and paginated mixed statuses.
- Capture the single executed candidate/count/page-summary statement; compare old/new results and plans for all nine filters, under ordinary and generic prepared plans, including empty statuses.
- Migration is idempotent; added indexes support earliest task and status/source lookups.
- Browser checks confirm hidden polling pause, immediate resume, no overlap and cleanup; Web lint/build and diff checks pass.

## Out of scope
Caching, queue execution changes, new dependencies, production index creation/restarts and publishing changes.
