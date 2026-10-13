# Emby extension discovery

## Goal
Expose MSG source-center discovery to native players through `/emby-ext/v1`, approved by the user on 2026-10-13. MSG first; LinPlayer integration is a later task.

## Requirements
- Reuse TMDb and Douban centers and source query caches; do not duplicate source databases or provider clients.
- Authenticated capability discovery, ranking directory/items, full Bangumi weekly calendar, and exact external-ID library matches.
- Keep source ranks distinct from saved catalog order. No fabricated totals, times, episode numbers or playable URLs.
- Persist the full Bangumi calendar; stale reads retain the last successful result and refresh asynchronously with coalescing and lifecycle cancellation.
- Match only visible files under the current user/profile/library restrictions. Return Emby IDs, not file paths or stream credentials.
- Player API page has Emby and Emby extension groups with shared search/category/details/copy.

## Acceptance
Actual HTTP tests cover auth/revocation/permissions, invalid input, source failure, exact match and hidden/fileless items. PostgreSQL tests cover calendar restart retention, complete weekdays, concurrency and failed refresh. Browser tests cover both protocol groups and existing page regressions.

## Out of scope
LinPlayer changes, personal Trakt/Bangumi account synchronization, new third-party ranking providers beyond MSG-supported sources, deployment, commit and packages.

## Evidence
`internal/service/tmdb.go:146` uses TMDbDataCenter; `internal/service/douban_cache.go:201` persists query cache; `internal/service/bangumi_calendar.go:12` loses weekday and truncates 24; `internal/repository/discover_repository.go:47` demonstrates visible-file identity matching; `web/src/pages/AdminEmbyAPIsPage.tsx:29` owns API catalog UI.
