# Current Web UI and Capability Inventory

## Purpose

This snapshot records the repository evidence used to plan the UI
reorganization. Paths and line anchors refer to the worktree inspected on
2026-08-10. It is context for implementation and review, not a second source of
product requirements.

## Route and Navigation Baseline

- `web/src/appRoutes.tsx:48-85` defines one flat route array. Viewer routes
  include Home, Libraries, Discover, Search, Favourites, Playlists, media and
  playlist detail, Player, Profile, DLNA, History, Poster Wall, AI, and playback
  profiles. Files, Storage, Duplicates, Scheduler, Tasks, Recycle, STRM,
  Notifications, Settings, admin Assistant, Stats, and Admin are `adminOnly`.
- `web/src/App.tsx:54-80` wraps only `adminOnly` entries in `RequireAdmin`.
  `web/src/components/RequireAuth.tsx:16-22` redirects a non-admin user to `/`.
- `web/src/components/layoutNavigation.ts:39-87` separately restates navigation
  access. Search carries `can_use_ai`, Discover carries `can_view_discover`,
  DLNA carries `can_cast`, and the viewer AI page carries
  `can_use_ai_assistant`.
- `web/src/components/LayoutSidebarContent.tsx:62-72` filters menu visibility,
  but that filtering does not protect the route. The result is a known mismatch
  for Search and AI direct URLs.
- `web/src/components/useLayoutPermissions.ts:6-24` obtains effective
  permissions and lets admin/super users pass. The same resolved state should
  feed route and navigation decisions.
- `web/src/stores/permissions.ts:5-62` initializes with empty permissions and
  `isLoading=false`, has no loaded-user identity/readiness flag, and does not
  reset loading state in `clearPermissions`. Those fields cannot distinguish a
  first render, a loaded empty set, or stale state from another login.

## Retained Viewer Capabilities

| Capability | Current evidence | Planning consequence |
|---|---|---|
| Home | `web/src/pages/HomePage.tsx:18-83` loads featured, recent media, and unfinished history | preserve aggregation; add recommendations as an independent section |
| Libraries | `web/src/pages/LibrariesPage.tsx:12-87` loads library previews and admin repair actions | retain as default Libraries view |
| Poster Wall | `web/src/pages/PosterWallPage.tsx:11-55` fetches up to 2,000 items per library path and caps cards | merge into Libraries and load incrementally only when selected |
| Search | `web/src/pages/SearchPage.tsx:8-53` combines local, external-provider, and optional AI intent results | keep one global Search route; permission only AI mode |
| AI viewer page | `web/src/pages/AIAssistantPage.tsx:8-47` contains smart search plus history-based recommendations | move search to Search and recommendations to Home |
| Favourites | `web/src/pages/FavouritesPage.tsx:8-78` lists favourite media | compose under Me |
| Playlists | `web/src/pages/PlaylistsPage.tsx:10-88` creates/deletes lists and links detail | compose list under Me; preserve detail route |
| Watch History | `web/src/pages/WatchHistoryPage.tsx:19-141` paginates, resumes, removes, and clears history | compose under Me without losing actions |
| DLNA | `web/src/pages/DlnaPage.tsx:10-50` discovers LAN renderers and casts selected media | move entry to context; keep guarded deep link |
| Media/Player | `web/src/pages/MediaDetailPage.tsx:14-57` and `PlayerPage.tsx:18-100` own detail, playback, progress, and return logic | keep paths and return chain unchanged |

## Discover Inventory

`internal/handler/discover_extra.go:29-42` contains the retained catalog:

1. TMDb daily trending
2. TMDb weekly trending
3. TMDb latest movies
4. TMDb latest series
5. TMDb popular movies
6. TMDb popular series
7. TMDb top-rated movies
8. TMDb upcoming movies
9. Douban popular movies
10. Douban popular series
11. Douban top-rated movies
12. Bangumi daily calendar

`internal/handler/discover_extra.go:248-306` filters sections through API
provider enablement, and `:84-87` returns disabled metadata for a disabled feed.
`web/src/pages/DiscoverPageSections.tsx:30-67` identifies the three sources and
persists user section choices. The UI reorganization must not alter this
catalog or persistence contract.

## Administration Baseline

- `web/src/pages/AdminPage.tsx:9-67` combines library, user, and external API
  panels and links to Files, Storage, Notifications, and admin Assistant.
- `web/src/pages/AdminPage.tsx:9-32` recognizes exactly `library`, `users`, and
  `api` query tabs; missing or unknown values currently select `library`.
- `web/src/pages/StoragePage.tsx:26-76` provides storage statistics plus links
  to Files, Duplicates, Recycle, STRM, Scheduler, Tasks, Stats, Notifications,
  and Assistant. Admin and Storage therefore act as overlapping entry pages.
- `web/src/appRoutes.tsx:72-85` leaves these capabilities at independent root
  URLs even though all are admin-only.
- `web/src/pages/AssistantChatPage.tsx:115` uses an unconditional fixed
  two-column grid, which is a known narrow-screen overflow risk when hosted in
  a management shell.

## Responsive and Verification Baseline

- `web/src/components/LayoutSections.tsx:36-109` uses a desktop sidebar above
  the `lg` breakpoint and a fixed mobile drawer below it. Both render the same
  `LayoutSidebarContent`.
- `web/src/components/LayoutHeaderSections.tsx:64-90,146-169` contains the
  mobile menu and Search shortcut; narrow viewports hide the full Search field.
- `web/src/components/useLayoutSidebar.ts:5-39` closes the mobile drawer on
  route changes and maintains active groups.
- `web/package.json:6-10` provides `dev`, `build`, `preview`, and `lint` scripts
  but no current test/browser-test command.

## Pagination and Discover Reconciliation Evidence

- `internal/service/media_listing.go:18-27` defaults media listing to 50 items,
  clamps the existing endpoint at 2,000, and normalizes page numbers below one.
- `internal/repository/media_view_repository.go:145-160` applies offset/limit
  after a deterministic release date, year, updated time, created time, and
  media ID ordering. The ID is the final tie-breaker required for stable
  per-library offset pagination.
- `web/src/pages/DiscoverPage.tsx:31-55` filters persisted section keys through
  the enabled section catalog and uses enabled defaults when no saved key
  survives. `:61-75` removes unavailable active keys and uses the existing
  empty selection state when none remain. `:95-133` isolates row failures and
  preserves cached rows when a failed response has no replacement items.

## Scope Boundary from the Active Core Reduction

The separate `08-09-focus-emby-core` task removes retired cloud, tracker, and
conversion surfaces while explicitly preserving ordinary media search,
TMDb/Douban/Bangumi discovery, AI search, local libraries, metadata, favourites,
playlists, playback history, users, direct playback, DLNA, and retained admin
operations. This task reorganizes those surviving Web capabilities and does not
change that retirement task's backend or data decisions.
