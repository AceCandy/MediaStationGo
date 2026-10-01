# Player Request Logging and Redirect Cache

## 1. Scope / Trigger

Apply this contract when changing playback redirect resolution, Emby stream cancellation behavior, or persisted player request diagnostics.

## 2. Signatures

- `playbackRedirectResolver.Resolve(ctx, mediaID, rawURL, userAgent)` caches the resolved Location.
- Cache and in-flight keys are `mediaID + "\x00" + userAgent`; the default TTL is one hour.
- `player_request_logs.response_body text NOT NULL DEFAULT ''` stores only sanitized failed-response content.
- `PlayerRequestLogItem.response_body` exposes the field to the admin player-log page.
- Web 与 Emby 视频流路径中的 `id` 是 concrete `media.id`，已加载的 `*model.Media` 通过 `ServeMedia` 直接播放。

## 3. Contracts

- A media ID uniquely owns one immutable STRM playback address. A different STRM address requires a different media ID.
- `rawURL` is used only on cache miss to resolve the upstream Location; it is not cache identity.
- Successful URL resolutions and valid HTTP-500 local-file fallbacks are cached; other failures are not. Equal keys share one in-flight upstream request.
- Successful HTTP PlaybackInfo requests asynchronously prefetch configured redirects for all visible versions of the same-season episode number + 1, using the request User-Agent and the same resolver/cache as playback. Each version must first have a valid persisted complete probe document from `MediaProbeService.LoadMany`; wait for the existing track backfill rather than starting another probe. Check pending documents once per second within the round budget; ready versions proceed independently and unresolved versions are skipped on timeout/cancellation. Do not cross seasons, skip missing episodes, or request unconfigured remote prefixes/local direct files. Empty User-Agent and rejected requests do not prefetch. Each round uses the service-lifetime context with a one-minute budget, so returning the HTTP response does not cancel prefetch and service shutdown does.
- Before caching a remote target, GET the redirect chain with the same player User-Agent and `Range: bytes=0-0`, without player credentials or a cookie jar. Accept only final HTTP 200/206 with one readable byte; close the body without buffering media. Cache the validated final URL.
- A target-validation HTTP 403 permits up to two fresh source resolutions (three attempts total). Other validation failures are not retried and must not trigger the source-500 local fallback. Resolution, validation and retries share the existing 15-second context budget; cancellation releases the in-flight entry and never caches a failure.
- Player request `body` remains the sanitized request body. `response_body` is populated only for status 400 or greater.
- Failed response content is capped at 64 KiB and uses the same sensitive JSON-field redaction as request bodies.
- Non-JSON content containing a sensitive field marker is replaced as a whole instead of persisted verbatim.
- Successful, redirect, and media-byte responses never persist response content.
- PlaybackInfo emits an INFO `emby playback info response summary` application
  log after request-token attachment. Its allowlist contains media source IDs,
  capabilities, container, runtime, default indices, stream types/indices/codecs,
  and address-structure booleans only. Never log raw Path/DirectStreamUrl,
  credentials, media titles, or user identity. The summary does not populate
  player request `response_body` or change the client response. Application INFO
  logging must be enabled to collect it; historical success bodies cannot be
  reconstructed from player request logs.
- 视频流 handler 不通过 `Item` 或 `PlayableMediaID` 解析 metadata、season、series ID；非 concrete media ID 返回 404。

## 4. Validation & Error Matrix

| Condition | Behavior |
|---|---|
| Same media ID and User-Agent before TTL | Return cached Location without an upstream request |
| Different media ID with the same URL and User-Agent | Resolve independently |
| Different User-Agent or expired entry | Resolve again |
| New Location validates as HTTP 200/206 with readable data | Cache the final validated URL |
| Target validation returns HTTP 403 | Resolve again up to twice; cache only a validated success |
| Validation fails, returns an empty body, or exhausts retries | Preserve the original-URL redirect fallback without caching the failed target |
| Upstream HTTP 500 with a valid `ffprobe.path_mappings` file | Serve the local file and cache the fallback |
| Other upstream error, missing local file, or invalid Location | Redirect to the original URL without caching the failure |
| Emby video stream request is canceled | Record status 499 with no response body |
| Emby item/people/search/count/latest/resume/next-up/season/episode browsing returns a cancellation error | Record status 499 with no response body; preserve genuine errors as 500 |
| Other 4xx/5xx player response | Persist sanitized `response_body` |
| 2xx/3xx player response | Persist an empty `response_body` |

## 5. Good / Base / Bad Cases

- Good: repeated Range requests for one media ID and player reuse one resolved Location.
- Base: a 500 JSON error stores its message while token-like fields become `[redacted]`.
- Bad: keying by the signed source URL, caching failures, or buffering successful media bytes.

## 6. Tests Required

- Assert same-key sequential and concurrent calls produce one upstream request.
- Assert different media IDs do not share a cache entry even when URL and User-Agent match.
- Assert User-Agent isolation, TTL expiry, and failure non-caching.
- Assert next-episode prefetch is nonblocking, respects visibility and episode boundaries for ordinary/NFO/HongGuo sources, and reuses cached remote STRM/path-mapped targets during playback; cover HTTP triggering, missing/invalid probe documents, metadata becoming ready, and lifecycle cancellation. Database-backed checks must execute with an isolated test schema; report skips separately from passes.
- Assert HTTP 500 uses and caches an existing mapped local file, while other statuses and missing files keep the original redirect fallback.
- Assert target 403 recovery/exhaustion, non-403 failure without retries or local fallback, empty-body rejection, one-byte reads, redirect-chain validation, and cancellation cleanup.
- Assert only 4xx/5xx bodies are captured, sensitive JSON fields are redacted, oversized bodies use the truncation marker, and client responses remain unchanged.
- Assert PostgreSQL migration adds `response_body` idempotently and canceled Emby video streams return 499.

## 7. Wrong vs Correct

Wrong:

```go
key := rawURL + "\x00" + userAgent
```

Correct:

```go
key := mediaID + "\x00" + userAgent
```

The correct key follows the project's immutable media-to-STRM identity and avoids misses caused by volatile URL query values.
