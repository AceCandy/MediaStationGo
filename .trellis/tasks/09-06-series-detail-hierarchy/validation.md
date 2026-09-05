# Validation

- Passed: `npm --prefix web run lint`, `npm --prefix web run build`, `node web/scripts/check-series-detail.mjs`, `git diff --check`.
- Passed targeted Go command: `go test ./internal/service ./internal/handler ./internal/repository -run 'Test.*(Series|MediaCredits|MediaVersions|UpdateMetadata)' -count=1`. Database-dependent tests skipped without `MEDIASTATION_TEST_POSTGRES_DSN`; this is not PostgreSQL integration validation.
- Independent frontend review completed; final backend review checked canonical Series edits, visibility, favorites and history boundaries.
- Mock-browser checks: exact deep link beyond first poster page, specials, season/episode/version selection, refresh and player-return restoration, favorite state, Series edit fields, failed-file isolation and retry. Light/dark viewport widths 390, 639, 640, 767, 768, 1023, 1024 and 1440 had no horizontal overflow.
- Real PostgreSQL data, real media playback and external players were not tested. These remain integration risks.
- Temporary mock page and screenshots removed; dedicated browser session and Vite server stopped. No real media data changed.
- Series edit isolation is recorded in the shared metadata specification. Database and real-playback validation remain follow-up checks.
