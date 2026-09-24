# Evidence and change boundaries

## Search and document ownership

- `internal/repository/hongguo_search_index.go:16-35`: `hongGuoSearchWorks`, `searchDocuments` and `searchDocumentIDs` derive canonical `hg-work-` / `hg-group-` identities independently of files. `:52-107` enumerates all visible identities, passes `CandidateIDs` to OpenSearch, and repeats the scope for title/visibility revalidation. Successful empty index responses still reach that SQL.
- `internal/repository/hongguo_groups.go:15`: official album presentation uses the earliest stored series season (positive season number, then source ID); a file is not required on that primary season. Do not replace this with the earliest playable season.
- `internal/repository/hongguo_series.go:17`: `hongGuoFileScope` joins media/binding and applies allowed/hidden library IDs. `:108` scopes final representative files by source work IDs. Keep that card-loading behavior separate from this task.
- `internal/repository/media_search_repository.go:193`: existing metadata filter preparation can derive the positive visible-library set. Hidden-only scopes currently use distinct media library IDs; do not silently substitute different library-row/deletion semantics. A work present in both hidden and visible libraries remains eligible through the visible one.
- `internal/service/emby_search_items.go:25-52,85-118`: person/favorite filters are request-local; played/unplayed/resumable already have database paths. Do not put these predicates only after a top-100 index response.

## Existing index lifecycle

- `internal/repository/opensearch.go:20-63,142-163,262-288`: ordinary and HongGuo currently share schema version 1, while aliases/document types differ. Mapping already supports keyword `library_ids`; HongGuo does not populate it. Readiness caches a boolean after checking schema/document type, so changing document semantics without a HongGuo-specific version would incorrectly accept the old index.
- `internal/repository/opensearch.go:202-223`: refresh the completed target, atomically switch alias, mark backend ready. `:321` orphan cleanup is scoped by alias/version and must not delete active aliases.
- `internal/repository/media_view_repository.go:68` and `internal/repository/hongguo_repository.go:18`: independent `searchIndex` coordinator instances hold mutex/rebuild/dirty/failure state. `internal/repository/repository.go:44` wires Media to MediaView, but not to the existing HongGuo instance; new mutation hooks must use the configured HongGuo instance, not `New(tx).HongGuo` with an empty backend.
- `internal/repository/media_search_repository.go:743-851`: keyset backfill, dirty replay under the coordinator lock, alias activation only after success, failed-target discard and failure reset on successful rebuild. `:974-1006`: HongGuo incremental recomputation/writes serialize; errors mark `searchFailed`. Search reads that flag without taking the write mutex.
- `internal/service/service_search_warmup.go:15-52`: configured asynchronous startup warmup (120-second default delay) runs ordinary and HongGuo independently. No durable cross-process dirty queue exists. Keep settings/delay/concurrency unchanged; clearly describe fallback before trustworthy HongGuo reconstruction.

## Mutation matrix

| Owner / anchor | Existing behavior | Necessary index notification |
| --- | --- | --- |
| `repository/hongguo_media.go:36` `upsertHongGuoMedia` | Media upsert and binding occur in one transaction; no index notification. Ordinary Upsert exits before its normal hook for HongGuo (`media_repository_upsert.go:27`). | Capture previous binding/album membership, then refresh affected old/current identities only after commit. Cover library changes and binding deletion to pending state. |
| `repository/hongguo_media.go:108` `RebindWork` | Each media has a separate row-locked transaction; partial progress can commit before error/cancellation. | Refresh committed changes even on an early exit; do not notify only on whole-loop success. Deduplicate a bounded batch where possible. |
| `repository/hongguo_repository.go:153` `SaveDetailWithChange` | Existing post-commit work/album refresh. Service calls `RebindWork` afterward (`service/hongguo.go:301`). | Preserve existing refresh and ensure the later binding change updates membership too. |
| `repository/hongguo_groups.go:53` `SaveAlbum` | Existing post-commit old/new album refresh. | Preserve old/new identity invalidation, recompute union membership and primary title. |
| `service/media_delete.go:10` `Delete` | Direct media delete, ordinary metadata refresh. | Capture bound work/old album before deletion; refresh afterward. Disk-file behavior unchanged. |
| `service/scanner_prune.go:15,147` `RemovePath`, `deleteMediaByIDs` | Watcher and both prune flows delete by path or batches of 500; ordinary refresh only. | Cover both shared deletion points; retain filesystem/offline guards, batching, counts and cancellation. |
| `service/media_library.go:17` `DeleteLibrary` | Media/root/library deletion shares a transaction; ordinary refresh after commit. | Capture HongGuo affected identities inside the transaction and refresh only after the whole transaction commits. |
| `repository/media_repository.go:147,159` `DeleteByLibrary`, `DeleteByLibraryRoot` | Direct deletes with ordinary post-delete refresh; root service calls the latter. | Capture and refresh HongGuo membership without changing root deletion ordering. |
| `service/duplicate.go:235` `removeMissingRows` | Detect scans all catalog media, deletes missing file rows, updates report counts. | Add HongGuo invalidation without changing cleanup eligibility/error/report rules. |
| `service/organizer_directory_versions.go:190` `replaceVersions` | Direct path deletes after existing filesystem replacement checks. | Preserve replacement semantics; notify for any affected HongGuo binding. |
| `service/organizer_reclassify_media_rows.go:10,48` | Direct path/library update and path delete; ordinary notifications only. | Capture old binding identities and refresh membership after successful writes. |
| `service/organizer_reclassify_scanned.go:276` | Library-only reclassification updates `library_id` directly. | Include notification when it touches an already-bound HongGuo row; do not change classification eligibility. |
| `repository/hongguo_download_cleanup.go:18` | Confirmed catalog cleanup protects files/bindings and already refreshes removed work/album after commit. | Regression coverage; no download behavior changes needed. |

For delete/rebind capture, the authoritative old relationship is `hongguo_media_bindings`, not merely `media.lookup_catalog_id` (which can be changed or pending). Capture source work IDs and old logical identities before loss; include the current albums of those work IDs during post-commit recomputation. Avoid serializing network writes inside database transactions.

## Existing tests and gaps

- `hongguo_search_index_test.go:40`: lifecycle test currently expects fileless detail saves to upsert documents and uses CandidateIDs assertions. Update the intended index contract rather than weaken the assertions. It tests dirty replay, fallback and failure recovery, but the final raw DB deletion checks only request-side visibility, not production deletion notification.
- `hongguo_search_index_test.go:160,199`: explicit-live temporary index and HTTP alias/filter tests need HongGuo version 2/library membership cases. Normal metadata version 1 remains valid.
- `hongguo_search_plan_test.go:10`: 10,000 works/40,000 files; captures actual parameterized SQL for visibility enumeration/revalidation/representatives. Replace the healthy-path enumeration expectation with its absence and inspect work/album traversal too, not just file counts. Keep fallback coverage and array binding.
- `hongguo_media_test.go`, `media_upsert_concurrency_test.go`: pending/bound/invalid coordinates/classification/concurrent insertion fixtures support incremental membership tests.
- `service/emby_source_search_test.go`, `media_search_parallel_test.go`: three-source behavior, failure/join, candidate limits, pagination and concurrency must remain valid. Some fixtures use read-only backend doubles rather than sync backends; they must explicitly model a ready indexed state without weakening startup safety.

## Evidence limits

Three bounded read-only source audits were followed by main-thread spot checks of lifecycle, binding, deletion transaction and library-update owners. No implementation or tests have run for this task. No production service/index/data was changed. Direct administrator SQL and concurrent multi-process writers are outside the existing single-process best-effort index lifecycle; do not claim new distributed consistency guarantees.

## Planning review

An independent read-only plan review requested explicit permission-state normalization, rollback/partial-commit assertions, shared-coordinator ownership, and a distinction between same-process routine rebuild and restart with a persisted alias. These are now specified in `design.md`, `implement.md` and the acceptance criteria. No additional product feature or production operation was introduced. Context-manifest validation passed with size warnings for the two large spec files; implementation must read required originals, not rely on truncated automatic injection.
