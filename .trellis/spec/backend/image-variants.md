# Parameterized Image Delivery

All managed-image readers, including HongGuo and HuangGuo AI artwork endpoints,
use `managedImagePath` with the correct category. Never prepend catalog directories
to a stored relative key. HongGuo person artwork uses `people`; work/source posters
use `artwork`. An invalid stored key follows the ordinary missing-image repair path.

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
- `TestCatalogSourceArtworkRequiresCurrentKey` checks HongGuo posters/person images and HuangGuo AI posters with real PostgreSQL: current keys support GET/HEAD, while bare relative keys are rejected even when the catalog file exists. Missing-image repair sets a retry without clearing the stored key or resetting backoff.
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

### 3. Contracts
- Same-source/category content reuses a file; different sources keep separate
  copies. Artwork assets use unique storage keys, with matching SHA required
  when loading a conflict winner. SHA alone is not globally unique.
- Managed readers accept only the current catalog key shape and category;
  unsafe providers and traversal keys fail before filesystem access. There is
  no original or thumbnail startup migration and no old-key read fallback.
- Original writes sync a temporary file before atomic replacement and remove
  temporary files on failure. Failed replacement retains existing references.
- Uploaded library covers use `local/artwork`; their public route's version
  includes the content hash and extension. Clearing/replacing a cover changes
  only the reference: deleting shared content could break another library or NFO.
- Catalog thumbnails live in an adjacent directory using stable content
  identity. Missing thumbnails regenerate on demand. Proxy caches and
  user-owned sidecar originals retain their independent lifecycle.

### 4. Validation / Error Matrix
- Unsafe provider/path, old storage key or wrong category -> reject.
- Failed write/rename -> clean temporary files and keep the original/reference.
- Missing thumbnail -> generate lazily with original fallback on failure.

### 5. Good / Base / Bad Cases
Good: TMDb and Douban share content bytes but use distinct asset IDs/files.
Base: two Douban subjects reuse the same source asset.
Bad: lookup a conflict winner using SHA alone or delete an uploaded cover's
shared file.

### 6. Tests Required
`TestManagedImagePathOnlyAcceptsCatalogKeys`, `TestWriteStoredImageCleansFailedTarget`,
`TestArtworkStorageKeysIsolateSources`, `TestCatalogPeopleImageCacheIsSourceScoped`,
artwork concurrent reuse, people cooldown/cancellation, Douban images,
NFO/catalog repair, library covers and image variant/authentication tests.
Use real isolated PostgreSQL and race tests for source separation and serving.

### 7. Wrong vs Correct
Wrong: `Where("sha256 = ?", hash)` after importing into a source directory.
Correct: find the row by storage key AND SHA, retaining storage-key uniqueness.
