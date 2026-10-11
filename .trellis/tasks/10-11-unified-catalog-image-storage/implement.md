# Execution

## Adjacent catalog thumbnails follow-up plan

1. Route catalog thumbnail reads/writes to hash-adjacent versioned directories, using a content-based identity. Preserve remote/non-catalog cache behavior.
2. Extend startup reuse to both catalog and pre-catalog cache layouts, copying valid current variants directly beside originals. Retain old files and retry behavior.
3. Verify moved-root/mtime cache hits, actual migrated marker bytes, HEAD/ETag, new-content isolation, failed writes and both migration entry layouts. Independently review the change and update the image contract. Do not restart production or remove historical files.

### Adjacent thumbnail verification

- Catalog variants now use `<hash>.thumbnails/v1/<spec>` beside immutable originals. Content hash plus processing version/options determines identity; remote and non-catalog local sources retain cache storage and metadata-based invalidation.
- Startup scans the prior catalog cache layout before pre-catalog layouts and copies validated current variants directly beside originals. Existing targets and old files remain intact; no full-original scan or database migration was added.
- Passed `go test -race ./internal/service -run 'TestImageVariant|TestCatalogImageVariantMigration|TestCopyStoredImage|TestImageClone' -count=1`, the image/variant migration suite under `CGO_ENABLED=0` with `-tags nodynamic`, handler/server compilation, service/server go vet, and git diff --check.
- Tests verify actual marked cache bytes after migration and after moving DataDir/touching original mtime, stable ETags, distinct new content, fallback on blocked adjacent directories, missing/corrupt/stale sources, retry, both historical layouts, and retained old caches. Two independent read-only reviews found no confirmed defects; final main-thread source/diff review completed.
- Production has not been restarted and no production files were moved/deleted. Real-data startup timing and browser behavior remain deployment checks; native WebP opt-in runtime verification was not repeated because codec behavior is unchanged. No temporary services or export files were created. Changes remain uncommitted.

1. Centralize three-shard key and source/category path construction; preserve controlled legacy reads.
2. Update artwork deduplication, source propagation and avatar cache identity; cover catalog originals and local library covers.
3. Implement idempotent pre-listener migration and schema compatibility; validate multi-source splitting and interruption/invalid-file safety.
4. Run focused real-PostgreSQL image/artwork/people/NFO/catalog/cover tests, race checks and static checks. Verify frontend image URL contract remains unchanged.
5. Independent read-only review, fix concrete findings and update storage specs. Stop temporary services. Leave changes uncommitted until requested.

## Verification

- Passed focused regression tests across service, repository, database, handler and server packages with isolated PostgreSQL 16: artwork, people, catalog migration, Douban images/details, NFO, episode recheck, covers and image variants/fetches.
- Passed race tests across the same five packages, plus a final service migration/source-deduplication race run after the last changes.
- Migration tests cover source splitting, NFO item/binding references, preserving rich JSON and refresh timestamps, repeated runs, missing/corrupt originals, filesystem failure, database transaction rollback and retry, and batches exceeding 100 rows.
- Passed schema upgrade tests, including repeated AutoMigrate and removal of the old global SHA uniqueness constraint.
- Passed go vet for the five affected packages, frontend image URL checks, lint/build, and git diff --check.
- Two independent read-only reviews completed. Corrected two tests that still used the old avatar cache key; added pagination, rollback and source-scoped cache coverage. Final focused code review completed.

## Deployment limits

- Production was not restarted or modified for verification. Startup migration duration on the real dataset and browser behavior after that restart remain unverified.
- Migration runs before listeners/background writers, retains legacy bytes for recovery, and does not clean old originals or image variants. The first upgraded startup may take longer.
- Separate native/pure-Go WebP runtime configurations and multi-instance rolling upgrades were not validated in this change.
- Changes remain uncommitted and the task is not archived.

## Review follow-up: NFO ownership lookup indexes

- Added non-unique indexes to both image reference fields in the shared NFO model; AutoMigrate creates four table-specific indexes before image migration runs.
- Root cause: the per-asset ownership query was reviewed for reference coverage, but the two NFO tables lacked indexes for its reverse lookups. Small correctness fixtures concealed the repeated full scans.
- The regression test first failed against the old model, then passed after the fix. It simulates existing tables without the indexes and checks two complete AutoMigrate runs.
- Passed `go test ./internal/database ./internal/service -run 'TestArtworkStorageMigration|TestCatalogImageMigration' -count=1` against isolated PostgreSQL 16, with no skipped database tests. Final diff review and whitespace checks passed.
- Updated the database contract to require indexed reverse lookups and corrected its obsolete global-SHA deduplication description. Production restart and real dataset timing remain unverified; initial index creation still takes time.

## Thumbnail migration follow-up

- Added a post-original pass over legacy variant groups, including previously migrated originals. It resolves legacy source identity, verifies matching original content, and copies existing image variants under the new path/size/mtime identity without encoding.
- Existing target variants, old originals and old caches remain intact. Stale versions and content mismatches are skipped. Cache copy errors warn and leave normal lazy generation available; retry reuses completed targets.
- Covered artwork and people split across sources, Hongguo artwork/people, Huangguo, and legacy library covers (including relative source paths). Tests assert actual cache hits using marked valid image bytes, HEAD/ETag, idempotence, cancellation, mismatches, failed writes and retry.
- Race tests passed for the new migration and existing image variant tests; handler and server packages compiled. Static checks passed for service/server. Independent read-only review completed; configuration relocation is outside the cache-reuse contract because old path identities cannot be reconstructed after arbitrary moves.
- Read-only production diagnosis: the existing process started at 19:13:19 and had migrated 143,254 artwork assets by 19:43:48 (about 78/second). Per-image serial validation, file sync and many DB round trips add up; sampling confirms continuing I/O but does not isolate the dominant bottleneck.
- Counted 7,937 legacy cache source groups; a read-only directory enumeration took 0.45 seconds. This is traversal time only, not the complete copy/verification duration. No production restart or cache mutation was performed to test the new pass.

## Migration throughput follow-up

- Removed redundant asset insertion/reload and association updates for sources that retain the original asset ID. Source aliases mapping to the same target also skip those writes; splitting to other IDs retains the existing transaction path.
- Isolated PostgreSQL regression measured single-source/unreferenced migration at eight traced statements before and four after, including one batch SELECT (seven to three per asset, excluding transaction control). The local/local_nfo alias fixture dropped from sixteen to four. Existing-target fallback remains at eight statements.
- Tests verify asset/reference IDs, timestamps and idempotence, plus existing multi-source, rollback, missing-file and pagination coverage. File validation, atomic writes, fsync and transaction boundaries remain unchanged.
- This reduces database work; end-to-end production speedup is not yet measured. The running production process is unchanged and must load the new code on a later restart to use it.
- Passed real-PostgreSQL `go test -race ./internal/service -run '^TestCatalogImageMigration' -count=1`, service go vet and git diff --check. Reviewed the final branch separately against ID preservation and conflict fallback. The isolated test container was stopped.

## Batched updates and filesystem cloning

- Batch artwork ownership and destination lookups, then commit ordinary keys with one observed-key UPDATE per 100 rows. People, Hongguo/Huangguo and library-cover keys use the same batch update. Asset splitting retains its transaction path and flushes pending ordinary changes first; batch plans retain no image buffers.
- Originals and thumbnails prefer Linux filesystem reflink, verify cloned content, and fall back to writing validated bytes on unsupported cloning or changed sources. File sync, atomic replacement, independent inodes, old files and cleanup remain intact. Non-Linux platforms use the copy fallback.
- Isolated PostgreSQL fixture with 105 ordinary assets dropped from 317 SQL statements / 1.138 seconds to 8 statements / 0.518 seconds. This small local fixture does not establish a production speed multiplier.
- Added batch query-count, injected-statement-failure rollback/retry, observed-key and affected-row tests, plus cloning/content-change/cleanup tests. Real workspace filesystem cloning succeeded. Passed the combined service migration, clone, variant, artwork, cover and people race suite (45.608 seconds), service/server go vet and whitespace checks.
- Two independent read-only reviews completed. The reported same-batch destination conflict was checked against the immediate pending-key reservation and flush logic and was not reproducible from that path. Added explicit changed-source fallback coverage on a filesystem already verified to support reflink.
- Production was only queried, not restarted or modified. At 20:07 on October 11, the existing process had migrated 268,205 artwork assets with 11,532 remaining; 127,995 people images were still pending. The current process does not include this optimization. Production throughput and non-Linux runtime behavior remain unverified.
- Final clone-focused race tests passed, including changed-source fallback on the reflink-capable workspace. The isolated PostgreSQL container was stopped, temporary clone directories were removed, and final whitespace checks passed. Changes remain uncommitted.

## 提交归档

用户要求本轮提交归档，图片目录与缩略图改动作为 TMDb 资料中心的依赖一并提交到 main。上述各阶段验证记录保留；用户已自行重启，最新生产状态未在本次提交前核验，不将归档视为生产迁移验收。
