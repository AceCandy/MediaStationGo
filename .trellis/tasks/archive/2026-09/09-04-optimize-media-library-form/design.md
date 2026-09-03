# Design: Optimize Media Library Form Interactions

## Boundaries

The change is limited to media-library administration. It does not change library identity, scanning, media metadata, or the shared artwork-selection model.

Expected product-code changes:

- `web/src/pages/AdminLibraryPanelSections.tsx`: remove create-time cover/advanced UI and render each root as visible name/path inputs.
- `web/src/pages/AdminLibraryTable.tsx`: align editable root fields, render an inline add-root row after the list, and expose upload/clear cover controls with the 16:9 hint.
- `web/src/pages/useAdminLibraryPanel.ts`: remove create cover state and browser prompts; own the minimal add-root draft and upload actions.
- `web/src/pages/AdminLibraryPanel.tsx`: pass the adjusted state/actions between the existing panel and dialogs.
- `web/src/api/library.ts`: add multipart upload and clear-cover calls while retaining existing JSON update compatibility.
- `internal/handler/media.go` and `internal/handler/routes_authenticated_core.go`: accept authenticated admin uploads, serve stored covers, and clear covers.
- `internal/service/media_library_roots.go` (or one adjacent focused service file): validate, atomically persist, serve, and clear library cover files.
- Focused Go tests adjacent to the service/handler behavior.

Explicitly excluded: image cropping/resizing, a reusable upload framework, database migrations, and refactoring shared media artwork.

## Path Interaction

Both create and edit rows use the existing `RootDraft` shape. At responsive widths, the optional name input is the left column and the path input is the wider right column; mobile stacks them. Existing edit rows retain their enable/save/delete actions.

The edit dialog owns at most one unsaved add-root draft. A full-width dashed “add path” control follows the existing list. Activating it replaces the control with inline name/path fields and save/cancel actions. Saving calls the existing `POST /libraries/:id/roots`, then refreshes the list. This avoids a new shared abstraction and removes both `window.prompt` calls.

## Cover Data Flow

```text
native file input
  -> multipart PUT /api/libraries/:id/cover
  -> bounded byte read + existing image-byte validation
  -> atomic write under App.DataDir/library-covers/<library-id-hash>/<content-hash>
  -> Library.cover_url = /api/libraries/:id/cover?v=<content-hash>
  -> refreshed library list
  -> existing image rendering through authenticated imageURL(...)
```

The service writes a content-versioned file before switching `cover_url`, deletes the new file if the database update fails, and removes the previous version after a successful switch. This preserves the old cover on database failure and prevents normal replacements from accumulating files. The content hash in the stored URL invalidates browser cache without additional state. The implementation reuses existing stored-image validation and atomic-write helpers within the service package; it does not attach a library to metadata artwork tables.

`GET /api/libraries/:id/cover` is authenticated like other artwork delivery and serves only the deterministic file for that library ID. `DELETE /api/libraries/:id/cover` is administrator-only, deletes the managed file when present, and clears `cover_url`. The existing `PATCH /api/libraries/:id` contract remains available so old external URL values and clients continue to work.

## Validation and Errors

- Bound reads to the existing artwork byte limit.
- Decode and accept only formats supported by the existing stored-image validator (JPEG, PNG, WebP, GIF, and BMP as currently implemented).
- Reject missing, oversized, or undecodable files with a client error; do not update `cover_url` unless persistence succeeds.
- Verify the library exists before changing files or database state.
- The 16:9 ratio is guidance only; no crop or ratio rejection is added.

## Compatibility and Rollback

No schema migration is required. Existing external cover URLs continue to render. Rolling back the code leaves uploaded files under `App.DataDir/library-covers`; they are harmless local data, while their stored `/api/libraries/:id/cover` URLs require the upload code to display. A code rollback should therefore be paired with restoring previous `cover_url` values if uploaded covers must remain visible.
