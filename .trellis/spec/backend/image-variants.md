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
- GIF, animated WebP and APNG are passed through without flattening. Failed processing returns original bytes with `no-store`; placeholders retain existing semantics.
- One shared instance per service container; two concurrent encodes and same-key generation coalescing. Waiting observes request cancellation; an already-running codec finishes before cancellation is checked again.
- Variants live at `<cache.cache_dir>/image-variants/<hash-prefix>/<hash>.<format>` and use existing atomic file writes. No automatic cleanup or pre-generation.
- Keys include algorithm version, source path/size/nanosecond mtime and normalized options. Downloaded bytes use a content hash if original storage failed. Source updates create a new variant; old variants remain.
- Web defaults to maxWidth=640; large backgrounds use 1920. `original:true` removes processing parameters. URL version replacement must not create duplicate query keys; tokens must not enter image URLs.
- Same-origin `/api/` images rely on the existing HttpOnly Cookie, not a token-bearing URL. Playback URLs retain their existing token behavior. Non-critical list, search and administration images use native lazy loading and asynchronous decoding.
- Browser SW identities retain transformation parameters, removing only credentials and version/retry/refresh values. Its existing same-specification old-version eviction is independent of server disk retention.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Invalid, repeated or unsupported processing parameter at image delivery | 400 text/plain |
| Missing image/upstream failure | Existing missing-image/placeholder behavior |
| Decode/encode/cache write fails, unsupported animation, source limit exceeded | Original image, no-store |
| Matching ETag | 304 without body |
| HEAD | Same image headers without body |
| Source metadata changed | New cache key, no deletion of old files |

## 5. Good / Base / Bad Cases

- Good: a 1200×600 image requested with maxWidth=640 returns 640×320 WebP.
- Base: the same image requested without processing options returns its original bytes.
- Bad: using an original-only exit for a first remote fetch, or flattening an animation.

## 6. Tests Required

- `go test ./internal/service ./internal/handler ./internal/middleware -run 'Image|Artwork|Cover|FFmpeg|FFprobe|Cookie'`
- `go test -race ./internal/service -run TestImageVariant`
- `CGO_ENABLED=0 go test -tags nodynamic ./internal/service -run TestImageVariant`
- Cover actual formats, transparency, aspect ratios, no upscale, EXIF, original pass-through, cold/hot remote responses, write failure, concurrent reuse, invalid parameters, HEAD/304 and source invalidation.
- `cd web && node scripts/check-image-url.mjs && npm run lint && npm run build`
- Database-dependent tests require `MEDIASTATION_TEST_POSTGRES_DSN`; a skip is not database verification. Browser/real-player latency is deployment verification, not a unit-test result.
- `TestLibraryCoverUploadServeAndClear` asserts one cover SELECT, no library-root preload, and correct missing/cleared-cover behavior when PostgreSQL is available.

## 7. Wrong vs Correct

Wrong: `serveImageFile(...)` directly from a managed image store; only cold requests or some image types would skip transformation.

Correct: `store.imageProxy.serveImageFile(...)`; reserve the package-level helper for final raw file delivery inside the variant wrapper.
