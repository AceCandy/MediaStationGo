# Shared UI consistency

## Boundary and ownership

Reuse the existing CSS primitive system in `web/src/index.css`. Page components retain state, links, buttons, callbacks and data loading. No generic table or navigation state component is required.

## Contracts

- `.tab-list` owns the rounded, wrapping navigation surface. `.tab-item` owns 44px minimum targets, typography, hover and focus. `[aria-current="page"]` and `[aria-pressed="true"]` select the same active style. Native links/buttons retain their semantics. All seven equivalent category/switcher surfaces use these classes; filter chips, date-range filters and episode selectors remain separate.
- All HTML data tables reuse `.data-table`. CSS variables own theme colors, and low-specificity child selectors permit explicit alignment, semantic status colors and compact preview spacing. Content truncation belongs to individual cells, never the shared table rule.
- Dark-theme selectors include input/button compatibility aliases used by current pages. No page-by-page substitution is necessary; unused surface/divider aliases are outside the change boundary.

## Compatibility and rollback

Keep all URL normalization, unsaved settings, permissions, lazy mounts, native button types and callbacks. Tables retain column widths, responsive wrappers and sticky headers. Revert only this task's CSS/JSX changes to roll back; no persisted data or API changes occur.

## Validation

Use the existing browser CLI and mock APIs. Compare computed navigation styles across pages, verify focus and touch size, test alias equivalence and table colors/content visibility, then run existing settings/discovery/download regressions. Inspect light/dark desktop/mobile screenshots. Run lint/build and independent read-only review.
