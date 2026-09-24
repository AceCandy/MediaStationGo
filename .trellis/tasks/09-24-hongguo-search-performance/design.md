# Design

Reuse HongGuo library paging's visible-file scope and correlated `EXISTS (... OFFSET 0)` boundary in `HongGuoRepository.SearchCandidates`. Retain current catalog filters, logical album projection, independent OpenSearch candidate scope and database revalidation. Do not move permission checks after LIMIT.

In `hongGuoSearchRepresentatives`, first resolve the requested logical identities into source work IDs using the existing official album projection. Then constrain the existing representative query with `b.work_id = ANY(?)` before file joins. Preserve its visibility, identity recheck, ordering and presentation overlay. Empty source-work sets return immediately.

Only repository query logic changes; Web and Emby callers keep their contracts. No migration, new index, dependency, cache or runtime setting is needed. Revert the two repository edits to roll back.

Validation separates correctness, query-plan work, and HTTP timing. Use isolated PostgreSQL fixtures for writes. Current application data is only read through read-only connections/transactions with timeouts; never run normal application startup or migrations against it for this check.
