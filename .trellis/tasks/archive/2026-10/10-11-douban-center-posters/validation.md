# Validation

- Isolated PostgreSQL 16, temporary schemas, no live database test writes.
- Passed: `go test -race ./internal/service ./internal/handler ./internal/repository ./internal/model ./internal/database -run 'Douban|ImageVariant|ArtworkStore' -count=1` with MEDIASTATION_TEST_POSTGRES_DSN.
- Passed: go vet for the same five packages.
- Passed: Web lint/build and check-image-url.mjs.
- Passed: check-douban-data-center.mjs with mocked API/image responses on an
  isolated Vite server. Real image decoding, failed-image placeholders, refreshed
  versions, URL filters, admin/session lifecycle and cancellation were checked
  at 390, 768, 1279, 1280 and 1440 widths in light/dark themes.
- Desktop/mobile light/dark dialog screenshots visually reviewed; temporary
  screenshots and the injected check script were removed afterward.
- The isolated Vite server, image fixture workers, browser sessions and PostgreSQL
  container were stopped; the user-owned backend remained running.
- Two independent read-only reviews found no confirmed defects; main review
  sampled CAS, auth chain and image cache-header propagation.
- Operational cleanup completed by the user after DBX SQL_BLOCKED prevented
  tool execution. Read-only verification found zero remaining targets and zero
  r-only Douban snapshots. All three metadata items and their 4/1/1 media
  references remain. Live background ingestion advanced global counts, so
  global-count equality is not used as preservation evidence.
- No application restart/deployment and no live Douban poster request performed.
  Code/schema become effective after rebuilding/restarting the application.
- Existing unbound-subject migration changes and the pre-existing untracked core
  file were preserved. Cleanup AC is met; code submission and task archival
  are authorized by the user.
- Subsequent authorized legacy cleanup: all 8646 valid old Douban snapshots
  were matched to nonempty shared subjects; 8645 payloads were identical and one
  shared payload was newer, with no lower quality or older fetch time. Current
  read/write and poster-repair paths were verified to use douban_subjects. The
  guarded SQL was supplied to the user; final read-only verification confirms
  zero remaining legacy Douban snapshots. Canonical local fields stay intact.
