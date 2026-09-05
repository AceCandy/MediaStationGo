# On-Demand STRM Target Inspection

## 1. Scope / Trigger

- Apply when exposing the current contents of a local `.strm` sidecar to an authenticated Web client.
- STRM target inspection is optional detail data and must not add filesystem reads to the general media detail or version-list paths.

## 2. Signatures

- API: `GET /api/media/:id/strm-target`.
- Service: `MediaService.GetSTRMTargetVisible(ctx, mediaID, visibility) (string, error)`.
- Web client: `mediaAPI.getSTRMTarget(mediaID) -> Promise<string>`.

## 3. Contracts

- `:id` is an exact concrete `Media.ID`, not a logical metadata ID.
- The service applies the same NSFW, allowed-library, and hidden-library visibility as media detail before reading the file.
- Only a visible media whose `Media.Path` ends in `.strm` is read. Parsing reuses `readLocalSTRMTarget`, including BOM/comment handling, absolute local media validation, and HTTP/HTTPS normalization.
- Success is exactly `{ "target": string }`; it never serializes `Media`, the sidecar path, or cached `Media.STRMURL`.
- Web requests run only for the selected `.strm` version. Selection changes clear the previous value, and stale responses cannot replace the current target.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Exact visible `.strm` media has a valid target | `200` with only `target` |
| Media is missing, hidden, disallowed, NSFW-hidden, or addressed by metadata ID | `404` without a target |
| Media path is not `.strm` or the sidecar has no valid target | `404` without a target |
| Sidecar read fails | Internal/canceled error response; general detail remains unaffected |
| Web selection is not `.strm` | Make no target request and render no target row |

## 5. Good / Base / Bad Cases

- Good: selecting a visible `.strm` version reads its current first valid target and displays it below the sidecar path.
- Base: selecting a normal media file performs no extra request.
- Bad: return cached `Media.STRMURL` through `GET /api/media/:id`, or accept a caller-supplied filesystem path.

## 6. Tests Required

- Handler: visible media returns the parsed target as the only response field; hidden media and a logical metadata ID return `404`.
- Service parser: comments/BOM, local absolute video paths, HTTP/HTTPS normalization, invalid targets, and unreadable files.
- Web: lint and production build; verify selection changes clear stale target state.

## 7. Wrong vs Correct

```go
// Wrong: caller controls the path, bypassing media visibility.
target, err := readLocalSTRMTarget(c.Query("path"))

// Correct: exact visible media identity owns the only readable path.
target, err := svc.Media.GetSTRMTargetVisible(ctx, c.Param("id"), visibility)
```
