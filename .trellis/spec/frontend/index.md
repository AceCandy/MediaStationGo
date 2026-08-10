# Frontend Development Guidelines

> Executable routing, access, navigation, and bounded-loading contracts for the Web package.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Routing and Loading Contracts](./routing-and-loading-contracts.md) | Route ownership, permission lifecycle, canonical query state, and bounded poster loading | Active |

---

## Pre-Development Checklist

- [ ] Read the route manifest before adding a route, permission gate, or navigation entry.
- [ ] Identify whether UI state belongs in the URL and define its canonical default.
- [ ] Trace permission readiness and failure behavior for the current authenticated user.
- [ ] Keep cross-library loading bounded and user initiated when the result set is open ended.

## Quality Check

- [ ] Visible navigation and pasted URL access use the same manifest metadata.
- [ ] Account changes cannot reuse another user's permissions or an older in-flight response.
- [ ] Missing, duplicate, and invalid query values normalize with replace navigation.
- [ ] Ordinary search remains available when AI capability is denied or unavailable.
- [ ] Poster loading uses `page_size=50`, schedules at most three requests per explicit batch, and retries failed pages without advancing them.
- [ ] Required mobile, tablet, desktop, and breakpoint-boundary viewports have no horizontal overflow or occluded controls.
- [ ] Run `npm run lint`, `npm run build`, and `git diff --check` after Web changes.

---

**Language**: Project specification documents are written in English.
