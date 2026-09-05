# On-Demand STRM Target Inspection

## 1. Scope / Trigger

- Apply when exposing the current contents of a local `.strm` sidecar to an authenticated Web client.
- STRM target inspection is optional detail data and must not add filesystem reads to the general media detail or version-list paths.
- Apply the deletion contract when an administrator removes the resolved local target file or its parent directory from media details.

## 2. Signatures

- API: `GET /api/media/:id/strm-target`.
- Service: `MediaService.GetSTRMTargetVisible(ctx, mediaID, visibility) (string, error)`.
- Web client: `mediaAPI.getSTRMTarget(mediaID) -> Promise<string>`.
- Admin preview API: `GET /api/admin/media/:id/strm-delete-target` -> `{ target_path, parent_path? }`.
- Admin delete API: `DELETE /api/admin/media/:id/strm-delete-target` with `{ delete_parent: boolean }`.
- Service: `FileManagerService.ResolveSTRMDeleteTarget(ctx, mediaID)` and `DeleteSTRMTarget(ctx, mediaID, deleteParent)`.

## 3. Contracts

- `:id` is an exact concrete `Media.ID`, not a logical metadata ID.
- The service applies the same NSFW, allowed-library, and hidden-library visibility as media detail before reading the file.
- Only a visible media whose `Media.Path` ends in `.strm` is read. Parsing reuses `readLocalSTRMTarget`, including BOM/comment handling, absolute local media validation, and HTTP/HTTPS normalization.
- Success is exactly `{ "target": string }`; it never serializes `Media`, the sidecar path, or cached `Media.STRMURL`.
- Web requests run only for the selected `.strm` version. Selection changes clear the previous value, and stale responses cannot replace the current target.
- Delete requests never accept a filesystem path. The service re-reads the concrete media row, sidecar, current `ffprobe.path_mappings`, and filesystem state for every preview and delete.
- Direct local targets must be existing regular files inside FileManager allowed roots. HTTP/HTTPS targets are deletable only when the longest matching probe-path mapping resolves them to an existing regular file; that mapping's local prefix is the trusted root.
- FileManager treats the fixed `/mnt/all` directory as an allowed root when it exists; this trust does not extend to `/mnt` or prefix-similar paths such as `/mnt/all-other`.
- Resolve real paths before deletion, reject a symlink target and filesystem-root trust boundary, and never return a parent path when it is the trusted root or contains the corresponding `.strm` sidecar.
- `delete_parent=false` removes only the target file. `delete_parent=true` recursively removes only the previewed parent directory. Neither path updates the `.strm` sidecar or Media/STRM database records.
- The confirmation UI derives its displayed final absolute path from `delete_parent`: `target_path` when unchecked and `parent_path` when checked.

## 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Exact visible `.strm` media has a valid target | `200` with only `target` |
| Media is missing, hidden, disallowed, NSFW-hidden, or addressed by metadata ID | `404` without a target |
| Media path is not `.strm` or the sidecar has no valid target | `404` without a target |
| Sidecar read fails | Internal/canceled error response; general detail remains unaffected |
| Web selection is not `.strm` | Make no target request and render no target row |
| Delete route caller is not an administrator | `401`/`403` before target resolution |
| Local target is missing, non-regular, outside allowed roots, or a symlink | No delete preview; deletion fails without filesystem mutation |
| `/mnt/all` is absent or inaccessible | Omit that allowed root without affecting other configured roots |
| HTTP/HTTPS target has no usable local mapping | No delete preview; deletion fails without filesystem mutation |
| Requested parent is a trusted root or contains the sidecar | Omit `parent_path`; reject `delete_parent=true` |

## 5. Good / Base / Bad Cases

- Good: selecting a visible `.strm` version reads its current first valid target and displays it below the sidecar path.
- Base: selecting a normal media file performs no extra request.
- Bad: return cached `Media.STRMURL` through `GET /api/media/:id`, or accept a caller-supplied filesystem path.
- Good: checking “delete parent” immediately changes the confirmation path to `parent_path`, and the server recomputes that same choice before removal.
- Bad: pass the path displayed by React to a generic file-delete endpoint, or recursively remove a directory containing the owning sidecar.

## 6. Tests Required

- Handler: visible media returns the parsed target as the only response field; hidden media and a logical metadata ID return `404`.
- Service parser: comments/BOM, local absolute video paths, HTTP/HTTPS normalization, invalid targets, and unreadable files.
- Web: lint and production build; verify selection changes clear stale target state.
- Service deletion: direct local file, mapped remote file outside FileManager roots, parent deletion, missing mapping, protected root, sidecar-containing parent, and symlink rejection; assert the sidecar and media row remain.
- Handler deletion: both routes are registered below the admin middleware and reject unauthenticated/non-admin callers.

## 7. Wrong vs Correct

```go
// Wrong: caller controls the path, bypassing media visibility.
target, err := readLocalSTRMTarget(c.Query("path"))

// Correct: exact visible media identity owns the only readable path.
target, err := svc.Media.GetSTRMTargetVisible(ctx, c.Param("id"), visibility)
```

```go
// Wrong: a client-selected path can bypass STRM resolution and confirmation.
err := svc.FileManager.Delete(c.Query("path"))

// Correct: media identity plus the boolean choice are the only delete inputs.
path, err := svc.FileManager.DeleteSTRMTarget(ctx, c.Param("id"), req.DeleteParent)
```
