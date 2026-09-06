# Validation

## NFO specials and repeated profile imports

- Before the fix, the explicit-zero merge regression returned -1/25, the
  PostgreSQL retry with actual show/episode NFO fixtures reproduced the exact
  Season validation error, and two works downloaded the shared avatar twice.
- After the fix, focused tests passed without PostgreSQL skips: NFO merge/read,
  scan recovery, library/single retry with stored -1 and already-repaired 0,
  profile persistence/reuse, missing/corrupt local file repair, source change,
  failed import retries, explicit refresh and complete catalog Series hydration.
- `go build ./...` and `git diff --check` passed. Independently reviewed the
  changed NFO conversion, all profile import callers and explicit-refresh bypass.
- No production rows, sidecars, task executions or running services were changed.
  Browser/production retry and real long-Series wall-clock improvement remain
  unverified. Successful profile mappings are process-local and expire after
  24 hours or bounded-cache eviction, so a cold import can still download once.
- Root-cause category: cross-layer contract and test-coverage gap. Repairing
  stored coordinates did not prevent later NFO merging from reintroducing -1;
  keep the conflicting sidecar fixtures in retry regression coverage. Content
  deduplication after download is not request deduplication; assert HTTP counts.

## Canonical Series grouping priority

- Reproduced the duplicate-card bug with 167 two-version Episodes plus 123
  mixed-year Episodes: before the guard, the test returned 167 cards instead
  of two. Canonical-Series priority fixes the same regression.
- Checked logical-episode counts, all-version selection, distinct Series with
  overlapping hints, subset-stable keys, legacy unbound grouping and movies.
- Root cause: the outer resolver's heuristics overrode the canonical priority
  already implemented by `mediaSeriesRawKey`. The prior presentation regression
  covered only one episode and therefore could not expose this bypass.
- No production mutation or restart. Old split-card deep links may need users
  to return to the library and reopen the newly merged card.

## Series card presentation correction

- Added isolated PostgreSQL regression for canonical Series card title, year,
  rating, overview, poster, missing-poster filters and unchanged Episode target.
  Includes the inverse case: an Episode poster cannot fill a missing Series poster.
- Kept library visibility checks and Series detail ownership regressions; updated
  the recent-series fixture with its missing probe table and asserted Series title.
- Backend build and both existing Web selection/presentation scripts passed.
- Independently reviewed the projection merge: file identity and technical facts
  stay intact, including codec; Series-owned Douban rating is not overwritten by
  the representative file. The generic Episode projection remains unchanged.
- No production data changes, restart, rescrape or commit. Live browser rendering
  after deployment and full-suite validation were not performed this round.

## Long-series ingestion and scheduling

- Fourteen focused PostgreSQL tests executed without skips: complete catalog
  trees/recovery, three concurrent workers, unresolved binding, inventory and
  snapshot reuse, waiting execution persistence, lock handoff/cancellation,
  same-episode versions, movie sharing and entity-owned fields. Four scheduling
  and inventory tests also passed with the race detector.
- `go build ./...`, the TMDb loaded-details parser check and `git diff --check`
  passed. Final independent diff review covered all changed backend paths.
- Regression coverage checks missing-episode snapshot refresh, completed-job
  resumption, invalid coordinates/provider conflicts, preservation of completed
  details and directory hints, and failure isolation between sibling Episodes.
- The concurrency fixture was updated to use explicit provider IDs: current
  ingestion does not perform the legacy title-search path. Disable automatic
  metadata creation so the test actually exercises concurrent provider requests.
- An additional existing `TestKnownTMDbIDReusesLoadedDetails` case failed with
  its stale library/metadata fixture; it was not included in the passing focused
  set and was left unchanged. No claim of full-suite success is made.
- No production data repair, retry, deployment or service restart was performed.
  Live long-library timing, browser behavior and real provider latency remain
  unverified. Root artwork/credits still use existing ingestion behavior; full
  Episode details/artwork run in the durable catalog queue. No speedup factor is
  promised. Test schemas and mock servers clean themselves up.

### Root cause and prevention

- Cross-layer assumption: sharing a Series lookup was incorrectly treated as
  sharing the representative Episode attachment. Earlier read/identity fixes
  did not exercise multiple previously unbound episodes in one claimed group.
- Scheduling assumption: one catalog job was treated as a small unit although
  one long Series can contain thousands of entity requests. Progress must exist
  before lock acquisition and scheduling must yield at entity boundaries.
- Prevention is captured in shared metadata/task execution specs and executable
  multi-episode PostgreSQL regressions, not a new scheduling abstraction.

- Passed: `npm --prefix web run lint`, `npm --prefix web run build`, `node web/scripts/check-series-detail.mjs`, `git diff --check`.
- Passed targeted Go command: `go test ./internal/service ./internal/handler ./internal/repository -run 'Test.*(Series|MediaCredits|MediaVersions|UpdateMetadata)' -count=1`. Database-dependent tests skipped without `MEDIASTATION_TEST_POSTGRES_DSN`; this is not PostgreSQL integration validation.
- Independent frontend review completed; final backend review checked canonical Series edits, visibility, favorites and history boundaries.
- Mock-browser checks: exact deep link beyond first poster page, specials, season/episode/version selection, refresh and player-return restoration, favorite state, Series edit fields, failed-file isolation and retry. Light/dark viewport widths 390, 639, 640, 767, 768, 1023, 1024 and 1440 had no horizontal overflow.
- Real PostgreSQL data, real media playback and external players were not tested. These remain integration risks.
- Temporary mock page and screenshots removed; dedicated browser session and Vite server stopped. No real media data changed.
- Series edit isolation is recorded in the shared metadata specification. Database and real-playback validation remain follow-up checks.

## Watching-first refinement

- Passed final lint, production build, selection regression script, new `check-series-presentation.mjs` and `git diff --check`. Render checks cover action-before-disclosure ordering, collapsed synopsis/file paths, read-only tracks, loading text and unchanged movie defaults.
- Mock-browser checks passed: light/dark widths 390, 639, 640, 767, 768, 1023, 1024 and 1440 without horizontal overflow; primary Series action remained in the viewport despite long synopsis content. Also checked 390x844 and 768x1024, native disclosure keyboard toggling, expanded long synopsis, read-only track contents, version switch and refresh targeting, and selecting a visible card without shifting the strip.
- Reviewed mobile dark and desktop light screenshots and the final component diff separately from implementation. Browser runtime error list was empty. The mock page uses missing-artwork placeholders; real artwork/content combinations and real playback remain unverified.
- Dedicated browser and Vite server stopped; temporary page and screenshots removed. This refinement is not committed.

## Direct Series attachment 404 fix

- Reproduced the failure in real PostgreSQL with a file directly attached to Series before any Season/Episode rows exist. The old service returned nil; the corrected projection passed the same test.
- Both `TestMediaSeriesDetailSupportsEveryAttachmentLevel` and `TestMediaSeriesDetailOwnsMetadataAndUserScope` executed successfully in isolated, automatically cleaned PostgreSQL test schemas (not skipped).
- Verified the reported real file using the new service inside an explicitly read-only transaction; canonical Series projection succeeded. Removed the temporary verification source afterward; no production media data changed.
- No backend restart or deployment was performed. Live authenticated HTTP endpoints and playback still require post-restart verification.

## Scan hint / canonical Series identity fix

- Root cause: cross-layer contract confusion. Scanner directory hashes in
  `Media.SeriesID` were passed as canonical primary keys in both provider and
  local persistence. The prior detail 404 fix addressed a separate read path.
- Reproduced `record not found` using `localSeriesIdentity` in the unresolved
  episode integration test with automatic metadata fixtures disabled.
- Five focused PostgreSQL tests passed in isolated, automatically cleaned
  schemas: unresolved episode binding and readable hierarchy, provider child
  field preservation, local persistence/repeat attachment at all three levels,
  shared movie identity, and same-episode version identity. Two existing test
  cases needed their missing probe/artwork-recheck tables added locally.
- Targeted Series/MediaCredits/MediaVersions Go checks and backend build passed;
  database cases in that separate broad command skipped without a DSN.
- Final diff and both persistence callers were reviewed separately. No schema
  migration, frontend change, dependency, production data write, backend restart,
  or commit was performed for this fix. The build artifact was removed.
- Prevention: shared metadata spec now distinguishes scan hints from canonical
  ownership and requires realistic unbound-file regressions. Live library retry,
  provider success, page rendering, and playback remain unverified.

## Follow-up: directly visible series information

### Compact subtitle chooser and canonical Season cover

- Subtitles reuse the movie track chooser with default-track initialization;
  this only changes viewed information, not playback settings. Video/audio
  remain read-only rows. Season header now shows its own cover/title, with
  keyed async results, number validation, placeholder and retry.
- Added authenticated read-only `/media/:id/season`, resolving canonical
  SeasonID after file visibility checks and checking Season NSFW. Three real
  PostgreSQL tests passed in isolated schemas: new Season artwork/visibility
  regression and two existing Series hierarchy tests. The new test includes
  direct Season/Episode attachments and incorrect scan season numbers.
- Web lint/build, Go build, presentation/selection regressions and whitespace
  checks passed. Separately reviewed route, identity, response and UI call chain.
- No production restart, scrape, data write or authenticated browser test.
  Season covers require running the updated backend; no automatic deployment.

### Episode information-row parity

- Reused movie row shells/icons for read-only Episode tracks, keeping all
  track entries visible and version selection functional. Removed Episode
  ratings/Douban association, preserving Series/movie displays and Episode TMDb.
- Explicit desktop auto/flexible rows prevent long synopsis content from
  enlarging the artwork row. Browser fixture measured a 24px artwork-to-tracks
  gap with a long synopsis; 1440px and 390px had no horizontal page overflow.
- Rendering regressions cover row reuse, multi-track visibility, loading,
  scoped/standalone Episode metadata and retained Series/movie metadata.
  Separately reviewed conditions and layout changes; no API/identity changes.
- Real media playback and authenticated screenshots were not tested. Fixture
  browser closed and temporary screenshot deleted; no production data writes.

### Detail-menu organizing removal

- Removed movie/Episode organizing menu items, props, state, unused dialog and
  its sole client wrapper. Dedicated directory/library tools and backend remain.
- Build and menu/selection regressions passed. Independent diff/reference
  review confirmed metadata editing, refresh, probing and playback wiring remain.
- No live browser check or actual organizing operation was performed. No data
  or media files changed; deleted dialog source is recoverable through Git.

### Subsequent hierarchy layout

- Grouped season/episode selection and selected Episode on one surface. Added
  Episode landscape artwork/fallback and desktop columns; mobile order is still,
  metadata/actions, tracks. Season heading/count replaces the generic heading.
- Extended runnable presentation checks for artwork, stale-file isolation,
  selected-file playback, mobile DOM order, single-season and special-season UI.
- Web build and independent read-only review passed. Isolated browser fixtures
  exercised actual components at 1440px and 390px with no page overflow, inspected
  desktop light/dark and mobile dark screenshots, and confirmed selecting E2
  updates the playback link to E2 after asynchronous completion.
- Browser fixtures stubbed reads only; no authenticated library data, real
  artwork or playback was verified. No Season synopsis API was added. No
  deployment, production mutation or commit. Temporary screenshots removed.

- Removed Series/Episode metadata and read-only track disclosures; retained
  version choices, movie defaults and existing file/action targets.
- Presentation and selection regressions passed, including visible synopsis,
  selected-file path, read-only tracks and multiple-version choices.
- Web lint, TypeScript/Vite build and diff whitespace checks passed. Separately
  reviewed rendering branches and unchanged selection/action bindings.
- Live authenticated page, mobile layout and light/dark screenshots remain
  unverified (browser access previously stopped at login). Long synopsis and
  many-track media now take more vertical space. No commit or restart.
- Title-language follow-up: reproduced Thai replacing the valid Chinese Season
  label before the fix; title/parser regressions pass after restricting candidate
  languages. Three real PostgreSQL snapshot-localization tests pass, including
  historical Thai repair, repeat execution and manual metadata preservation.
  `go build ./...` and `git diff --check` pass; independently reviewed the scoped
  diff and shared callers. No frontend changes or browser verification this round.
  Read-only inspection found 53 Thai replacement candidates. A guarded repair of
  the reported Season was blocked by DBX policy; production data remains unchanged.
  No deployment, restart or commit. Historical repair awaits fixed-backend startup.
- Visible correction task follow-up: focused real-PostgreSQL service checks
  pass (concurrent edits, version gating/retry/daily logs, snapshot localization,
  media-worker concurrency and cancellation). Task definition and unavailable
  manual-action handler checks pass. Web lint/build, Go build and diff whitespace
  checks pass. Independent scoped diff review covered atomic field-limited writes,
  manual-source preservation, cancellation status and generic Web action wiring.
  Expanded checks also exposed failures outside this correction path:
  TestTaskDefinitionLogsSeparateSharedKinds (event-only messages create no log)
  and TestTaskDefinitionRunHandlerQueuesSingleAndAllMediaLibraries (reused GORM
  destination retains a primary key). Left untouched; broader suite is not green.
  No authenticated browser test, production task trigger, deployment or restart.
  Existing running processes need the new backend before this task appears.
- Invalid-season follow-up: reproduced the S00 single-episode reference being
  changed to Season 1, then passed parser/range regressions after the fix.
  PostgreSQL scan-state/upsert checks pass for S00E25, S00E01, negative-to-S02,
  unknown filenames, ordinary seasons and already-valid specials. Both cached
  and direct reads repair invalid coordinates and skip the next unchanged scan.
  Nine existing MediaUpsert regressions pass. Independently reviewed the scoped
  diff for preserved validation, metadata ownership and matched/running status.
  Production records were read only and remain -1/error; DBX direct writes are
  disabled. Updated-backend library scan is required to repair/requeue them.
  No production rescan, provider retry, restart or browser test was performed.
- Season heading/retry follow-up: presentation checks assert numbered concrete
  titles, deduplicated generated names and titled Specials. Both frontend render
  checks pass. Real PostgreSQL tests pass for scan repair, library/single scrape
  retries without scans and Series inventory sibling binding. Scoped review
  verified original-coordinate write guards and unchanged normal seasons.
  No authenticated browser/provider-production verification, production repair,
  restart or commit. The new retry path requires running the updated backend.
