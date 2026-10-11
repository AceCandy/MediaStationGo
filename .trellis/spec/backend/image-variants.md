# Parameterized Image Delivery

## 1. Scope / Trigger

Read when changing Web/Emby image delivery, encoding, or image cache identity.
Original storage/import and authentication are not transformation concerns.

## 2. Signatures

- `ImageProxy.serveImageFile` / `imageVariants.serveFile` wrap the raw package-level `serveImageFile`.
- `parseImageVariantOptions(*http.Request)` handles case-insensitive query names.
- `imageURL(remote, version, options)` builds stable, Cookie-authenticated Web URLs.
- `LibraryRepository.FindCoverURL(ctx, id)` reads only `cover_url`; `ServeLibraryCover` must not preload library roots or probe table existence per image request.

## 3. Contracts

- No processing parameters: original bytes. Otherwise default `format=webp`, `quality=80`.
- `width`, `height`, `maxWidth`, `maxHeight`: integers 1–4096, fit proportionally, never upscale.
- `fillWidth` and `fillHeight`: both required, integers 1–4096, center crop then fit; other size limits still apply.
- `quality`: 1–100; `format`: webp/jpeg/jpg/png. PNG is lossless regardless of quality.
- Output edges are capped at 4096; decoding is bounded to 32 MiB and 32 million source pixels.
- JPEG/PNG EXIF orientation is applied before sizing; WebP uses the decoder's AutoRotate option.
- GIF, animated WebP and APNG are passed through without flattening. Failed processing returns original bytes with `no-store`; Emby missing-image placeholders are HTTP 200 PNG with `no-store`, including HEAD headers. Successful image caching is unchanged.
- One shared instance per service container; two concurrent encodes and same-key generation coalescing. Waiting observes request cancellation; an already-running codec finishes before cancellation is checked again.
- Catalog variants live beside the original as `<hash>.thumbnails/v1/<spec>` under DataDir. Recognize only canonical `catalogs/<provider>/<artwork|people>/aa/bb/cc/<hash>.<ext>` paths; never write thumbnails beside arbitrary user media. Other DataDir images retain mirrored cache paths under `<cache.cache_dir>/image-variants`; remote and external local images use `remote/<source-hash>` and `local/<path-hash>`. Atomic writes remain; there is no automatic cleanup or pre-generation.
- Native WebP with `gen2brain/webp v0.6.4` requires a dynamic Linux ELF and resolvable `libwebp.so` plus `libwebpdemux.so`; versioned runtime libraries alone do not satisfy its loader. Docker builds use the target-platform C toolchain and runtime `libwebp-dev`; `dev.sh` supplies project-local aliases for installed `.so.7`/`.so.2` libraries and forces CGO/external linking on Linux when a C compiler exists. Keep the pure Go fallback for other environments, and report `webp_backend` at image service construction.
- Catalog keys include algorithm version, immutable original content hash and normalized options. Moving DataDir or touching the original preserves thumbnail reuse; new original content has a new hash. Non-catalog keys retain source path/size/nanosecond mtime; downloaded bytes use a content hash if original storage failed. Source updates create a new variant; old variants remain. HTTP ETags still account for served thumbnail metadata.
- Web defaults to maxWidth=640; large backgrounds use 1920. `original:true` removes processing parameters. URL version replacement must not create duplicate query keys; tokens must not enter image URLs.
- Same-origin `/api/` images rely on the existing HttpOnly Cookie, not a token-bearing URL. Playback URLs retain their existing token behavior. Non-critical list, search and administration images use native lazy loading and asynchronous decoding.
- File variants honor their caller's Cache-Control, including hot variants and
  HEAD/304 responses. Mutable Douban subject poster URLs use private/no-cache;
  serving a previous poster after a failed update uses no-store even when the
  requested variant already exists on disk.
- Browser SW identities retain transformation parameters, removing only credentials and version/retry/refresh values. Its existing same-specification old-version eviction is independent of server disk retention.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Invalid, repeated or unsupported processing parameter at image delivery | 400 text/plain |
| Missing image/upstream failure | Existing missing-image/placeholder behavior |
| Decode/encode/cache write fails, unsupported animation, source limit exceeded | Original image, no-store |
| Matching ETag | 304 without body |
| HEAD | Same image headers without body |
| Catalog original moved or touched | Same thumbnail identity; reuse adjacent thumbnail |
| Non-catalog source metadata changed | New cache key, no deletion of old files |

## 5. Good / Base / Bad Cases

- Good: a 1200×600 image requested with maxWidth=640 returns 640×320 WebP.
- Base: the same image requested without processing options returns its original bytes.
- Bad: using an original-only exit for a first remote fetch, or flattening an animation.

## 6. Tests Required

- `go test ./internal/service ./internal/handler ./internal/middleware -run 'Image|Artwork|Cover|FFmpeg|FFprobe|Cookie'`
- `go test -race ./internal/service -run TestImageVariant`
- `CGO_ENABLED=0 go test -tags nodynamic ./internal/service -run TestImageVariant`
- With native libraries available: `MEDIASTATION_TEST_NATIVE_WEBP=1 CGO_ENABLED=1 go test -ldflags=-linkmode=external ./internal/service -run TestImageVariant`. This gate must fail instead of silently using the fallback; for the local alias setup also set `LD_LIBRARY_PATH="$PWD/.dev-cache/native-webp"`.
- Cover actual formats, transparency, aspect ratios, no upscale, EXIF, original pass-through, cold/hot remote responses, write failure, concurrent reuse, invalid parameters, HEAD/304 and source invalidation.
- `cd web && node scripts/check-image-url.mjs && npm run lint && npm run build`
- Database-dependent tests require `MEDIASTATION_TEST_POSTGRES_DSN`; a skip is not database verification. Browser/real-player latency is deployment verification, not a unit-test result.
- `TestLibraryCoverUploadServeAndClear` asserts one cover SELECT, no library-root preload, and correct missing/cleared-cover behavior when PostgreSQL is available.
- `TestImageVariantCatalogSurvivesRelocation` verifies actual cached bytes after moving DataDir and changing original mtime, new-content isolation, and adjacent write-failure fallback.

## 7. Wrong vs Correct

Wrong: `serveImageFile(...)` directly from a managed image store; only cold requests or some image types would skip transformation.

Correct: `store.imageProxy.serveImageFile(...)`; reserve the package-level helper for final raw file delivery inside the variant wrapper.

## Scenario: Source-owned persistent originals

### 1. Scope / Trigger
All managed original image writers/readers, including shared artwork, people,
Douban subjects, Hongguo/Huangguo and uploaded library covers.

### 2. Signatures
- `catalogImageKey(provider, category, hashKey)` produces a DataDir-relative
  `catalogs/<provider>/<artwork|people>/aa/bb/cc/<full-SHA256>.<ext>` key.
- `prepareStoredImage` supplies three two-character shards without a `sha256`
  directory. Local NFO/manual/empty provider names normalize to `local`.
- `PeopleImageStore.Import/ImportCached(ctx, provider, source)` receive explicit
  ownership; their remote URL cache includes the provider.
- `MigrateCatalogImages(ctx, db, dataDir)` runs before service construction and
  listeners at server startup.
- `MigrateCatalogImageVariants(ctx, dataDir, cacheDir)` follows original migration
  and restores existing thumbnail caches, including originals migrated by an
  earlier run. It scans cache groups from the catalog layout first, then older
  layouts, rather than every original or DB row.

### 3. Contracts
- Same-source/category content reuses a file; different sources keep separate
  copies. Artwork assets use unique storage keys, with matching SHA required
  when loading a conflict winner. SHA alone is no longer globally unique.
- Old shared artwork is split by selection/candidate/Douban/NFO ownership.
  Original IDs remain usable; unknown historical ownership is `legacy`.
- Sources retaining the original asset ID need only its storage-key update.
  Skip conflict inserts, asset reloads and reference rewrites for that key,
  including provider aliases mapping to the same directory. Other split targets
  and preexisting destination assets still require transactional reassignment.
- Query artwork ownership and existing destinations per 100-row batch. Commit
  ordinary key changes in one compare-and-set UPDATE; flush pending changes
  before a split or existing-target fallback. Reserve pending destination keys
  within the batch to prevent duplicate updates to a unique storage key.
- People, catalog and library-cover keys also use one UPDATE per batch. Count
  actual affected rows; a failed statement leaves the whole batch unchanged.
- Copy validated bytes before changing references. Asset splits are transactional;
  other updates compare observed keys/JSON. Preserve timestamps, unknown rich
  JSON fields, current selections and public authenticated image routes.
- Original and thumbnail migration prefer filesystem reflink through
  `copyStoredImage`. Verify cloned bytes against the validated input; unsupported
  cloning or changed source content falls back to writing that input. Keep file
  sync, temporary-file cleanup and atomic replacement; source and target never
  share an inode. Batch plans must not retain original image buffers.
- Keep old bytes for recovery. Missing/corrupt legacy files retain their old
  references and contribute to the migration summary; filesystem/DB write
  failures stop startup and can be retried. Do not restart a deployed server
  merely to test migration, or run older binaries against the changed schema.
- Legacy keys remain readable; regular image repair writes the new layout.
  Hongguo person images use `people`, even though legacy bytes lived in artwork.
- Uploaded library covers use `local/artwork`; their existing route's version
  includes the content hash and extension. Clearing/replacing a cover changes
  only the reference: deleting shared content could break another library or NFO.
- Proxy caches and user-owned sidecar originals retain their lifecycle.
  Catalog thumbnails use the adjacent directory and stable content identity.
  Copy only the cached version matching the source original's path/size/mtime,
  after verifying original content equality and hash. Sources include the previous
  `cache/image-variants/catalogs` layout and the pre-catalog layouts. Catalog
  cache migration does not require the earliest legacy original to still exist.
  Preserve existing nonempty destination thumbnails and old files. Unavailable
  caches regenerate on demand. Pre-upgrade cache identities still require the
  original path/mtime during this one-time migration; only the new adjacent layout
  is portable across directory moves without regenerating.
- Cover the legacy artwork/people, Hongguo/Huangguo and library-cover cache groups.
  Legacy library-cover source identity may use a relative path; do not normalize
  it differently from its original reader. A thumbnail copy failure logs a warning
  and keeps normal serving available; later startups can retry missing copies.
  Completed target variants are not rewritten on subsequent passes.

### 4. Validation / Error Matrix
- Unsafe provider/path -> reject; never interpolate external URLs into directories.
- Copy failure -> retain old references; transaction failure -> rollback split.
- Missing/corrupt original -> count and keep legacy reference for existing repair.
- Repeated run -> migrated rows are excluded; interrupted copies are reusable.
- Thumbnail cache: stale original version or mismatched bytes -> skip;
  existing destination -> preserve; failed copy -> warn and allow lazy regeneration.

### 5. Good / Base / Bad Cases
Good: TMDb and Douban share content bytes but use distinct asset IDs/files.
Base: two Douban subjects reuse the same source asset.
Bad: lookup a conflict winner using SHA alone, delete an uploaded cover's shared
file, or clear supplement failure state while rewriting an avatar key.

### 6. Tests Required
`TestCatalogImage*`, `TestCatalogPeopleImageCacheIsSourceScoped`,
`TestArtworkStorageMigrationRemovesGlobalHashUniqueness`, artwork concurrent
reuse, legacy/managed readers, people cooldown/cancellation, Douban images,
NFO/catalog repair, library covers and image variant/authentication tests.
Use real isolated PostgreSQL and race tests for source separation, pagination,
idempotence, reference rollback and failure recovery.
`TestCatalogImageMigrationPreservesAssetWithoutReferenceRewrites` verifies
reference/timestamp preservation, provider aliases and existing-target fallback.
`TestCatalogImageMigrationBatchesArtwork` bounds 105 ordinary assets to eight
SQL statements across two batches (four per nonempty batch).
`TestCatalogImageMigrationBatchRollbackAndRetry` verifies batch atomicity, retry,
compare-and-set protection and affected-row counts.
`TestImageClone*` and `TestCopyStoredImage*` cover real filesystem cloning when
available, independent writes, changed/missing sources and temporary-file cleanup.
`TestCatalogImageVariantMigration*` verifies actual cache hits across sources,
relative cover paths, HEAD/ETag, stale versions, content mismatch, preservation,
cancellation, write failure and retry without re-encoding originals. The
`FromCatalogCache` case covers already-migrated originals with no pre-catalog
source, and rejects stale, missing or hash-mismatched originals.

### 7. Wrong vs Correct
Wrong: `Where("sha256 = ?", hash)` after importing into a source directory.
Correct: find the row by storage key AND SHA, retaining storage-key uniqueness.
