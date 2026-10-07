# Primary progress API

User approved implementing an exact per-user progress API for LinPlayer primary-server synchronization. Ordinary automatic reports retain recording thresholds and event semantics.

## Acceptance
- Read effective progress and a monotonic revision for all four catalogs.
- Atomic conditional write accepts explicit zero, short progress and Played=false without deleting history or generating events.
- Any existing progress writer, clear/delete or identity merge invalidates stale revisions; favorite-only changes do not.
- Missing-state concurrent first writes have exactly one winner. Deleted identities retain revision tombstones.
- Authentication, visibility, exact concrete source and logical identity are validated; foreign users and mismatched versions cannot write.
- PostgreSQL integration tests cover actual migration, concurrency and rollback. API catalog is updated.

No production database changes, commits or pushes are authorized by this task.
