# Reorganize Web UI Around Emby Core - Implementation Plan

## Success Criteria

The task is complete when retained viewer capabilities read as Home,
Libraries, Discover, Me, and global Search; retained admin capabilities live
under a coherent `/admin/*` space; legacy URLs remain compatible; navigation
visibility and direct route access agree; and the complete route and viewport
matrix passes without backend contract changes.

## Ordered Checklist

### 1. Record and Protect the Current Contract

- [ ] Reconfirm the route, internal link, permission, Discover section, and
      responsive-layout inventories against the current worktree before edits.
- [ ] Capture baseline screenshots for viewer desktop/mobile and the current
      admin entry using sanitized local data.
- [ ] Record current successful navigation from Home, Libraries, Discover,
      Favourites, Playlists, History, Search, media detail, and playback.
- [ ] Confirm the Web package's actual lint/build commands and avoid adding a
      test dependency unless the existing toolchain already provides one.

Verify: the baseline matches `research/current-ui-inventory.md`; no product
file has changed yet.

### 2. Establish One Route and Access Contract

- [ ] Give existing application route entries stable IDs and add only the
      navigation/access metadata required by the approved architecture.
- [ ] Derive viewer and management navigation access from the route manifest
      instead of duplicating `adminOnly` and permission keys.
- [ ] Add a route-level permission guard that waits for permission loading,
      permits administrator/super-user behavior consistently with the current
      store, and replace-navigates denied users to a safe route.
- [ ] Bind loaded permissions to the current user ID. Clear permission data and
      readiness when the authenticated user changes or logs out; reuse one
      in-flight request, and show retry/return UI on load failure rather than
      treating the error as a denial.
- [ ] Keep Search authenticated but unrestricted; apply `can_use_ai` only to
      AI mode. Apply `can_use_ai_assistant` to Home recommendations and
      `can_cast` to DLNA.
- [ ] Add canonical `/me` and `/admin/*` routes plus the compatibility redirects
      in design section 3.3. Guard legacy admin routes before redirecting.
- [ ] Map the exact former Admin queries `library`, `users`, and `api`; normalize
      unknown Admin tabs to the overview. Make `/ai` terminate at AI Search when
      allowed or ordinary Search when denied, with no redirect loop.
- [ ] Render documented defaults when Libraries view, Me tab, or Search mode is
      missing. Replace-normalize duplicate or invalid values; valid user changes
      push history.
- [ ] Keep `/library/:id`, `/playlist/:id`, `/media/:id`, `/play/:id`,
      `/profile`, `/play-profiles`, and `/dlna` paths stable.

Verify: role/permission combinations produce the same result from a visible
entry, a pasted URL, and a browser refresh; user switches do not reuse stale
permissions, load failures are retryable, and all redirects terminate once.

Rollback point: revert the route-manifest/guard commit as one unit.

### 3. Reorganize Viewer Navigation and Layout

- [ ] Reduce desktop viewer navigation to Home, Libraries, Discover, and Me;
      keep Discover permission-aware through the shared route manifest.
- [ ] Keep global Search in the header and remove its `can_use_ai` navigation
      restriction.
- [ ] Add the four-destination mobile bottom navigation using existing icons,
      tokens, and route state; add stable safe-area/bottom content spacing.
- [ ] Retain the current drawer for management and low-frequency account
      actions rather than creating a second drawer implementation.
- [ ] Keep Profile, playback profiles, logout, and the administrator space
      switch in the user menu.
- [ ] Update active-route grouping so detail, playlist, Me-tab, and canonical
      management paths highlight the correct owner.
- [ ] Add semantic navigation labels, active `aria-current`, visible focus,
      44x44 minimum targets, reduced-motion handling, and mobile drawer focus
      trap/return without replacing existing components.

Verify: 390x844, 768x1024, and 1440x900 layouts have stable navigation sizes,
no content occlusion, working Escape/overlay behavior, and no horizontal
overflow.

### 4. Compose Libraries, Me, Search, Home, and DLNA

- [ ] Add a segmented `library|poster` view to `/libraries`, driven by the URL
      query and defaulting to `library`.
- [ ] Mount poster fetching only in poster view. Track page/total/error per
      library, schedule at most three `page_size=50` requests per explicit
      batch in deterministic round-robin order, append in scheduled order,
      de-duplicate media IDs, preserve series grouping, retry without advancing
      failed pages, and stop only when every library is exhausted.
- [ ] Add `/me` with URL-driven Favourites, Playlists, and Watch History tabs by
      reusing the current content/data logic.
- [ ] Preserve playlist create/delete/detail, history remove/cleanup/resume, and
      favourite playback interactions.
- [ ] Make `/search?mode=ai` the only viewer AI-search surface; preserve local
      and external search when AI is unavailable or denied.
- [ ] Move the existing AI recommendation behavior to an independent Home
      section and remove the duplicate standalone page from canonical UI.
- [ ] Add the `can_cast` header link plus current-media Cast controls to media
      detail and Player, all with accessible names; hide them without permission
      and retain the full permission-protected `/dlna` selection flow.
- [ ] Confirm Discover retains the exact 12-section catalog, selection storage,
      provider enablement, caching, and disabled behavior without code changes
      outside links/layout unless a regression is found.
- [ ] Verify all-on, each-provider-off, all-off, stale saved selection, cached
      row, and provider error Discover states. Preserve surviving selections,
      fall back to enabled defaults only when none survive, and isolate row
      failures.

Verify: query-state refresh/back behavior, network requests, empty/error states,
and all viewer acceptance criteria pass. Inspect the browser network panel to
prove poster requests start only after selecting poster view and remain
bounded.

Rollback point: viewer composition changes can revert while the canonical
route/access foundation remains.

### 5. Build the `/admin/*` Management Space

- [ ] Add the shared management shell and five approved groups without
      duplicating the viewer primary navigation.
- [ ] Reuse current library/user/API panels and specialist pages at the route
      locations in design section 3.2.
- [ ] Update every internal admin shortcut and page link to its canonical
      `/admin/*` destination; leave old root paths as redirects only.
- [ ] Replace former `tab=library|users|api` bookmarks with the exact canonical
      routes and normalize unknown tab values to `/admin`.
- [ ] Ensure every retained management capability is reachable from `/admin`
      in no more than two interactions.
- [ ] Make management navigation usable in the retained mobile drawer and make
      the admin Assistant stack on narrow screens instead of using an
      unconditional fixed two-column grid.
- [ ] Keep viewer AI search/recommendations distinct from administrator AI
      sessions in labels, access, and routes.

Verify: non-admin, admin, canonical URL, legacy URL, desktop, tablet, and phone
management scenarios all pass; no legacy root link remains in normal internal
navigation.

Rollback point: revert the management shell/link checkpoint; legacy redirects
still preserve previous entry points.

### 6. Full Verification and Independent Review

- [ ] Run `npm run lint` from `web/`.
- [ ] Run `npm run build` from `web/`.
- [ ] Run focused authenticated browser smoke checks for the acceptance matrix,
      including refresh and direct navigation for each relevant permission.
- [ ] Verify legacy redirects and unchanged deep links with browser history and
      playback return navigation.
- [ ] Verify Discover section count/labels with all providers enabled and the
      exact 4/9/11 remaining counts when TMDb/Douban/Bangumi is disabled, the
      all-disabled empty state, saved-selection repair, cached rows, and row
      error isolation.
- [ ] Verify no request for the former 2,000-item poster batch occurs on default
      Libraries load.
- [ ] Inspect changed CSS classes at all three required viewports for text,
      controls, overlays, bottom navigation, and admin Assistant overlap.
- [ ] Run keyboard-only checks for navigation names/current state, visible
      focus, logical order, drawer/menu trap and focus return, Escape, 44x44
      targets, reduced motion, and safe-area bottom spacing.
- [ ] Exercise missing, duplicate, invalid, and valid `view`, `tab`, and `mode`
      values through refresh and Back/Forward; confirm invalid states issue no
      content request and normalization does not create an extra history entry.
- [ ] Run a targeted `rg` inventory for legacy internal links, duplicated route
      access declarations, and retired top-level navigation labels; review
      legitimate redirect definitions rather than requiring zero matches.
- [ ] Run `git diff --check`.
- [ ] Dispatch an independent Trellis check against the PRD, design, current
      worktree, and relevant thinking guides; fix and repeat until clean.
- [ ] Shut down every development or backend service started for browser
      verification and remove any screenshots, captures, or local exports that
      contain private data.

Suggested commands, adjusted only if package scripts change before execution:

```bash
cd web && npm run lint
cd web && npm run build
rg -n "to=\"/(files|storage|duplicates|scheduler|tasks|recycle|strm|notify-channels|settings|assistant|stats)" web/src
rg -n "can_use_ai|can_use_ai_assistant|can_view_discover|can_cast|adminOnly" web/src
git diff --check
```

## Review Gates

- Do not hide a page in navigation without applying its shared access metadata
  to direct route access.
- Do not gate ordinary Search behind any AI permission.
- Do not request poster data before poster view is active or restore an
  unbounded cross-library batch under another component name.
- Do not replace stable media, playlist, or player deep links.
- Do not remove a retained Discover section while moving its entry.
- Do not mix viewer AI permissions with administrator AI-session access.
- Do not introduce a UI, router, state, or test dependency for behavior already
  covered by the current stack and focused browser verification.
- Do not log, commit, or retain credentials, tokens, signed URLs, user media
  names, or unsanitized screenshots.

## Completion Record

Completed on 2026-08-10 against the approved PRD and technical design.

Implemented:

- One route manifest now owns route access and navigation metadata.
- Viewer navigation is Home, Libraries, Discover, and Me; Search, DLNA,
  profile, and management remain contextual actions.
- Libraries owns the lazy poster view with 50-item pages, at most three
  requests per explicit batch, ordered merging, ID de-duplication, and retry.
- Search AI mode, Home recommendations, current-media casting, compatibility
  redirects, and the shared `/admin/*` shell follow their documented access
  boundaries.
- Permission readiness and cached data are bound to the authenticated user ID.
- Mobile bottom navigation, drawer/menu focus management, 44px controls,
  responsive admin navigation, and breakpoint-specific header behavior were
  verified and adjusted from rendered screenshots.

Verified:

- `npm run lint`
- `npm run build`
- `git diff --check`
- `python3 ./.trellis/scripts/task.py validate 08-10-reorganize-web-ui`
- Exact scans for legacy internal links and temporary review values
- Canonical redirect and invalid-query normalization matrix
- Poster first batch of three `page_size=50` requests and explicit fourth-library load
- 390x844, 768x1024, 1024x768, and 1440x900 document/header widths
- Drawer and user-dialog initial focus, Tab loop, Escape, and focus return
- Home, Libraries, Discover, Me, Admin, and Search without runtime console errors
- Independent route/permission, viewer-flow, and mobile/accessibility reviews

Not verified with a real authenticated backend session:

- Mutating playlist/history/favourite/admin operations and live media playback
- Provider-backed Discover response, cache, and failure states

The browser checks used isolated mocked authentication/data only for UI and
route behavior. The browser, temporary `6210` server, and all screenshots were
removed after verification; pre-existing `6200/6201` services were untouched.
