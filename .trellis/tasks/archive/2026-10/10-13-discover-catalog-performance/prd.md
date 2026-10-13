# Discover catalog performance and consistency

## Requirements
- Reduce Douban persisted list, genre and literal title-search latency while preserving matches, totals, identity, source priority and ordering.
- TMDb movie/TV/person and Douban categories append on scrolling; preserve rows on continuation failure and retry that page.
- Filters and refresh reset continuation; dialogs preserve rows and pause automatic loading.
- Compact category/ranking switch matches HongGuo. Keep source routes and administrator/viewer boundaries.

## Acceptance
- Real PostgreSQL tests exercise generated projection, existing data, repeated maintenance, updates, filters and plans.
- Browser tests cover both centers, reset/append/retry/cancellation, responsive themes and compact controls.
- Type check, lint, build and independent review pass. No production mutations or service restarts.
