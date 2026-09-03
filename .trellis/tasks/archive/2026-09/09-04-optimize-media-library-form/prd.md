# Optimize Media Library Form Interactions

## Goal

Make media-library path editing visible and consistent between create and edit flows, while moving cover selection to an image upload available only after creation.

## Background

- The create dialog currently hides the optional root name inside a per-root advanced section (`web/src/pages/AdminLibraryPanelSections.tsx:146`).
- The create dialog also exposes a cover URL inside a top-level advanced section (`web/src/pages/AdminLibraryPanelSections.tsx:81`).
- The edit dialog keeps the root name in a collapsed advanced section and adds roots through two browser prompts (`web/src/pages/AdminLibraryTable.tsx:265`, `web/src/pages/useAdminLibraryPanel.ts:150`).
- Cover editing currently accepts a URL through a browser prompt; the repository has no library-cover upload endpoint (`web/src/pages/useAdminLibraryPanel.ts:159`, `web/src/api/library.ts:147`, `internal/handler/media.go:122`).

## Requirements

- R1. In both create and edit flows, each editable path row is always expanded and presents an optional name field on the left and the path field on the right at layouts that have enough width; narrow layouts may stack them.
- R2. The create dialog contains no advanced-settings sections and no cover field. A cover can only be configured after the library has been created.
- R3. The edit dialog places a visually prominent add-path control immediately after the existing path list. Adding a path uses inline name and path inputs consistent with the create dialog rather than browser prompts.
- R4. The edit dialog changes cover configuration from URL entry to local image upload, shows “recommended 16:9”, and retains an explicit clear-cover action.
- R5. Existing URL-backed covers continue to display until the user replaces or clears them; the change must not require data migration.
- R6. Uploaded files are validated at the HTTP boundary and persisted below the configured application data directory. Only authenticated administrators may upload or clear a library cover.
- R7. Successful path or cover changes refresh the media-library list and preserve the existing success/error feedback pattern.

## Technical Notes

- Root drafts already contain `name`, `path`, `enabled`, and `sort_order`; no data-model change is required (`web/src/pages/adminLibraryPanelModel.ts:9`, `web/src/pages/adminLibraryPanelModel.ts:30`).
- Library cover storage remains represented by `Library.cover_url`; an uploaded cover can therefore be exposed as an application URL without a database migration (`internal/model/library_media.go:8`).
- The existing stored-image helpers validate JPEG, PNG, GIF, WebP, and BMP image bytes and support atomic persistence under `App.DataDir`, but the metadata artwork-selection model is not directly reusable for library IDs (`internal/service/image_asset_storage.go:21`, `internal/service/artwork_store.go:190`). The implementation should reuse the storage helpers without creating a second general artwork subsystem.

## Acceptance Criteria

- [ ] AC1. Opening the create dialog shows each root as visible “name (optional)” and “path” inputs with no path advanced disclosure.
- [ ] AC2. The create dialog has no media-library advanced disclosure and sends no user-selected cover.
- [ ] AC3. Opening the edit dialog shows each editable root with visible name and path inputs in the same order and responsive layout as creation.
- [ ] AC4. A full-width add-path control appears directly after the edit path list; activating it exposes inline name/path inputs and saving adds the root without `window.prompt`.
- [ ] AC5. The edit cover action opens a native image file chooser, displays “recommended 16:9”, uploads an accepted image, refreshes the library, and shows the saved cover.
- [ ] AC6. Non-image or oversized uploads receive a clear client-visible error and do not change the stored cover.
- [ ] AC7. Existing external cover URLs remain displayable, and the edit dialog provides a clear-cover action. Replacing or clearing a cover does not affect library roots or media.
- [ ] AC8. Relevant focused backend tests, Web lint/build, and `git diff --check` pass.

## Out of Scope

- Setting a cover during library creation.
- Image cropping, resizing, or an asset-management gallery.
- Refactoring unrelated media artwork storage.
- Changing library name, type, visibility, scanning, or media contents.
