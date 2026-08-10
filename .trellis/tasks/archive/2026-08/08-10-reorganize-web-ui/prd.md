# Reorganize Web UI Around Emby Core

## Goal

Reorganize the maintained Web UI around the capabilities that remain after the
Emby-core product reduction. A viewer should understand the product through
four primary destinations, while an administrator should enter one coherent
management space instead of navigating a collection of unrelated root pages.

The reorganization must preserve every retained capability, existing media and
playback behavior, stable deep links, and backend API contracts. It is an
information-architecture and interaction change, not another capability
retirement or a visual rebrand.

## Background and Confirmed Facts

- The current viewer navigation exposes Home, Libraries, Poster Wall,
  Discover, Search, DLNA, AI, Favourites, Playlists, and History as separate
  destinations. Several of these are alternate views or parts of one user
  journey rather than independent product areas.
- Local search is available without AI, but the current sidebar hides the
  entire Search entry behind `can_use_ai`. Direct route access does not apply
  the same permission check.
- The current AI page duplicates natural-language search already available on
  Search; its distinct value is recommendations derived from watch history.
- Poster Wall performs a broad cross-library fetch whenever that page opens;
  merging it into Libraries must not move that cost into the default overview.
- Administration is split between `/admin`, `/storage`, and many independent
  admin-only root routes.
- The Discover catalog still contains 12 retained sections: eight TMDb
  sections, three Douban sections, and the Bangumi daily calendar. Provider
  enablement and `can_view_discover` remain part of the contract.
- Desktop and mobile currently share one drawer-oriented navigation model.
  The Web package has lint and build scripts but no existing automated browser
  test framework.

## Requirements

### R1. Viewer Information Architecture

- The viewer's primary navigation contains Home, Libraries, Discover, and Me.
- Search is a global header action and route, not a fifth primary navigation
  destination.
- A user without `can_view_discover` does not see or enter Discover; the other
  viewer destinations remain available.
- Desktop and mobile present the same four-destination hierarchy. Mobile uses
  a persistent bottom navigation for these destinations rather than requiring
  the drawer for routine viewing.

### R2. Home

- Preserve featured content, Continue Watching, and Recently Added.
- Move AI recommendations from the standalone AI page into Home and show them
  only when `can_use_ai_assistant` and the configured provider make the feature
  available.
- Recommendation failure or unavailability must not block the rest of Home.

### R3. Libraries and Poster View

- `/libraries` owns both the existing library overview and a poster view,
  selected through `view=library|poster`.
- The library overview is the default and must not trigger poster-wall bulk
  loading.
- Poster data loads only after the poster view is selected and is fetched in
  explicit batches. Each request uses 50 items, each batch starts at most three
  requests, and the user requests the next batch with a Load More control.
- Poster batches preserve library order and the API's stable per-library order,
  de-duplicate by media ID before series grouping, expose retryable failures,
  and stop when every library has reached its reported total.
- `/library/:id` remains the stable route for an individual library.

### R4. Discover

- Preserve the current TMDb sections: daily trending, weekly trending, latest
  movies, latest series, popular movies, popular series, top-rated movies, and
  upcoming movies.
- Preserve Douban popular movies, popular series, and top-rated movies.
- Preserve the Bangumi daily calendar.
- Continue honoring `can_view_discover`, provider enablement, the existing
  section selection, cache behavior, and disabled-provider behavior.
- When a saved selection contains disabled or removed sections, discard only
  unavailable keys; if none remain, use the enabled default sections, or show
  the existing empty selection state when no provider is enabled.

### R5. Me

- Add `/me` as the viewer-owned area for Favourites, Playlists, and Watch
  History, with URL-addressable tab state.
- Preserve playlist creation, deletion, detail navigation, history cleanup,
  resume actions, and favourite playback behavior.
- Keep `/playlist/:id` stable so playlist detail links and bookmarks do not
  need a second compatibility route.
- Keep account Profile and playback profiles in the user menu; they are
  settings, not primary viewing destinations.

### R6. Search and AI

- `/search` remains available to every authenticated user for local media
  search and currently supported external provider results.
- `/search?mode=ai` is the canonical natural-language search mode and is
  available only with `can_use_ai` and an available AI provider.
- A user without `can_use_ai` who follows an AI-mode URL is returned to normal
  search without an AI request; ordinary search must remain usable.
- Remove the duplicate standalone AI destination after its search and
  recommendation responsibilities have moved to Search and Home.

### R7. Casting and Playback Context

- Remove DLNA from primary navigation. With `can_cast`, keep a stable header
  action to the full selector and Cast controls in media detail and the player;
  contextual controls use the current media while the full selector lets the
  user choose one.
- Without `can_cast`, no Cast control is rendered and the full deep link is
  denied after permission loading.
- Preserve `/dlna` as a permission-protected deep link.
- Keep `/media/:id` and `/play/:id` stable and preserve the current playback
  return chain, source selection, progress, subtitle, and direct-play behavior.

### R8. Administration Space

- `/admin/*` is the canonical location for every retained administrator page.
- The management navigation groups capabilities as Media Intake, Storage and
  Cleanup, Task Status, Users and Integrations, and System Settings.
- `/admin` provides an overview from which every retained management
  capability is reachable in at most two interactions.
- Reuse the current specialist pages and panels inside a shared management
  shell; do not merge all operations into one oversized page.
- Management entry points live in the administrator user menu or explicit
  space switcher and do not expand the viewer's four primary destinations.

### R9. Access Consistency

- Route rendering and navigation visibility consume one shared access
  declaration for administrator and permission requirements.
- Direct navigation, browser refresh, and menu navigation must produce the same
  authorization result for Discover, DLNA, AI mode, and all admin pages.
- Permission loading must not briefly expose protected content or permanently
  redirect an allowed user before their permissions are available.
- Permission state is bound to the current user. Login/account changes clear
  stale state and load that user's permissions; a load failure shows a
  retryable access state instead of being treated as a denial.
- Backend authorization remains authoritative; the Web guard is a consistent
  user-experience boundary, not a replacement security layer.

### R10. Compatibility and Responsive Behavior

- Legacy viewer and administrator URLs redirect to their canonical locations
  with browser history replacement.
- `/library/:id`, `/playlist/:id`, `/media/:id`, and `/play/:id` remain
  unchanged.
- Navigation, overlays, content, and bottom actions must not overlap at common
  phone, tablet, and desktop widths. Keyboard focus, Escape behavior, and
  route-change closing behavior remain usable.
- Primary and management navigation use semantic labels, visible focus,
  `aria-current` for the active destination, at least 44x44 CSS-pixel touch
  targets, safe-area padding, reduced-motion behavior, and drawer focus
  trapping/return.
- Existing visual tokens, components, icons, and interaction conventions are
  reused. No new UI framework or design-system dependency is introduced.
- No backend route, payload, database, or permission schema changes are part of
  this task.

## Acceptance Criteria

- [ ] An eligible viewer sees exactly Home, Libraries, Discover, and Me as the
      primary destinations on desktop and mobile; an ineligible viewer sees the
      same structure without Discover and cannot open `/discover` directly.
- [ ] Global Search remains reachable and returns local results for a user with
      no AI permission. AI mode is visible and callable only with `can_use_ai`.
- [ ] Home still shows featured, Continue Watching, and Recently Added content;
      AI recommendations appear there only when allowed and available, and a
      recommendation failure does not fail Home.
- [ ] `/libraries` defaults to the library overview, switches to the poster
      view through `view=poster`, performs no poster fetch before that switch,
      and loads at most three 50-item requests per explicit batch. Results keep
      stable library/API order, contain no duplicate media IDs, retry failed
      pages, and stop at the reported per-library totals.
- [ ] Discover exposes all 12 retained TMDb, Douban, and Bangumi sections when
      all providers are enabled; disabling TMDb, Douban, or Bangumi leaves 4,
      9, or 11 sections respectively, and disabling all providers produces the
      empty state. Saved unavailable selections are filtered and fall back to
      enabled defaults without affecting healthy provider rows.
- [ ] `/me` provides working Favourites, Playlists, and Watch History tabs;
      playlist detail, history cleanup/resume, and favourite playback continue
      to work.
- [ ] DLNA is absent from primary navigation. With `can_cast`, the header links
      the full selector and media detail/player expose current-media Cast
      controls with accessible names; without it, controls are hidden and
      `/dlna` is denied.
- [ ] Every retained management capability is under `/admin/*`, grouped by the
      five approved domains and reachable within two interactions from
      `/admin`; non-admin users cannot enter any canonical or legacy admin URL.
- [ ] `/poster-wall`, `/favourites`, `/playlists`, `/history`, `/ai`, and every
      former admin root URL redirect to the documented canonical destination
      without a redirect loop. `/admin?tab=library|users|api` follows its
      documented mapping, and denied `/ai` mode settles on ordinary `/search`.
- [ ] `/library/:id`, `/playlist/:id`, `/media/:id`, and `/play/:id` remain
      stable, and navigating into and back from playback preserves the current
      source-page return behavior.
- [ ] Navigation visibility and direct-route access agree for administrator,
      ordinary-user, and relevant permission combinations after refresh and
      after permissions load. Switching users cannot reuse the previous user's
      permissions, and a permission request failure is retryable without
      exposing protected content or redirecting as a denial.
- [ ] At 390x844, 768x1024, and 1440x900, primary navigation, header actions,
      user menu, management navigation, and page content are usable without
      incoherent overlap or horizontal overflow.
- [ ] Keyboard-only checks confirm visible focus, logical order, Escape and
      focus return for drawers/menus, semantic labels and `aria-current` for
      navigation, 44x44 minimum touch targets, reduced motion, and safe-area
      spacing for the mobile bottom bar.
- [ ] Missing `view`, `tab`, and `mode` values render their documented defaults;
      duplicate or invalid values are replace-normalized to those defaults.
      Browser Back/Forward restores valid state without duplicate requests.
- [ ] Web lint and production build pass, focused browser smoke checks cover
      the route/access/viewport matrix, and `git diff --check` passes.

## Out of Scope

- Adding or removing backend capabilities, providers, APIs, permission fields,
  database schema, or Emby protocol behavior.
- Adding new Discover feeds or changing ranking, provider fallback, or cache
  policy.
- Redesigning media cards, the player, metadata editing, or the overall brand.
- Replacing React Router, Zustand, Tailwind, Lucide, or the current component
  system.
- Adding a browser-test framework solely for this reorganization.
- Removing legacy URLs instead of redirecting them.
