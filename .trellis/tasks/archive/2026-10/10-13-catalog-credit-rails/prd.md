# Catalog credit rails

## Goal
Split source-detail credits into cast and crew rails, loading 20 records near the right edge, without pager controls. Flatten the TMDb header and localize region/language/job labels.

## Scope
TMDb subject credits use category-filtered server pagination. Douban and HongGuo use their existing arrays in 20-item display batches. Keep source order within cast; show crew separately. No fabricated cast for HuangGuo AI/Bangumi. Library details and person filmography keep existing behavior. Names/characters retain source text. No production DB writes, source crawls, migrations or automatic git commit.

## Acceptance
- [x] Independent cast/crew totals and batches of 20; append without clearing visible records or scroll position.
- [x] Failed continuation retries the same page; no duplicate requests or stale writes after close/navigation.
- [x] SQL category filtering/count occurs before pagination; cast order preserved.
- [x] TMDb header has no inner card; Chinese regions/languages/jobs use source codes or explicit labels.
- [x] Isolated PostgreSQL tests, browser regressions, responsive screenshots, lint/build and independent review pass.
