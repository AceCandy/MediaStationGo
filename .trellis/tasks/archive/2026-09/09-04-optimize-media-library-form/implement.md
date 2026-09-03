# Implementation Plan: Optimize Media Library Form Interactions

## Change Boundary

Smallest behavior gap: currently optional path names and cover URLs are hidden behind advanced disclosures, edit-time additions use prompts, and no cover upload exists. The behavior lives in the two admin dialogs, their hook/API calls, and the library handler/service boundary.

Do not change library naming/type rules, scan behavior, media artwork selection, or unrelated admin UI. No local refactor is planned beyond deleting props/state made obsolete by this task.

## Steps

1. Add focused cover persistence behavior at the media service boundary.
   - Reuse existing image validation, size bound, atomic file write, and image-serving helpers.
   - Add checks for accepted image upload, invalid/oversized input, replacement cache version, serving, and clearing.
2. Add authenticated library-cover HTTP routes and Web API methods.
   - Multipart upload and explicit clear are administrator-only; cover delivery remains authenticated.
   - Preserve the existing JSON cover update route for compatibility.
3. Simplify the create dialog.
   - Remove cover state/props/request value and both advanced disclosures.
   - Render visible optional-name and path columns for every root.
4. Align the edit dialog.
   - Use the same field order/layout for existing roots.
   - Replace prompt-based addition with one inline draft after the root list and a prominent full-width add control.
   - Replace URL prompting with native file upload, 16:9 guidance, and clear-cover action.
5. Review the complete diff independently for scope, accessibility labels, authenticated image rendering, stale draft state, and compatibility.

## Verification

- Focused Go test command for the changed service and handler packages (exact test regex selected after test names are added).
- `cd web && npm run lint`
- `cd web && npm run build`
- `git diff --check`
- Manual code-path review: create payload omits cover choice; add-root uses existing endpoint; upload/clear refreshes the selected library; existing external URLs still pass through image rendering.

## Rollback Points

- UI/API changes can be reverted independently of the schema because no migration is introduced.
- Uploaded files are confined to `App.DataDir/library-covers`; removal is safe only after confirming no library row still references the managed cover URL.
