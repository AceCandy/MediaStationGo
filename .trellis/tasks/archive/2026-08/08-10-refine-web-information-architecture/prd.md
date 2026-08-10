# Refine Web Information Architecture

## Goal

Make the retained Web capabilities easier to understand by removing duplicate
entry pages, making management hierarchy semantically consistent, and placing
advanced fields in the correct context. This task changes information
architecture and presentation only; retained capabilities, backend contracts,
permissions, and stable deep links remain available.

## Confirmed Facts

- Management navigation currently derives a clickable group root from a real
  content route, so entries such as Media Intake both own content and expose
  nested destinations.
- The Admin overview, integration overview, Media Intake shortcuts, and
  Storage maintenance shortcuts repeat destinations already present in the
  persistent sidebar.
- Media library management and Storage overview are real workflows and must be
  retained as second-level pages, not removed with their shortcut shells.
- Tasks and Scheduler are small related operational views; Runtime Stats also
  mixes storage/business totals with live process metrics.
- System Settings uses component-local tabs, so its active section cannot be
  restored by URL or represented as sidebar navigation.
- Most low-frequency fields have real runtime behavior. They should be grouped
  or collapsed, not deleted.
- Viewer Me and Libraries already use URL-addressable views and should remain
  composed pages.

## Requirements

### R1. Navigation Semantics

- A navigation item is either a content destination or an expandable group,
  never both.
- Management Space remains a non-link section label.
- Media Intake, Storage and Cleanup, Tasks and Runtime, Users and Integrations,
  and System Settings are non-link expandable groups.
- Every retained management workflow appears as a visible second-level link.
- Only the actual destination link receives `aria-current="page"`.
- Direct `/admin` and group-root URLs terminate at a deterministic retained
  second-level page without redirect loops.

### R2. Management Destinations

- Media Intake contains Media Library Management, Files and Import, and STRM.
- Storage and Cleanup contains Storage Overview, Duplicates, and Recycle Bin.
- Tasks and Runtime contains Task Center and Runtime Monitor.
- Users and Integrations contains User Management, External APIs,
  Notification Channels, and AI Sessions.
- System Settings contains General, Playback and Probe, Recognition Words,
  Content Access, and System Update.
- TMDb, Douban, Bangumi, OpenAI, notification, and all other retained provider
  configuration stays reachable.

### R3. Duplicate Entry Removal

- Remove the Admin overview shortcut page and the Integration overview
  shortcut page from normal navigation.
- Remove shortcut blocks from Media Library Management and Storage Overview.
- Remove the Recycle Bin link back to the viewer Home page.
- Remove redundant desktop header shortcuts for Discover and administrator
  notification configuration; keep global Search, DLNA, and profile actions.
- Preserve compatibility redirects for previous root and deep-link URLs.

### R4. Page Composition

- Task Center presents active/recent background tasks and scheduled jobs on one
  page while preserving their independent refresh cadence, loading state,
  run-now action, and error feedback.
- Runtime Monitor keeps CPU, memory, disk, Go runtime, and goroutine data.
- Storage Overview owns media/library count, storage size, duration, and
  per-library/per-container breakdowns. User count belongs with User
  Management rather than Runtime Monitor.
- Libraries removes the duplicated library-entry grid by making each retained
  shelf heading the route into that full library. Full-library navigation must
  remain obvious and keyboard accessible.
- Duplicates, Recycle Bin, Files and Import, STRM, External APIs,
  Notifications, AI Sessions, Profile, playback profiles, and DLNA remain
  distinct workflows.

### R5. Settings Structure

- System Settings sections are URL-addressable and represented as second-level
  sidebar links rather than a duplicate horizontal selector.
- General contains common UI/metadata options.
- Playback and Probe contains public playback URL, path mappings, redirect
  resolution, FFprobe path, and concurrency.
- Content Access contains global Adult/NSFW policy and clearly distinguishes
  it from per-user/per-playback-profile policy.
- System Update keeps its executable controls visible while moving image,
  Compose directory, and custom command into an explicit advanced area.
- The community-link setting is renamed to describe its remaining login-page
  scope, unless the login footer is removed separately.

### R6. Field and Control Cleanup

- Remove the External API Eye/status action that only repeats configured state.
- Merge duplicate key/status table presentation into one scannable
  configuration summary without losing masked-key or Adult source-count data.
- Remove encryption implementation details from user-facing API page copy.
- Keep provider-specific fields available only for the provider that uses them.
- Keep media library path label and cover URL as optional advanced fields.
- Keep Bot polling controls reachable as Notification runtime controls.
- Keep playback mapping, FFprobe, and update execution settings editable in
  clearly labelled advanced sections.
- Replace the Duplicates implementation-algorithm paragraph with concise,
  outcome-oriented copy.
- Remove the low-value aggregate refresh/definition section from Runtime
  Monitor after its useful metrics are reassigned.

### R7. Viewer Duplication

- Preserve Home, Libraries, Discover, and Me as viewer destinations.
- Keep the Home Discover call-to-action and sidebar destination; remove the
  duplicate desktop header Discover shortcut.
- Keep Search as a global action and full result page without rendering two
  desktop search inputs simultaneously.
- Preserve Me tabs, Libraries view state, Search mode state, DLNA, Profile,
  playback profiles, media detail, playlists, and playback behavior.

### R8. Compatibility and Scope

- No backend route, payload, database, permission, provider, or capability
  change is part of this task.
- Existing canonical URLs remain stable where practical. Changed group roots
  use replace redirects and all existing legacy redirects continue to work.
- Responsive drawer, focus management, permission filtering, and active-route
  behavior must continue to work on desktop and mobile.
- No new UI framework, router, state library, or test dependency is added.

## Acceptance Criteria

- [ ] Every management group expands without navigation and every retained
      workflow is reachable through a second-level link.
- [ ] A management leaf is the only navigation element marked current.
- [ ] `/admin`, every group root, every canonical management URL, and every
      existing legacy admin URL terminates at an allowed retained page.
- [ ] Admin overview, integration overview, Media/Storage shortcut blocks,
      Recycle Home link, redundant header actions, API Eye action, and Runtime
      aggregate explanation are absent.
- [ ] Media Library Management and Storage Overview retain their real data and
      CRUD functionality after shortcut removal.
- [ ] Task Center shows active, recent, and scheduled tasks, keeps independent
      refresh/error behavior, and existing links to `/admin/tasks` land where
      active update work is visible.
- [ ] Storage Overview owns storage/media totals and Runtime Monitor contains
      one non-duplicated presentation of live process metrics.
- [ ] System Settings sections restore from direct URL and browser refresh,
      appear in the sidebar, and no duplicate top section selector remains.
- [ ] Optional and advanced fields remain editable; no retained provider or
      playback/update capability is removed.
- [ ] Libraries exposes one representation per library with an obvious link to
      `/library/:id`; the separate duplicate entry grid is gone.
- [ ] Viewer navigation and Me/Libraries/Search URL state continue to work.
- [ ] Web lint, production build, focused browser route/viewport checks, and
      `git diff --check` pass.

## Out of Scope

- Backend or database changes.
- Removing retained providers, feeds, search modes, casting, playback, or
  administrator operations.
- Visual rebranding or broad component redesign.
- Replacing page-specific data hooks with a new generic framework.
- Removing compatibility redirects.
