# Refine Web Information Architecture - Technical Design

## 1. Design Principle

The hierarchy follows one invariant: a sidebar node is either a group button
or a route link. Group metadata is not inferred from the first route in that
group. This removes the current parent-as-page ambiguity without replacing the
route manifest or sidebar component system.

## 2. Navigation Model

Keep route navigation metadata for actual destinations. Define the five
management groups separately with ID, label, icon, order, and active path.
Project visible routes into each group's children.

```text
management section
  group button (no `to`, toggles expansion)
    route link
    route link
```

The group receives visual active state when any child route matches, but never
receives `aria-current`. Existing permission and administrator filtering is
applied to children before empty groups are removed.

The first existing route in each group remains a real second-level page:

| Group | Default leaf | Route |
|---|---|---|
| Media Intake | Media Library Management | `/admin/media` |
| Storage and Cleanup | Storage Overview | `/admin/storage` |
| Tasks and Runtime | Task Center | `/admin/tasks` |
| Users and Integrations | User Management | `/admin/integrations/users` |
| System Settings | General | `/admin/settings/general` |

`/admin` replace-redirects to `/admin/media` and `/admin/integrations`
replace-redirects to `/admin/integrations/users`. `/admin/settings` and the
legacy `/settings` redirect to `/admin/settings/general`. Existing specialist
and legacy paths remain unchanged.

## 3. Page Boundaries

### 3.1 Media and Storage

Media Library Management reuses `AdminLibraryPanel` without its local shortcut
cards. Storage Overview keeps its summary and breakdown tables without local
cross-domain shortcuts. Duplicates and Recycle Bin remain separate because
they have destructive, independently reviewable workflows.

### 3.2 Task Center and Runtime Monitor

Compose the existing background-task and scheduler views in one Task Center.
Each section keeps its existing hook/state and polling interval; composition
must not couple the 3-second task snapshot to the 5-second scheduler status.
Existing `/admin/tasks/scheduler` becomes a compatibility redirect to the
scheduled section on `/admin/tasks` only if a section anchor/query is needed;
otherwise it redirects to the composed page.

Runtime Monitor removes only duplicate/low-value presentation. Storage and
media totals migrate to Storage Overview; user count is shown with User
Management. Live CPU, memory, disk, Go runtime, and goroutine values remain.

### 3.3 Settings

One `SettingsPage` implementation accepts a route section. Explicit child
routes mount it with `general`, `playback`, `recognition`, `access`, or
`update`. The page no longer owns a local horizontal selector. Existing group
definitions are reorganized, not replaced with a new settings framework.

General keeps metadata/UI settings. Playback contains public URL, mapping,
redirect, and FFprobe fields, with low-frequency values inside native
`details`. Access keeps global Adult policy. Update keeps `SystemUpdatePanel`
and places deployment overrides in `details`.

### 3.4 Integrations

User, API, Notification, and AI pages stay independent leaves. Remove the
integration shortcut page. API rows expose a single configuration summary and
edit/clear actions; the placeholder Eye action is removed. Notification Bot
polling remains available from an explicit runtime-control disclosure.

### 3.5 Viewer Cleanup

Libraries uses its per-library shelf as both preview and navigation. The shelf
heading must link to the full library before the separate entry-card grid is
removed. Home and sidebar retain Discover entry; the desktop header duplicate
is removed. Search, DLNA, and profile actions remain contextual.

## 4. Compatibility and Access

- Route guards continue to derive administrator and permission requirements
  from `appRoutes`.
- Group definitions contain no independent access policy beyond projecting
  guarded routes.
- Parent active matching uses segment boundaries so `/admin/mediafoo` cannot
  activate `/admin/media`.
- Existing direct specialist URLs and redirects use history replacement.
- Settings route changes preserve `/admin/settings` and `/settings` through
  redirects.
- Task scheduler and integration overview paths remain valid redirects.

## 5. Responsive and Accessibility Behavior

Expanded desktop sidebar shows management group buttons and indented leaves.
Collapsed desktop sidebar and mobile drawer reuse existing group controls;
there is no new navigation surface. Group buttons expose `aria-expanded` and
leaf links rely on React Router for `aria-current`. Route changes open the
active section and close unrelated sections as today.

## 6. Rollout and Rollback

1. Fix navigation semantics and redirects while specialist pages remain
   unchanged.
2. Remove duplicate shells and compose Task/Stats content.
3. Route Settings sections and clean fields/controls.
4. Remove viewer duplication and verify the full route/viewport matrix.

Each checkpoint is reversible without data migration. No persisted value is
deleted; advanced-field changes are presentation-only.

## 7. Risks

- Converting group roots can strand direct links unless redirects are installed
  before shortcut pages are removed.
- Composing Tasks and Scheduler can accidentally couple polling or obscure
  update-task status.
- Splitting Settings by route can lose unsaved state when navigating sections;
  section changes must use normal route transitions and current dirty-state
  behavior must remain explicit.
- Removing library entry cards can reduce discoverability unless shelf headers
  are clear links.
- Fields that appear technical still control playback/update behavior and must
  remain editable.
