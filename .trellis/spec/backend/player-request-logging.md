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
- Player request `body` remains the sanitized request body. `response_body` is populated only for status 400 or greater.
- Failed response content is capped at 64 KiB and uses the same sensitive JSON-field redaction as request bodies.
- Non-JSON content containing a sensitive field marker is replaced as a whole instead of persisted verbatim.
- Successful, redirect, and media-byte responses never persist response content.
- 视频流 handler 不通过 `Item` 或 `PlayableMediaID` 解析 metadata、season、series ID；非 concrete media ID 返回 404。

## 4. Validation & Error Matrix

| Condition | Behavior |
|---|---|
| Same media ID and User-Agent before TTL | Return cached Location without an upstream request |
| Different media ID with the same URL and User-Agent | Resolve independently |
| Different User-Agent or expired entry | Resolve again |
| Upstream HTTP 500 with a valid `ffprobe.path_mappings` file | Serve the local file and cache the fallback |
| Other upstream error, missing local file, or invalid Location | Redirect to the original URL without caching the failure |
| Emby video stream request is canceled | Record status 499 with no response body |
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
- Assert HTTP 500 uses and caches an existing mapped local file, while other statuses and missing files keep the original redirect fallback.
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
