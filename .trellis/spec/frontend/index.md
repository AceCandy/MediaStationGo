# Frontend Development Guidelines

> Executable routing, access, navigation, and bounded-loading contracts for the Web package.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Routing and Loading Contracts](./routing-and-loading-contracts.md) | Route ownership, permission lifecycle, canonical query state, and bounded poster loading | Active |
| [Discover Feed Loading Contract](./discover-feed-loading.md) | Discover batching, cancellation, cache refresh, and stable poster URLs | Active |
| [Cinema Design System](./design-system.md) | Brand palette, theme tokens, motion language, and component primitives | Active |
| [Emby API Catalog Synchronization](../backend/emby-api-catalog-sync.md) | Cross-layer synchronization contract for the static administrator catalog | Active |

---

## Pre-Development Checklist

- [ ] Read the route manifest before adding a route, permission gate, or navigation entry.
- [ ] Identify whether UI state belongs in the URL and define its canonical default.
- [ ] Trace permission readiness and failure behavior for the current authenticated user.
- [ ] Keep cross-library loading bounded and user initiated when the result set is open ended.
- [ ] When changing the discover page or feed, read the [discover feed loading contract](./discover-feed-loading.md).
- [ ] When touching the Emby API catalog, read the backend route and handler sources required by the [catalog synchronization contract](../backend/emby-api-catalog-sync.md).

## Quality Check

- [ ] Visible navigation and pasted URL access use the same manifest metadata.
- [ ] Account changes cannot reuse another user's permissions or an older in-flight response.
- [ ] Missing, duplicate, and invalid query values normalize with replace navigation.
- [ ] Ordinary search remains available when AI capability is denied or unavailable.
- [ ] Poster loading uses `page_size=50`, schedules at most three requests per explicit batch, and retries failed pages without advancing them.
- [ ] Discover loading keeps poster URLs stable, aborts stale batches, and limits row pagination to the changed section.
- [ ] Required mobile, tablet, desktop, and breakpoint-boundary viewports have no horizontal overflow or occluded controls.
- [ ] Run `npm run lint`, `npm run build`, and `git diff --check` after Web changes.
- [ ] Player-visible Emby route, auth, parameter, response, or support-level changes are reflected in `web/src/pages/embyApiCatalog.ts` in the same task.

---

**Language**: Project specification documents are written in English.
