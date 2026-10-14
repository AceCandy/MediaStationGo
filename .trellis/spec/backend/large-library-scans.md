# Large Library Scans

Full/root scans must not materialize all library fingerprints or a full-file seen map.

- `scanLocalLibraryFiles` buffers 500 media paths and queries only their narrow
  fingerprints using the unique path index. NFO keeps its separate sidecar path.
  Preserve the explicit `scan_file_mtime_ns` GORM mapping in narrow projections;
  default acronym splitting produces a different column name.
- Unix inode checks reuse directory-entry information. Symlinks still resolve
  target identity without following linked directories or replacing link fingerprints.
  Cross-batch hardlinks consult persisted identities across library roots. Adding a
  hardlink does not change size/mtime: persist its newly learned identity even on
  the unchanged branch so later batches can deduplicate it.
- Root prune runs only after a successful complete walk and flushed writes.
  Read 500 rows at a time by `(library_id, path)` keyset using
  `idx_media_library_scan_path`; never OFFSET-page a changing result set. Check root
  availability per page. Only `os.IsNotExist` allows deletion; access errors retain
  rows. Keep the legacy unassigned-root ownership predicate.
- A directory-name cache holds at most 4096 names from one directory to avoid
  restatting every existing file. Larger/unreadable directories fall back to Stat.
  WalkDir itself can still allocate one directory's entries; this is not a claim
  of constant memory for arbitrary flat directories.
- Track directories with parsed part candidates or stored part fields, including
  rows encountered during prune. Reconcile these directories, not all library rows;
  ordinary files with stale part fields and surviving siblings of deleted parts
  must still be repaired. Candidate-directory and existing probe-scope collections
  retain their prior semantic requirements; episodic scans do not collect probe IDs.
- `UpsertHongGuoScanBatch` admits at most 100 records per transaction, loads work
  and requested episode identities in batches, and checks each library once per
  transaction. Reuse the single-file ownership/ON CONFLICT core. Return actual
  created flags, not pre-transaction existence guesses. On rollback restore input
  values and let the scanner retry individually; only committed batches publish
  captured search refreshes. Watcher/single-file calls retain immediate semantics.
- Additive task metrics are `snapshot_ms`, `walk_ms`, `prune_ms`, `parts_ms`.
  `walk_ms` includes snapshot and ingest time; these metrics are not disjoint.
  HTTP/root and scheduler terminal results both retain them.

## Verification

Use isolated PostgreSQL, including race detection:

- `TestScanLargeLibraryBoundedReadsAndDeletion`: unchanged 1201 files use three
  narrow fingerprint and prune pages, no whole-library parts query; deleting 601
  files preserves keyset completeness and bounded details.
- `TestScanHardlinkAcrossReadBatches`, `TestScanFilteredWalkAndIdentityReuse`,
  `TestScanSymlinkFingerprintAndDirectoryBoundary`, `TestScanDirectoryNameCacheIsBounded`.
- `TestScanClearsOldPartStateWithoutFullLibraryReconcile` and existing part tests.
- `TestHongGuoScanBatchLookupAndRollback`, `TestScanHongGuoBatchFailureKeepsOtherFiles`,
  `TestMediaScanBatchConcurrentPath`, existing single-file concurrent path tests.
- `TestScanPagingIndexPlan`: repeated index maintenance and real EXPLAIN ANALYZE
  on 20000 media rows; keyset page must use an index without full-table sort/scan.
- Retain failed/unreadable/offline root, NFO, watcher, progress, probe-scope and
  recognition/search batch tests. `BenchmarkScanFileIdentity` compares legacy
  restatting with attribute reuse; it is not an end-to-end 2-million-file benchmark.
