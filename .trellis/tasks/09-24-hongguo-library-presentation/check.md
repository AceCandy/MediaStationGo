# Verification — 2026-09-24

## Result

Implementation and independent read-only review passed. No schema migration or
production data operation. The reduced HongGuo library components were removed;
their source remains recoverable from Git. Common components own presentation.

## Executed checks

- Isolated PostgreSQL 16, with `MEDIASTATION_TEST_POSTGRES_DSN` set (not skipped):
  `go test ./internal/service ./internal/repository ./internal/handler -run 'Test(HongGuoLibrarySeriesPresentation|LibrarySeries|MediaSeason|SeriesSeason|SeriesDisplay|HongGuoHTTPAccess|Continuation|NFOSeriesHierarchyAndStateIsolation|FavoritesOnlyMoviesAndSeries|ListFavourites)' -count=1`.
- The new HongGuo service test passed again after adding scoped cross-season
  continuation and missing-first-season fallback assertions.
- `npm run lint`, `npm run build`, `git diff --check`.
- `node scripts/check-series-loading.mjs` and `check-series-presentation.mjs`.
- `node scripts/check-hongguo-discover.mjs`: common library cards, independent
  season data, version switching, source episode deduplication, series favorites,
  preserved first-season header, old series/movie links and browser errors.
  Library layouts: dark/light at 390, 639, 640, 767, 768, 1023, 1024, 1440 pixels.
  Inspected desktop dark and mobile light screenshots. Discovery's existing
  permission, navigation, modal and responsive checks also passed.

## Root cause and review

The old library branch used a separate reduced card/detail tree; sparse episode
fields did not require a different UI. Read-only catalog projection is the fix,
not copying source data into canonical metadata. Captured in the HongGuo spec.

Browser regression exposed a movie-only navigation race introduced by integration:
the parent navigated to file details while a mounted series child wrote the
season/episode query back. Do not mount series details during movie redirection;
the browser test asserts the final file route and absence of series-detail reads.

Two initial independent backend/frontend reviews and a final full-diff review
reported no confirmed defects. Final review's file-ID concern is covered by the
service assertion that a movie card retains its real file ID and the successful
browser legacy-movie redirect.

## Limits and cleanup

Browser APIs were fixture responses; real database/permissions were checked
separately in Go. No deployed UI, actual user library, real poster delivery or
device playback was tested. No full-repository test suite was run.
Temporary PostgreSQL container, preview server and browser sessions were closed;
temporary screenshots were removed. No test credential/export was added to Git.

## Proposed commit

One commit: `fix: unify hongguo library presentation`.

- `internal/repository/{hongguo_series.go,hongguo_groups.go,library_metadata_repository.go,media_search_repository.go,history_repository.go}`
- `internal/service/{media_series.go,media_listing.go,media_credits.go,hongguo_series_test.go}`
- `internal/handler/{media_credits.go,media_series_detail.go,series.go,hongguo_test.go}`
- `web/src/pages/{HongGuoLibraryDetail.tsx,HongGuoPage.tsx,LibraryMediaSections.tsx,LibraryPage.tsx,LibrarySeriesDetailHeader.tsx,LibrarySeriesDetailSection.tsx,LibrarySeriesEpisodeDetail.tsx,LibrarySeriesEpisodes.tsx,MediaDetailMetadata.tsx,MediaDetailPageSections.tsx,useLibraryData.ts,useLibrarySeriesSelection.ts,useMediaDetailPageState.ts}`
- `web/scripts/{check-hongguo-discover.mjs,check-series-loading.mjs,check-series-presentation.mjs}`
- `.trellis/spec/backend/hongguo-catalog.md` and this task's artifacts.

All dirty source paths belong to this task. The user confirmed this commit plan
with “ok”; no remote push or deployment is included.
