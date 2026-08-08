# Implementation Plan

## Success Strategy

Implement test-first in narrow slices. Each slice must demonstrate logical
metadata pagination and user-visible Media loading before the next caller is
converted. Preserve all public Emby IDs and response fields.

## Checklist

1. Add failing scope and visibility tests.
   - Extend `internal/service/emby_versions_test.go` for same-library versions,
     cross-library membership, global deduplication, and hidden sibling sources.
   - Extend `internal/service/emby_series_hierarchy_test.go` for Series logical
     pagination, hierarchy IDs, and Latest completeness beyond old prefetch caps.
   - Extend `internal/service/emby_movie_library_test.go` for Movie and
     path-classified Series behavior in mixed libraries.
   - Extend `internal/handler/emby_search_test.go` for SearchHints ID, total, and
     logical-work deduplication.
   - Add or extend Counts assertions so totals match `/Items` visibility.

2. Add the minimal metadata-scope repository query.
   - Represent global and physical scopes without virtual-library persistence.
   - Implement compatible MetadataItem count/order/page queries using scoped
     Media `EXISTS`/hierarchy relations.
   - Reuse the same filter construction for count and page operations.
   - Cover repository behavior with focused SQLite tests where service tests do
     not exercise query semantics directly.

3. Convert core Emby item listing.
   - Replace Media-root pagination in `internal/service/emby_items_list.go` with
     metadata-root pagination.
   - Batch-load visible MediaView versions for the selected metadata page.
   - Preserve requested-container `ParentId`, user state, and concrete source
     behavior.

4. Convert hierarchy and mixed-library paths.
   - Query Series, Seasons, and Episodes as logical metadata within the physical
     scope.
   - Remove correctness dependence on 50,000-row Episode/Media prefetches.
   - Preserve mixed movie library path classification and logical ordering.

5. Convert Latest, Resume, Counts, and SearchHints callers.
   - Apply the same metadata scope and visibility rules.
   - Remove correctness dependence on the 500-row Latest prefetch.
   - Ensure global projections deduplicate metadata across visible libraries.

6. Close sibling MediaSource visibility bypass.
   - Make user visibility an explicit input to `mediaVersionSiblings` or the
     common sibling loader in `internal/service/emby_media_sources.go`.
   - Update item payload and `internal/service/emby_playback.go` callers.
   - Reject hidden/disallowed siblings without changing visible source IDs,
     tracks, version labels, or playback selection.

7. Independently review the full diff.
   - Trace Items, Item detail, Resume, Latest, Counts, SearchHints, and
     PlaybackInfo from handler input to repository filter and output.
   - Check for N+1 queries, mismatched count/page filters, unstable ordering,
     fixed prefetch limits, and any visibility bypass.
   - Confirm no unrelated people/artwork changes were altered.

## Validation

Run focused tests during each slice, then the project quality gate:

```bash
go test ./internal/service/... ./internal/handler/...
go test ./...
go vet ./...
git diff --check
```

No service needs to be started for this backend query change. If a targeted test
requires a temporary process, stop it before completion.

## Risky Files and Rollback Points

- `internal/service/emby_items_list.go`: central list/count/pagination behavior.
- `internal/service/emby_movie_items.go`: mixed Movie/Series classification.
- `internal/service/emby_items_detail.go`: Latest, Resume, and item payloads.
- `internal/service/emby_media_sources.go`: cross-library visibility boundary.
- `internal/service/emby_playback.go`: concrete version selection.
- Metadata/MediaView repository files selected during implementation: SQL
  compatibility and filter consistency.

Keep commits or review slices aligned with the checklist so repository query,
caller conversion, and permission fixes can be reverted independently without a
data migration.

## Before `task.py start`

- Confirm this final planning summary has been explicitly approved in a later
  user message.
- Read `trellis-before-dev/SKILL.md` and the backend/repository specs selected by
  its routing instructions.
- Recheck the worktree and preserve all changes from
  `08-07-persist-people-images`.
- Start the task only after those gates pass.
