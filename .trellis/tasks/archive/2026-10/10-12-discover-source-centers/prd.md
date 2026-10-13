# Source-based Discover

## Goal

Unify source catalog browsing and rankings under Discover, removing the separate Data Center navigation entry.

## Background and Decisions

- The user approved the source-first structure and ordinary-viewer read access on 2026-10-12 after reviewing the proposed structure and permission split.
- Before this task, DiscoverHubPage combined TMDb/Douban/Bangumi in `system=catalog` alongside HongGuo and HuangGuo AI.
- Before this task, DataCenterPage switched administrator-only Douban and TMDb pages; persistent source tables already exist.

## Requirements

- R1: Discover has separate TMDb, Douban, HongGuo, HuangGuo AI and Bangumi branches. Mount only the selected branch.
- R2: TMDb and Douban each expose Data / Rankings switches; move existing source rankings into their owning branch and retain Bangumi daily airing.
- R3: Data browsing uses poster grids and existing detail dialogs. TMDb supports movie/tv/person roots and nested seasons/episodes; Douban supports movie/series.
- R4: Users with Discover permission can browse persisted source data/details/images and request bounded missing-source supplementation. Force refresh, local metadata writes, management status and raw diagnostics remain administrator-only.
- R5: Preserve source data, local metadata, existing source refresh schedules, image storage/authentication, ranking order/cache and HongGuo/HuangGuo functionality.
- R6: Navigation/filter/detail state survives canonical deep links, reload and back/forward; changing source resets incompatible filters and aborts obsolete reads. Remove the separate Data Center leaf and wrapper, and update internal links.

## Acceptance Criteria

- [x] Each source opens only its own list or rankings; switching source does not request other providers.
- [x] Ordinary viewers can read TMDb/Douban data and images without administrator endpoints or raw/internal diagnostic leakage; unauthorized reads/writes are rejected server-side.
- [x] Administrator import/refresh actions remain usable; viewer UI never shows them.
- [x] Details, TMDb seasons/episodes/person works and Douban cast images retain existing functionality.
- [x] Source-scoped rankings, search, filters, pagination, invalid/duplicate URL normalization, cancellation and detail focus return are verified.
- [x] Dark/light layouts at mobile/tablet/desktop widths remain usable, without horizontal overflow or console errors.
- [x] Focused backend tests, Web browser checks, lint/build and diff checks pass; independent review has no unresolved material findings.

## Out of Scope

- New rankings, upstream crawlers or changes to existing periodic refresh policies.
- A Bangumi data center, Douban person table, media metadata changes or unrelated UI refactoring.
- Git commits, remote pushes or restarting the user's service.

## Approved follow-up (2026-10-13)

- Task Center separates common, media, TMDb, Douban, HongGuo and HuangGuo AI systems. Source refresh/supplement tasks belong to their source; local work maintenance belongs to media, including TMDb episode recheck and translated local credits. Existing execution history follows the new classification without a runtime legacy fallback.
- Ranking details read the owning source center. Local metadata and translations remain untouched; local association supplies only library status/linkage.
- TMDb/Douban ranking summaries are persisted without extra source requests or overwriting complete source details. Opening a missing/incomplete source requests durable, deduplicated supplementation, visible as separate source tasks with progress and logs. Automatic Douban supplementation uses only Rexxar base and celebrities APIs, saving all returned credits without webpage or recursive person scraping. The upstream total may exceed the actual returned personnel list.
- Public TMDb details expose a safe pending flag rather than job diagnostics.
- Fix failed-page retry/cursor advancement, same-account session request isolation, missing-image cards and shared dialog focus handling.

### Follow-up acceptance

- [x] Six task systems classify definitions/new executions/history consistently; local maintenance remains independent of source supplementation.
- [x] Both ranking detail paths preserve source truth and automatic supplementation is bounded, durable, deduplicated and observable after restart.
- [x] Public projections omit job internals; viewer supplement requests cannot force refresh or modify local works.
- [x] Browser regressions cover failed page retry, same-account relogin, image placeholders and dialog focus containment/return.
- [x] Focused isolated-database tests, lint/build and independent review pass.

## Approved visual/category follow-up (2026-10-13)

The user approved the unified category/ranking layout and richer shared TMDb
detail proposal after reviewing both. Implementation continues in this task.

- All sources use the same Category / Rankings navigation and visual rhythm;
  Bangumi has Rankings only. TMDb category roots are movie/tv/person; Douban roots
  are movie/series. Keep HongGuo/HuangGuo source category meanings and actions.
- TMDb/Douban movie and series categories filter by persisted multi-value genre;
  TMDb people filter by primary department. Options and filtering are database
  reads, applied before counting/paging. Summaries lacking genres remain in All.
- Category results use consistent poster grids; rankings use shared numbered
  rows in source order. The number is a display position, not an invented source
  award. Preserve source-specific feed order, search, retry and bounded loading.
- Classification and rankings open one TMDb source detail implementation: soft
  backdrop, poster/facts/score header, synopsis, circular clickable credits,
  season cards and landscape episodes, person biography and work poster grid.
  Keep one modal with back/parent navigation, theme and source image delivery.
- Admin actions move into a compact menu; raw diagnostics stay folded and
  administrator-only. Missing opened details use bounded source ensure/polling,
  never local metadata refresh or recursive all-person/all-episode hydration.

### Visual/category acceptance

- [x] Multi-genre and department filters/options are source-scoped, parameterized,
  count/page correctly and never contact providers.
- [x] Five sources share Category/Rankings structure, card/ranking styles and
  consistent loading/error controls while retaining source-specific features.
- [x] TMDb category/ranking/person/season/episode navigation reuses one modal,
  retains library state, uses source facts and keeps administrative gates.
- [x] Dark/light mobile/tablet/desktop views, keyboard focus, URL/history,
  session cancellation, missing images and failed-page retry are verified.
- [x] Focused PostgreSQL tests, Web lint/build and independent review pass.


## Alignment audit closure acceptance

- [x] Failed TMDb related pages retain successful content and committed page;
  retry targets the failed page. First detail failure retains the entry summary.
- [x] Search/paging controls, ranking skeletons and continuation/end states align
  without changing source search semantics or independent continuation cursors.
- [x] Douban category/ranking management menus and administrator gates match.
- [x] Focused failure, cancellation, search and responsive browser checks pass.
