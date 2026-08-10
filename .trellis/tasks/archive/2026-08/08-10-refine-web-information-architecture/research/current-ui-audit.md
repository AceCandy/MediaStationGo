# Current UI Audit

## Navigation Root Cause

- `web/src/components/layoutNavigation.ts:49-57` derives each management parent
  from the content route whose URL equals `/admin/<group>`.
- `web/src/components/LayoutSidebarContent.tsx:195-224` renders that parent as a
  route link and then renders nested child links, creating dual semantics.
- `web/src/appRoutes.tsx:207-297` confirms the roots also own real page
  elements; Media and Storage content must be retained as named leaves.

## Duplicate Entry Evidence

- `web/src/pages/AdminPage.tsx:6-20` is a shortcut-only Admin overview.
- `web/src/pages/AdminPage.tsx:24-36` mixes Media shortcuts with the real
  `AdminLibraryPanel`.
- `web/src/pages/AdminPage.tsx:40-54` is a shortcut-only Integration overview.
- `web/src/pages/StoragePage.tsx:55-76` repeats cross-domain destinations.
- `web/src/pages/RecycleBinPage.tsx:89-92` jumps out of management to viewer
  Home even though persistent navigation is present.

## Page Composition Evidence

- `web/src/pages/TasksPage.tsx:155-199` shows active and recent background jobs
  with a 3-second refresh.
- `web/src/pages/SchedulerPage.tsx:7-83` shows scheduled jobs and run-now actions
  with a 5-second refresh.
- `web/src/pages/StatsPageSections.tsx:59-140` duplicates storage/business
  totals and live metrics, then adds a low-value refresh-definition section.
- `web/src/pages/LibrariesPageSections.tsx:96-130` renders the same libraries as
  an entry grid and again as shelves.

## Field Evidence

- `web/src/components/APIConfigsPanel.tsx:99-139` repeats configured state in a
  key column, status column, and an Eye action that only displays a toast.
- `web/src/pages/AdminLibraryPanelSections.tsx:49-54,89-104` confirms cover URL
  and path label are real optional fields and must not be deleted.
- `web/src/pages/settingsGroupGeneral.ts:34-65` confirms public URL, playback
  mappings, redirect prefixes, and FFprobe values have runtime behavior.
- `web/src/pages/settingsGroupSystemUpdate.ts:17-29` confirms Compose directory
  and custom command are required for non-default deployments.
- `web/src/pages/NotifyChannelsPage.tsx:49-80,110-128` confirms Bot polling is a
  real runtime action, suitable for disclosure but not removal.
- `web/src/components/AppFooter.tsx:13-21` and its only call site on the Login
  page show the community-link setting no longer describes authenticated UI.

## Retained Boundaries

- Users, External APIs, Notifications, and AI Sessions have distinct state and
  actions and remain separate leaves.
- Files, STRM, Duplicates, Recycle Bin, Profile, playback profiles, and DLNA
  remain separate workflows.
- TMDb, Douban, Bangumi, OpenAI, and Adult provider configuration remains in
  the retained API surface; this task only reorganizes presentation.
