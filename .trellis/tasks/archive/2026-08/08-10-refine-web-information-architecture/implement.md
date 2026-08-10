# Refine Web Information Architecture - Implementation Plan

## Success Criteria

The task is complete when management parents are non-link groups, all retained
workflows remain reachable through leaves, duplicate shells and controls are
removed, settings sections are sidebar-addressable, related operational views
are composed without losing behavior, and Web verification passes.

## Ordered Checklist

### 1. Preserve and Correct Navigation Contracts

- [x] Separate management group definitions from route links.
- [x] Render management groups as expandable buttons and their routes as
      second-level links in desktop, collapsed, and mobile navigation.
- [x] Use segment-aware route matching and ensure only leaves receive
      `aria-current`.
- [x] Redirect `/admin` and empty group roots to deterministic leaves while
      preserving every legacy redirect and access guard.

Verify: direct, refresh, and sidebar navigation agree for admin/non-admin;
desktop and mobile active/expanded states remain usable.

### 2. Remove Duplicate Management Shells

- [x] Make `/admin/media` the Media Library Management leaf and remove its
      shortcut cards without removing `AdminLibraryPanel`.
- [x] Remove Admin and Integration overview shortcut pages from canonical UI.
- [x] Remove Storage shortcut and maintenance blocks while retaining all
      summary/breakdown data.
- [x] Remove the Recycle Home link and adjust outcome-oriented page copy.

Verify: every removed shortcut target remains reachable in the sidebar and by
its existing URL.

### 3. Compose Tasks and Reassign Statistics

- [x] Reuse background-task and scheduler sections in one Task Center while
      preserving independent state, polling, actions, and errors.
- [x] Keep `/admin/tasks/scheduler` compatible.
- [x] Move storage/media totals to Storage Overview and user total to User
      Management.
- [x] Keep one Runtime Monitor presentation for CPU, memory, disk, Go runtime,
      and goroutines; remove aggregate refresh/definition copy.

Verify: update-task links land where active/recent tasks are immediately
visible; both polling flows stop on unmount.

### 4. Route and Group System Settings

- [x] Add explicit routes and second-level links for General, Playback and
      Probe, Recognition Words, Content Access, and System Update.
- [x] Remove the local horizontal settings selector.
- [x] Reassign existing settings to the approved sections without changing
      keys or backend calls.
- [x] Use existing/native disclosure for advanced media-library, playback,
      probe, provider, Bot runtime, and update controls.
- [x] Correct login-community-link and Adult policy scope copy.

Verify: each section deep-links and refreshes correctly; saving updates only
dirty keys; every current setting remains editable.

### 5. Remove Redundant Controls and Viewer Duplication

- [x] Merge API key/status presentation and remove the no-op Eye action and
      encryption implementation copy.
- [x] Keep provider-specific fields conditional and available.
- [x] Replace Duplicates algorithm copy with outcome-oriented text.
- [x] Make library shelf headings the full-library entry and remove the
      duplicate entry-card grid.
- [x] Remove desktop header Discover and administrator notification shortcuts;
      retain Search, DLNA, and profile behavior.

Verify: retained provider configuration, library navigation, viewer routes,
Search, casting, and profile flows remain reachable.

### 6. Full Verification and Independent Review

- [x] Run `npm run lint` from `web/`.
- [x] Run `npm run build` from `web/`.
- [x] Run focused browser checks at 390x844, 768x1024, and 1440x900 for viewer
      and management navigation, Settings routes, Task Center, and Libraries.
- [x] Check keyboard group expansion, leaf current state, drawer Escape/focus
      behavior, direct routes, and legacy redirects.
- [x] Run targeted inventories for duplicate shortcuts, legacy internal links,
      settings keys, and retained TMDb/Douban/Bangumi capabilities.
- [x] Run `git diff --check` and an independent full-scope review.
- [x] Stop any service started for verification and delete temporary captures.

## Validation Commands

```bash
cd web && npm run lint
cd web && npm run build
git diff --check
python3 ./.trellis/scripts/task.py validate 08-10-refine-web-information-architecture
```

## Rollback Points

- Navigation projection and redirects form one rollback unit.
- Task/Stats composition is independent of Settings routing.
- Field disclosure and viewer duplicate cleanup can be reverted without route
  or data migration.

## Review Gates

- Do not remove a shortcut until its target is visible in management
  navigation.
- Do not remove a settings key or retained provider capability.
- Do not merge Files, STRM, Duplicates, Recycle, Notification, AI Session,
  Profile, playback profile, or DLNA workflows.
- Do not couple Task and Scheduler polling state.
- Do not add a dependency or generic navigation/settings framework.
