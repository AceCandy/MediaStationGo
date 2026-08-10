# Reorganize Web UI Around Emby Core - Technical Design

## 1. Design Objective

Make the remaining product shape visible in the UI without changing the
backend contract. The smallest reliable implementation reuses the existing
pages, React Router tree, permission store, responsive layout, and visual
tokens. New structure is limited to three things the current code cannot
express consistently:

1. one route manifest that owns both route access and navigation metadata;
2. viewer composition pages for Libraries and Me;
3. a shared management shell under `/admin/*`.

No replacement router, state library, UI kit, generic plugin architecture, or
new test framework is needed.

## 2. Information Architecture

```text
Authenticated application
|
+-- Viewer space
|   +-- Home
|   |   +-- Featured
|   |   +-- Continue Watching
|   |   +-- Recently Added
|   |   `-- AI recommendations (permission/provider gated)
|   +-- Libraries
|   |   +-- Library overview (default)
|   |   `-- Poster view (lazy, incremental)
|   +-- Discover (permission/provider gated)
|   `-- Me
|       +-- Favourites
|       +-- Playlists
|       `-- Watch History
|
+-- Global/context actions
|   +-- Search (normal or AI mode)
|   +-- DLNA cast
|   +-- Profile
|   `-- Playback profiles
|
`-- Management space (admin only)
    +-- Overview
    +-- Media Intake
    +-- Storage and Cleanup
    +-- Task Status
    +-- Users and Integrations
    `-- System Settings
```

Media detail, playlist detail, and the player remain workflow destinations,
not primary navigation items.

## 3. Canonical Route Map

### 3.1 Viewer and Context Routes

| Route | Responsibility | Access |
|---|---|---|
| `/` | Home aggregation | authenticated |
| `/libraries?view=library\|poster` | library and poster views | authenticated |
| `/library/:id` | library detail | authenticated |
| `/discover` | retained provider feeds | `can_view_discover` |
| `/search?mode=default\|ai` | local/external search and optional AI mode | authenticated; AI mode uses `can_use_ai` |
| `/me?tab=favourites\|playlists\|history` | viewer-owned collections and history | authenticated |
| `/playlist/:id` | playlist detail | authenticated |
| `/media/:id` | media detail | authenticated |
| `/play/:id` | direct player | authenticated |
| `/dlna` | full cast workflow deep link | `can_cast` |
| `/profile` | account profile | authenticated |
| `/play-profiles` | playback profile settings | authenticated |

### 3.2 Management Routes

| Group | Canonical routes | Reused capability |
|---|---|---|
| Overview | `/admin` | grouped status and shortcuts |
| Media Intake | `/admin/media`, `/admin/media/files`, `/admin/media/strm` | libraries, manual organize/file manager, STRM tools |
| Storage and Cleanup | `/admin/storage`, `/admin/storage/duplicates`, `/admin/storage/recycle` | storage summary, duplicates, recycle bin |
| Task Status | `/admin/tasks`, `/admin/tasks/scheduler`, `/admin/tasks/stats` | tasks, schedules, runtime/library statistics |
| Users and Integrations | `/admin/integrations`, `/admin/integrations/users`, `/admin/integrations/apis`, `/admin/integrations/notifications`, `/admin/integrations/assistant` | users, provider/API configuration, notifications, admin AI sessions |
| System Settings | `/admin/settings` | settings, recognition words, system update controls |

Every management route uses the existing administrator role check. The shared
management shell provides group navigation and a responsive content outlet;
specialist pages stay separate so their state and layout do not become one
large conditional component.

### 3.3 Compatibility Redirects

| Legacy route | Canonical target |
|---|---|
| `/poster-wall` | `/libraries?view=poster` |
| `/favourites` | `/me?tab=favourites` |
| `/playlists` | `/me?tab=playlists` |
| `/history` | `/me?tab=history` |
| `/ai` | `/search?mode=ai` |
| `/files` | `/admin/media/files` |
| `/strm` | `/admin/media/strm` |
| `/storage` or `/tools` | `/admin/storage` |
| `/duplicates` | `/admin/storage/duplicates` |
| `/recycle` | `/admin/storage/recycle` |
| `/tasks` | `/admin/tasks` |
| `/scheduler` | `/admin/tasks/scheduler` |
| `/stats` | `/admin/tasks/stats` |
| `/notify-channels` | `/admin/integrations/notifications` |
| `/assistant` | `/admin/integrations/assistant` |
| `/api-configs` or `/admin?tab=api` | `/admin/integrations/apis` |
| `/settings` | `/admin/settings` |

The former Admin tab variants map exactly as follows:

| Legacy query | Canonical target |
|---|---|
| `/admin?tab=library` | `/admin/media` |
| `/admin?tab=users` | `/admin/integrations/users` |
| `/admin?tab=api` | `/admin/integrations/apis` |
| `/admin?tab=<unknown>` | `/admin` |

Bare `/admin` becomes the canonical overview. The previous default library
panel remains one interaction away at `/admin/media`.

Redirect routes retain the same access metadata as their targets and use
history replacement. Administrator redirects are guarded before navigation so
a legacy URL cannot bypass the canonical admin guard.

The legacy `/ai` route is authenticated, not AI-permission protected. It
replace-redirects to `/search?mode=ai`; after permissions load, Search keeps AI
mode for `can_use_ai` or replace-normalizes to ordinary `/search`. This gives a
denied user one terminating path and no redirect loop.

## 4. Shared Route and Access Contract

The existing `appRoutes` array becomes the route manifest. Each entry receives
a stable ID and may carry:

- `adminOnly` or a permission key;
- optional navigation group, label, icon, and order;
- its existing path, element, and index status.

`App.tsx` renders guards from that entry. `layoutNavigation.ts` derives visible
navigation from the same entries instead of restating permissions. This is
smaller than adding a second route registry and removes the current drift by
construction.

The current permission store cannot distinguish its initial empty object from a
successfully loaded empty permission set and does not identify which user owns
the state. Add only the readiness identity needed by both consumers: the
loaded user ID plus the existing loading/error fields. Clear that identity and
permission data when the authenticated user changes or logs out.

The permission guard must distinguish four states for the current user:

1. permissions are idle or loading: start/reuse one request and render the
   existing loading treatment;
2. loading failed: render a retry/return access state without protected
   content;
3. permission is granted: render the target;
4. permission is denied after a successful load: replace-navigate to the
   closest safe viewer route.

Administrator role checks remain synchronous. Search itself is authenticated
but not permission-gated; `mode=ai` is component state checked with
`can_use_ai`. Home applies `can_use_ai_assistant` only to the recommendation
section. This preserves ordinary search and avoids turning a section-level
permission into page-level denial.

```text
route manifest -----> React Router guard -----> page
       |
       `------------> navigation projection --> visible entry

permission store ---> both consumers after load
```

Backend API authorization remains unchanged and authoritative.

## 5. Page Composition

### 5.1 Home

Move only the existing recommendation request and result presentation from the
standalone AI page into a lazy Home section. The section is independent from
featured, Continue Watching, and Recently Added state so its absence or failure
does not change Home's success path.

### 5.2 Libraries

Use the `view` query parameter as the source of truth for the segmented view
control. The existing library overview is mounted for `library`; the poster
composition is mounted for `poster`. Poster requests do not run while their
view is inactive.

Poster view keeps one `{ nextPage, loaded, total, error }` state per library.
A batch schedules at most three library-page requests in library-list order;
each request calls the existing media API with `page_size=50`. `Promise.all`
results append in scheduled order, not completion order. Later batches proceed
round-robin across libraries, prioritizing libraries whose first page has not
loaded. Media IDs are de-duplicated before the existing `groupSeries` function,
so the first-seen representative and card order remain stable. A library is
complete when loaded rows reach its reported total or a page is empty. Failed
page state does not advance and is retried by the same explicit Load More
control. The control disappears only when all libraries are complete.

This uses the backend's current 50-item default and stable per-library ordering
without a new aggregate endpoint. One activation or Load More action therefore
starts no more than three requests and accepts no more than 150 raw rows.

### 5.3 Me

Use the `tab` query parameter as the source of truth. Recompose the existing
Favourites, Playlists, and Watch History content rather than copying their API
calls. Each former page becomes either a reusable section or a thin wrapper
around the shared section until the compatibility redirect is installed.

Libraries, Me, and Search accept exactly one supported query value. Missing or
explicit default values render the default state. Unknown or duplicate values
replace-normalize to `/libraries`, `/me?tab=favourites`, or `/search`
respectively. Valid changes push history so Back/Forward restores the prior
view, tab, or mode; normalization replaces history and does not issue a request
for the invalid state.

### 5.4 Search and AI

Keep one Search page. Normal search remains the default. `mode=ai` activates
the existing intent-parsing path only after permission and provider status are
known. Removing the standalone AI page must not remove external TMDb, Douban,
or Bangumi search results.

### 5.5 DLNA

Reuse the existing cast flow. Expose its trigger from media detail/playback
context and the header. Media detail and Player always render an icon Cast
control with an accessible name when `can_cast`; those controls carry the
current media into the existing cast flow. The header action opens `/dlna`,
where a user can choose media when no current-media context exists. Keep the
full page as a permission-protected deep link. All three controls are absent
without `can_cast`.

### 5.6 Management

The management shell owns group navigation and an outlet. Existing Admin,
Storage, File Manager, Duplicates, Recycle, Scheduler, Tasks, Stats, STRM,
Notifications, Settings, and Assistant components move under canonical routes
with only the link and layout adjustments required by the shell. The current
Admin panels for libraries, users, and API configuration are reused in the
matching groups.

The admin Assistant's fixed two-column layout gains a narrow-screen stacking
mode when hosted by the shell; it remains a distinct administrator capability
from viewer AI search and recommendations.

## 6. Responsive Navigation

- Desktop viewer space keeps a restrained sidebar with the four primary
  destinations and a global search field in the header.
- Mobile viewer space uses a four-item bottom navigation. Search remains a
  header icon/action. Content receives stable bottom padding for the bar and
  safe-area inset.
- The current mobile drawer is retained for management navigation and
  low-frequency account actions, so no second management navigation system is
  invented.
- The user menu remains the viewer/management space switch for administrators.
- Existing route-change and Escape closing behavior is preserved. Overlay and
  menu z-indexes are checked together rather than adjusted independently.
- Viewer and management navigation are semantic `nav` landmarks with accessible
  names; active links use `aria-current="page"`, icon-only actions have labels
  and tooltips, and interactive targets are at least 44x44 CSS pixels.
- The mobile drawer traps focus while open, Escape closes it, and focus returns
  to its trigger. Motion respects `prefers-reduced-motion`; the bottom bar uses
  `env(safe-area-inset-bottom)` and equal-width tracks for every visible item.

## 7. State and Compatibility

- URL query parameters own Libraries view, Me tab, and Search mode so refresh,
  browser history, and shared links reproduce the same state.
- Existing Discover local-storage section selection and feed cache keys remain
  unchanged.
- Discover keeps the current reconciliation contract: filter saved keys against
  the enabled catalog, retain surviving keys, use enabled defaults when none
  survive, and use the empty state when the catalog is empty. A feed failure
  retains a cached row when available and does not fail other provider rows.
- Existing media and playlist identifiers remain unchanged.
- Player navigation continues to receive the canonical source location. The
  route migration must not replace `/library/:id`, `/media/:id`, `/play/:id`,
  or `/playlist/:id`, preventing return-chain churn.
- Links inside pages are updated to canonical routes; redirect routes are a
  compatibility boundary, not the application's normal internal navigation.

## 8. Rollout and Rollback

Implementation proceeds in reversible checkpoints:

1. add the shared access contract and canonical routes while old routes still
   redirect;
2. switch viewer navigation and compose Libraries, Me, Search, Home, and DLNA;
3. move administrator pages under the management shell and update links;
4. run the complete route, role, permission, and viewport matrix.

Each checkpoint must lint and build before the next. Rollback restores the
previous Web bundle; no data or backend migration is involved. Legacy
redirects remain until a separate, explicitly approved compatibility task
removes them.

## 9. Risks

- Evaluating permissions before the permission request finishes can redirect
  authorized users or flash protected content.
- Legacy and canonical routes can form loops if links and redirects are changed
  in the wrong order.
- Reusing page components without separating their data hooks can duplicate
  requests when tab or view content is hidden rather than unmounted.
- Poster pagination can duplicate or omit items across libraries unless stable
  page ownership and keys are preserved.
- Moving source pages can break the player's return location even when the
  player route itself is unchanged.
- A mobile bottom bar, drawer, user menu, and the admin Assistant can overlap if
  their spacing and overlay layers are verified in isolation.
- With no current browser-test framework, the authenticated route and viewport
  matrix requires disciplined browser smoke verification in addition to lint
  and build.
