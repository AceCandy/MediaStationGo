# Shared Media Metadata Contract

Recent work ordering uses the persisted `latest_media_added_at` contract in
[database-guidelines.md](./database-guidelines.md#scenario-persisted-latest-media-time).
`ListRecentLogicalWorks` pages ordinary/NFO/HongGuo identities using global work
time and file existence; restore that page order after source-specific hydration.
`ListRecentByLibraries` deduplicates visible representatives for statistics.
Web final card sorting must not replace this time with file `CreatedAt` or metadata
`UpdatedAt`. Emby Latest and DateLastContentAdded use work time; ordinary/HongGuo DateCreated and
default release-date listing remain unchanged.

NFO item DateCreated uses first `nfo_items.created_at` through
`MediaView.CatalogCreatedAt`; file CreatedAt and MediaSource/PlaybackInfo dates
remain file facts. Adult visibility is exclusively library-scoped; no model,
API, editing, import or search contract retains media-level NSFW flags.

Ordinary Emby `latestMetadataViews` / `latestSeriesGroups` exclude works with
NULL latest time; the timestamp never replaces library eligibility or playback
filters. Both library-scoped candidates use `FilterWorkLibraries` over persisted
`library_ids`, ordering the complete candidate superset behind `OFFSET 0` before
the unchanged eligibility EXISTS. Do not reintroduce full-library Media identity
enumeration or the retired 512-work prefix/fallback. Global Latest keeps its prior
scope. Unknown library sets pass through to exact checks; no pre-filter LIMIT is
allowed. Web `ListRecentLogicalWorks` applies the same prefilter for explicit
allowed libraries on ordinary/HongGuo candidates; hidden-library and other file
filters still apply independently, and NFO retains its single-library identity.
Keep `EXISTS (... OFFSET 0)` for episode qualification and reuse effective state.
Ordinary
Latest filters episodes before Series grouping: a partially watched Series can
appear in both played and unplayed lists, with counts scoped to matching episodes.
`TestEmbyLatestCandidatePlans` verifies real PostgreSQL plans with multiple seasons,
versions and populated playback history, with the target library both newer and
older than other libraries, including small libraries. Capture actual SQL with
bound variables, never replay interpolated array parameters. Bound repeated file work and correlated subplan loops,
not only the number of hydrated results; empty results may exhaust candidates. The partial-playback
contract is covered by `TestEmbyLatestSeriesKeepsPartiallyPlayedWorkInBothLists`.
`TestEmbyLatestSeriesItemsContinueThroughCandidateTimeTie` also covers cross-library
time, deletion fallback, many unrelated recent works and unknown membership.
Do not truncate candidates to achieve a performance target.

## Scenario: Season-Batched Episode Metadata and Season-Owned Credits

### 1. Scope / Trigger

Background catalog hydration, deferred episode scraping, TMDb rechecks,
snapshot backfill and artwork repair use Season inventories. This contract
supersedes older requirements for per-Episode extended responses and credits.

### 2. Signatures

- `withTMDbSeasonBatch(ctx)` enables execution-local Season reuse.
- `GetTVEpisodeDetails` splits `episodes[]` in that scope; outside it, manual
  refresh and identity edits retain the original complete single-episode request.
- `PersonRepository.ListCreditsWithPeopleByMetadataIDs` resolves Episode to
  Season, but projects the requested metadata ID for existing batch consumers.

### 3. Contracts

- Fetch `zh-CN` first. Missing/generated/non-Chinese episode text can be filled
  from the Series `original_language`, not an arbitrary English translation.
  Keep usable Chinese text and match supplemental data by episode number and ID.
- A cache fill performs at most two Season detail requests, excluding the Series
  language lookup and HTTP retries. The cache retains at most 32 completed
  Seasons / 16 MiB of raw JSON; eviction or a new execution permits fresh reads.
  Concurrent requests for one Season share work; canceled waiters do not cancel
  another owner's request. Errors are not retained.
- Preserve the Episode's own image path and unknown JSON fields. Keep image
  selection, download retries and per-Episode completion checkpoints independent.
- A Season-derived Episode JSON must not overwrite a same-ID historical snapshot
  containing extended objects. Preserve that snapshot and its fetch time; metadata
  fields may still update. Full manual refresh or a changed identity can replace it.
- Web/Emby Episode credits come from its Season, never Series. Do not copy Season
  rows into Episodes. Person filters must use the same ownership in single-file,
  Series and search paths. Movie/Series/Season retain their own credit ownership.
- Keep Season relationships in the TMDb center and resolve them at read time.
  Ordinary metadata has no copied credit table. Shared people IDs and avatars
  remain independent of the source relationships.

### 4. Validation & Error Matrix

- Missing `episodes` or mismatched identity -> retryable invalid response.
- Valid inventory without target Episode -> not-found recheck after 72 hours,
  without claiming HTTP 404, requesting a single Episode, or deleting media.
- Both languages lack text -> keep generated title/empty overview; no third language.
- Missing Season credits -> return empty, never substitute Series credits.

### 5. Good / Base / Bad Cases

- Good: siblings reuse a Season response and expose one Season's actor list.
- Base: a fresh execution refetches; a manual Episode refresh keeps full raw JSON.
- Bad: delete shared people with Episode relationships, or clear extended history
  because the current inventory omitted extension fields.

### 6. Tests Required

- `TestTMDbSeasonBatch*`: request count, language, raw fields, concurrency and cancellation.
- `TestSeasonDerivedSnapshotPreservesFullEpisode`: same-ID preservation and changed ID.
- `TestEpisodeCreditsReadSeasonWithoutWritingEpisode`, `TestListEpisodeCreditsUsesSeason`,
  `TestEmbySeasonAndEpisodeDoNotInheritSeriesArtworkOrPeople`: read/write and batch/filter scope.
- Database tests must execute with `MEDIASTATION_TEST_POSTGRES_DSN`; skips do not count.

### 7. Wrong vs Correct

- Wrong: request each Episode for extensions that the background workflow no longer needs.
- Correct: split authoritative Season inventory; reserve complete single requests for manual operations.
- Wrong: reuse Season actors by inserting one relationship per Episode.
- Correct: persist Season relationships once and resolve them at read time.

## Scenario: Library Metadata Pagination

### 1. Scope / Trigger

- Web library cards and Emby Movie/Series/Season/Episode browsing must paginate logical metadata before loading file views.

### 2. Signatures

- `MediaViewRepository.ListLibraryMetadataPage(ctx, libraryID, kind, metadataID, offset, limit, filter)` returns page representatives, summaries and logical total.
- `ListLibrarySeriesCards(ctx, libraryID, page, pageSize, seriesID, key, visibility)` applies target filters before pagination.
- Emby uses `seriesSummaries`, `movieLibraryItems` and `hierarchyItems`; playback retains complete loaders.
- Series/Season `Item` details use `containerDetail`: SQL collapses visible Parts
  and versions, returns counts plus a representative ID, and hydrates at most one
  file view. Preserve the original representative library, preferred-version
  creation date, metadata-owned fields and user state. Never put a detail summary
  into complete Series/Season caches or replace the playback loaders.

### 3. Contracts

- File associations establish library membership and visibility; unlinked catalog metadata does not enter library cards.
- Web movie cards return `version_count`, not expanded `versions`. Concrete `id` remains usable for operations; `metadata_id` owns movie navigation and favorite display identity.
- Series cards use `metadata:<series-id>` keys, preserve concrete `rep.id`/`linkMedia.id`, and display Series-owned metadata. Old hashed links are rejected without enumerating work IDs. Client grouping hashes are internal only; navigation uses canonical identity or the representative media detail.
- Count, filtering and page selection share one scoped query. Missing-poster/title filters apply to the displayed work, not its representative episode.
- Emby summary counts must never overwrite complete Series/Season group caches. Load current-page movie/episode versions only where payload compatibility requires them.
- SQL still scans/aggregates matching associations for totals and ordering; bounded file hydration is not a constant-time database guarantee.
- Web `ListLibrarySeriesViews` places scoped files inside a derived Media table
  with `OFFSET 0` before joining display metadata. An outer `IN (subquery)` alone
  can project the whole library before filtering a small series. Keep the single
  statement, visibility scope, complete versions and ordering; verify identifier
  lookup loops with `TestLibrarySeriesEpisodesScopesProjectionBeforeJoins`.
- Ordinary Web Series lists without an explicit metadata ID start from visible
  Series candidates using persisted membership, then apply poster/title filters
  once per work. Poster presence still requires a linked artwork asset; Chinese
  title matching preserves the displayed Series title/original-name fallback.
  Order by persisted global `latest_media_added_at DESC NULLS LAST, id DESC`,
  not by Episode release dates, years or file update times. New files anywhere
  under the work, including another library, update this order; file visibility
  remains restricted to the requested library and permissions.
  Maintained nonempty membership qualifies ordinary Series/Season/Episode file
  associations for this list; do not repeat file-date aggregation or version
  statistics before pagination. Unknown membership (`library_ids IS NULL`)
  retains one library-scoped exact hierarchy/file qualification; no unknown
  candidates means that fallback must execute no Media reads.
  Totals and page selection share the same qualified work set. Only for the
  selected page, materialize its Seasons and expand direct Series/Season and
  Episode associations, then probe files by metadata ID inside an `OFFSET 0`
  boundary before applying library visibility. This prevents stale library
  statistics from choosing a library scan per page Episode. Preserve the
  original representative ordering and complete distinct item/version counts.
  An empty page retains its accurate aggregate total via LEFT JOIN.
  `TestLibrarySeriesPageUsesGlobalWorkTime` covers conflicting release dates,
  hidden-library global dates, new versions/episodes, deletion, NULL time and
  unknown-membership permissions. `TestLibrarySeriesWorkFiltersBeforeFiles`
  checks work-bounded artwork lookups, zero file reads for empty filters and
  known-member count fallback, and page-bounded version reads with normal
  statistics. `TestLibraryFilteredSeriesPageWithStaleStatistics` forces unknown
  membership after populating a new library without statistics refresh and
  checks the exact fallback's file scan and metadata traversal bounds. Page
  version index probes are distinct from this whole-library fallback; do not
  count bounded per-item probes as repeated whole-library scans.
  Explicit-Series reads retain their selective file scope and original date
  expressions; Movie pages retain multipart/version preference rules.
- All Movie pages, including unfiltered browsing, join Movie metadata directly
  (`work.id = mi.id`), without Season/Series hierarchy expansion. A parent CASE
  join can prevent the work ID from restricting Media through its metadata index,
  causing a library file scan per candidate. The actual movie count/page plan in
  `TestLibraryMoviePageKeepsVersionsWithWorkTimeOrder` must have no executed
  Season metadata scans and retain the existing file-visit bound.
  Keep the existing multipart primary selection and
  preferred-version ordering. `libraryMoviePage` first qualifies work candidates
  using persisted membership and file eligibility. After Movie identity and
  work filters are established, eligibility probes Media directly without
  rereading Movie metadata or self-joining Media. Enforce target-library file
  visibility even when persisted membership is unknown (`library_ids IS NULL`).
  Part minima still use the original visible, filtered file scope, including an
  explicit metadata ID when supplied; never restrict them to each candidate.
  Sorting uses persisted global `latest_media_added_at`; only selected works
  count versions. The Movie count/page statement runs with `SET LOCAL jit=off`
  in a read-only transaction to avoid seconds of JIT compilation. Restore the
  prior value on success, including inside an existing transaction; rollback
  restores it on failure. Never change the session/global setting. Count/page share one candidate CTE, including empty pages;
  no file views are loaded for an empty result. Keep Part minima library-scoped,
  even when an existing Part group spans metadata identities.
- `seriesMetadataPage` receives the visible Media/episode scope without the
  Season/Series joins. `seriesWorkPage` and `metadataWorkPage` select work IDs
  with persisted membership plus original file eligibility. Count/page share
  `work_candidates`; only file-dependent ordering computes dates/title fallback.
  Keep parent-kind checks and exact sort directions, null handling and tie keys.
  Current-page summaries retain `seriesScopeQuery`; resumable and bounded
  child lists retain their existing file/state-driven query contracts.
- Filters referencing Series IDs must not add a JOIN whose ON clause precedes
  those parent aliases; use a correlated WHERE/EXISTS for favorites and preserve
  user plus soft-delete predicates. Test combined filters after moving joins.

### 4. Validation & Error Matrix

- Restricted empty scope -> empty page and zero total; out-of-range page -> empty page with matching total.
- Work in a hidden/disallowed library -> excluded before count/page; query failure -> error, not a successful empty response.

### 5. Good/Base/Bad Cases

- Good: one Series card reads one Web representative or zero Emby file views.
- Base: a multipart movie contributes one version for its visible primary part.
- Bad: load every episode/version, build all payloads, then slice in memory.

### 6. Tests Required

- `TestLibraryMetadataPaginationBoundsFileReads`: page-sized file reads, logical totals, cross-library isolation, filters, adult-library restrictions, retired-link rejection, multipart counts, empty pages and complete detail after summaries.
- `TestListLibrarySeriesDoesNotTruncateLargeEpisodeLibraries`: all 2001 episodes remain available when selecting that Series.
- `TestEmbyContainerDetailBoundsReadsAndPreservesPayload`: compare full legacy
  payloads with bounded detail reads for large multi-version series, Parts,
  specials, favorites, played state, hidden libraries and empty scopes.
- `TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog`: execute count/page
  EXPLAIN against a catalog larger than the playable set; Media probe loops must
  stay within file cardinality. Cover Yamby ordering, version collapse, empty
  pages, favorite/person/visibility filters and played-Series summaries.
- `TestLibraryMoviePageKeepsVersionsWithWorkTimeOrder`: candidate metadata visits
  must stay within the catalog cardinality, with no repeated eligibility joins.
- `TestLibraryMoviePageScopesJIT`: query-local off, prior on/off restoration,
  standalone/nested transaction success and actual SQL failure rollback.
- `TestLibraryMoviePageEnforcesUnknownMembershipVisibility`: unknown membership
  cannot bypass hidden/allowed library constraints.
- Run PostgreSQL tests with `MEDIASTATION_TEST_POSTGRES_DSN`, Web lint/build and both Series check scripts; skipped database tests do not count as validation.

### 7. Wrong vs Correct

- Wrong: `loadAllFiles -> group -> payload -> pageSlice`.
- Correct: `scoped logical count/page SQL -> current-page metadata/representatives -> payload`.

## Scenario: Explicit TMDb Metadata Refresh

### 1. Scope / Trigger

- The detail-page action `刷新tmdb信息` refreshes only its current canonical entity.

### 2. Signatures

- Admin-only `POST /api/metadata/:id/tmdb/refresh`, without a request body.
- Admin-only `POST /api/discover/tmdb/:kind/:id/refresh`, without a request body; `ScraperService.RefreshMetadataTMDbByIdentity` resolves the existing same-kind metadata before delegating.
- `ScraperService.RefreshMetadataTMDb(ctx, metadataID) error`.

### 3. Contracts

- Read the unique same-kind TMDb identifier from metadata, never Media scan hints.
- The discover refresh accepts only a positive TMDb ID with `kind=movie|tv`, maps `tv` to `series`, and returns not-found without creating a metadata or Media row when no canonical identifier exists.
- Manual metadata refresh always fetches fresh details, regardless of existing snapshots or catalog checkpoints. The discover identity wrapper reuses a non-degraded TMDb snapshot younger than three hours; it returns local details without advancing the timestamp.
- Validate the returned entity ID; Season/Episode coordinates come from canonical parents.
- Update the current entity's display fields, loaded credit scopes, managed TMDb images,
  and raw snapshot. Preserve identity, other provider identifiers and hierarchy.
- Do not create children, enqueue catalog work, rematch, or write Media rows.
- Explicit image refresh bypasses the source URL cache and replaces the selection only
  after importing valid bytes. An absent upstream image preserves the old selection.
- Snapshot freshness advances only after all writes succeed. A persistence failure can
  leave partial field/credit/image updates; return an error and allow an explicit retry.
- The Web action submits `metadata_id`, suppresses duplicate clicks, and reloads details
  after `200 {"status":"complete"}`. Identity validation remains server-owned.
- Show a loading toast and blocking themed modal immediately; retain them through detail
  reload, then replace the toast with the outcome and restore page interaction.

### 4. Validation & Error Matrix

- Non-admin -> `403`; missing metadata -> `404`.
- Missing/ambiguous TMDb identity, invalid hierarchy, or returned-ID mismatch -> `400`.
- Provider or persistence failure -> `502` with no credentials or upstream URLs.

### 5. Good / Base / Bad Cases

- Good: refresh a catalog-only entity or a Movie with several media versions; Media stays unchanged.
- Base: TMDb has no own image; refresh other information and retain the existing image.
- Bad: reset Media to `pending`, reuse a completed catalog checkpoint, or refresh the whole Series tree.

### 6. Tests Required

- Four entity kinds, repeat network requests, unchanged Media, no new children, fresh images,
  loaded-credit replacement, missing/mismatched identity, failed images without snapshot advancement,
  admin gating, Web lint, and production build.

### 7. Wrong vs Correct

- Wrong: `POST /media/:id/scrape` to refresh already-bound canonical information.
- Correct: `POST /metadata/:id/tmdb/refresh` with the detail's `metadata_id`.

## Scenario: People Credits and Localization

- Ordinary work credits come from current TMDb subject-origin relations through
  `metadata_credit_presentations`; `MetadataCredit` is a read DTO. Movie,
  Series and Season read their own source. Episode reads its Season; it never
  copies credits or inherits Series credits. Ambiguous bindings expose no credits.
- `people` and `person_identifiers` retain stable identity and local image
  ownership. `person_presentations` reads current TMDb name/original name and
  biography, with a translation matching the current original text. Independent
  local/NFO people keep their own data. Source changes never fall back to obsolete
  TMDb copies in `people`.
- Source relations and translations are separate. Role caches use TMDb subject
  ID, original role, language and prompt version. Name caches use public person
  ID and current source original name. Changed originals cannot reuse old text.
- Translation candidates read source credits directly, including unbound source
  works. Page by subject ID and credit ID, hydrate only the current page's subject
  and parent, and retain 1,000-group/100-entry limits plus negative-cache handling.
  A source partial index bounds scans of already localized roles.
- Source profile/credit and translation-cache changes enqueue affected metadata
  for search and response-cache invalidation. Source display reads do not wait
  for a copy task. Translation cache hits require no upstream request.
- `SaveCreditPeople` maintains public identities/local avatars only. Concurrent
  first creation reuses the unique provider identity transactionally and removes
  only the losing transaction's provisional person.
- Verify source refresh, stale translation isolation, unbound roles, long roles,
  shared person creation, source indexing, and current-page Web/Emby credits with
  real PostgreSQL. Independent NFO/HongGuo/HuangGuo data paths remain separate.

## Scenario: Canonical Metadata and Managed Artwork

### 1. Scope / Trigger

- Apply this contract whenever scanner, scraper, media queries, playback, Emby, search, or artwork storage changes.
- `Media` owns playable-file facts and unresolved scan hints. `MetadataItem` owns shared display metadata. `ArtworkAsset` owns persistent image bytes.
- This is a fresh-install schema. Do not add legacy field dual writes or historical backfills unless a separate migration task requires them.

### 2. Signatures

- Canonical entities: `MetadataItem{Kind, ParentID, SeasonNum, EpisodeNum, Title, ..., Source}`.
- Local scan fingerprint: `Media{ScanFileSizeBytes, ScanFileMTimeNS}` stores the scanned media file's own size and nanosecond mtime.
- Flat technical API projection: `Media{DurationSec, SizeBytes, Container, Width, Height, VideoCodec, AudioCodec}` keeps the existing JSON shape but every field uses `gorm:"-"`; the `media` table has no corresponding column.
- External identity: `MetadataIdentifier{MetadataID, Provider, EntityKind, ExternalID}` with global uniqueness on `(provider, entity_kind, external_id)`.
- Season identity: `(parent_series_id, season_num)`; episode identity:
  `(parent_season_id, episode_num)`. TMDb catalog hydration also requires each
  Season/Episode's own provider identifier; local-only entities may remain
  hierarchy-identified.
- File link: nullable `Media.MetadataID` while scan status is unresolved; every
  non-null value has a restrictive foreign key to `MetadataItem.ID`.
- Metadata-owned state: `Favorite.MetadataID`, `PlaybackHistory.MetadataID`, and `PlaylistItem.MetadataID` are non-null; `MediaID` only selects a concrete playable version.
- Read model: `MediaViewRepository.FindByID`, `FindByIDs`, `ListByLibrariesFiltered`, and `SearchFilteredPage`.
- Library missing-metadata filters: `GET /api/libraries/:id/media` and
  `GET /api/libraries/:id/series` accept `missing_poster=1` and
  `missing_chinese_title=1`; omitted values leave the corresponding filter off.
- Emby logical-page boundary: `metadataPage` selects and paginates distinct
  `MetadataItem.ID` values, batch-loads visible `MediaView` versions for only
  that page, then `preferredMetadataViews` selects one representative view per
  logical item.
- Complete technical facts: `MediaProbeMetadata{MediaID, ProbeJSON, SchemaVersion, SummaryVersion, DurationMS, SizeBytes, Container, BitRate, Width, Height, VideoCodec, AudioCodec, ProbedAt}` uses `media_id` as both primary key and a cascading foreign key to `media(id)`. Lists may join only the typed summary columns; complete `ProbeJSON` remains detail/playback-only.
- Probe document: `ProbeDocumentSchemaVersion` and `MarshalProbeDocument` / `UnmarshalProbeDocument` own the versioned JSON contract. Only typed format, video, audio, subtitle, chapter, safe tag, disposition, and color/HDR fields are persistable.
- Emby playback response: `PlaybackInfo{MediaSources, PlaySessionId, DateCreated}`; `DateCreated` is the selected concrete `Media.CreatedAt` encoded as a non-null UTC JSON timestamp with seven fractional digits and a trailing `Z`.
- Web playback response: `GET /api/playback/:id/info -> {media, stream_url}`; an unresolved media uses raw file identity plus probe summary fields without becoming a general `MediaView` result.
- Duplicate report media projection: `DuplicateMedia{ID, Title, Path, SizeBytes, LibraryName}`; it never serializes the complete `Media` model.
- Direct-only source response: `EmbyMediaSource{DirectStreamUrl, SupportsDirectPlay, SupportsDirectStream, SupportsTranscoding=false}`; `TranscodingUrl` is absent.
- Probe execution boundary: `FFprobeService.Probe(context.Context, path)` and `ProbeHTTP(context.Context, rawURL)` invoke only the resolved `ffprobe` executable.
- Track probe path mapping setting: `ffprobe.path_mappings` contains one
  `remote HTTP(S) URL prefix => absolute local path prefix` rule per line.
- Remote HTTP(S) probing waits a cancellable random whole-second delay from two
  through five seconds before invoking ffprobe; local probing starts immediately.
- Direct playback routes: `/api/stream/:id` and Emby `/Videos/:id/{stream,original}` GET/HEAD variants serve unchanged bytes or a protocol-neutral HTTP redirect. HLS playlist/segment and transcode status routes do not exist.
- Redirect pre-resolution setting: `playback.redirect_resolve_prefixes` contains
  one literal HTTP/HTTPS URL prefix per line and applies to persisted STRM
  targets and the final URL produced by `playback.path_mappings`.
- Track backfill APIs: admin-only `POST /api/libraries/:id/probe` backfills one
  library; `POST /api/tasks/definitions/probe_backfill/run` starts a global
  service-lifetime task and accepts optional JSON
  `{limit: positive integer, library_id: string}`.
  Both report `total`, `completed`, `skipped`, and `failed` metrics.
  Each attempted item appends one marked task detail: `✅ <media-id> <media.path>`
  on success or `❌ <media-id> <media.path> <sanitized-error>` on failure. The
  path identifies the administrator-visible local media target; errors never
  include a remote URL.
- Manual apply API: `POST /api/media/:id/scrape/apply` accepts `ManualScrapeRequest`, persists metadata through `ScraperService.ApplyManualMatch`, then returns the refreshed `MediaView` from `MediaService.GetMedia`.
- Task-center manual recovery target: `MediaScrapeIssue{id, title}` identifies the raw `Media` row used by manual search and apply; opening the dialog does not require `GET /api/media/:id`.
- Artwork response: `/api/artwork/:assetID`; originals live under `App.DataDir/catalogs/<source>/artwork/aa/bb/cc/...`.
- Library deletion: `DELETE /api/libraries/:id` -> `MediaService.DeleteLibrary(ctx, id)`.
- Scrape entrypoints (`POST /api/media/:id/scrape`, manual apply, scan
  auto-scrape, STRM refresh, and task-center `media_scrape`) enrich metadata
  only and never invoke `OrganizerService`. The former library-wide forced
  rescrape routes, including `POST /api/libraries/:id/scrape` and
  `repair-rescrape`, are retired and must remain unregistered.
- Regular scrape entrypoints never infer an adult code or call `AdultProvider`. `ScraperService.AnyEnabled` reports regular provider availability and excludes the adult provider.
- Adult network lookup requires an explicit adult operation: manual search whose provider set contains `adult`, manual apply with `source=adult`, or organize with `mediaType=adult`. Manual search with an empty provider or `provider=all` is not an explicit adult operation.

### 3. Contracts

- Scanner resolves exact provider identifiers before writing media but never
  creates local metadata. An exact movie identity binds the Movie; an episodic
  identity binds only when its stored `Series -> Season -> Episode` hierarchy
  already contains that episode.
- Library type is authoritative for movie-versus-episode classification.
  `movie` and `nfo_movie` never become episodic because of persisted or parsed
  `season_num`, `episode_num`, or `series_hint` values. Filename scanning in
  `tv`, `nfo_tv`, `show`, and `shows` libraries accepts only case-insensitive
  `S` plus one or two season digits followed by `E` plus one to three episode
  digits; `anime` and `variety` retain the compatibility episode parser.
- After successfully walking a movie-library root, scanner transactionally
  clears dirty episodic fields for media under that root. A Movie metadata link
  is preserved; a non-Movie link is removed and requeued; `error` and
  `no_match` rows are reset to pending. The reconciliation count is reported in
  scan progress, task metrics, logs, and notifications. Do not perform this as
  a startup migration or across unscanned libraries or roots.
- An existing local media row is unchanged only when its persisted
  `ScanFileSizeBytes` and nonzero `ScanFileMTimeNS` equal the current file.
  This check runs before NFO, sidecar, path-derived metadata, or STRM target
  reads. Those derived values never trigger an update by themselves.
- A missing legacy fingerprint performs one normal update to persist both
  values. A size or mtime change follows the normal scan update flow.
- When exact identity does not resolve, scanner persists the media with
  `metadata_id = NULL`, scan hints, and `scrape_status=pending`. Provider or
  eligible local persistence fills the link after scan.
- A reliable provider ID resolves by `(provider, entity_kind, external_id)`. TMDb is optional; Douban-only and provider-less manual metadata are valid.
- Automatic scraping (including retries and organize-time lookup) requires the
  corresponding explicit provider ID from media scan hints, NFO, or path tags.
  Use `matchFromMediaExternalIDsWithOutcome`; never search by title/year or
  automatically select a search candidate. A failed ID lookup can try another
  provider only when that provider has its own explicit ID. Returned IDs must
  match the requested provider ID. TMDb detail 404 means no match; other
  provider errors preserve the error state. No match preserves the media row.
- Manual search and explicit user-selected apply remain available. Local NFO
  fallback, NFO-only libraries, and explicitly requested adult-code operations
  remain separate from automatic provider name matching.
- Provider match enriches one canonical `MetadataItem`, its identifiers and managed artwork, then links every matching file through `Media.MetadataID`.
- For Movie and Series automatic TMDb matches, nonempty online fields win;
  missing display fields retain canonical values before falling back to NFO.
  `useCanonicalTMDbLookupIDs` restores confirmed same-kind TMDb canonical
  cross-provider identifiers before exact reuse, so stale NFO cannot put old
  IDs back into Media lookup hints. It does not rewrite NFO or force a network
  refresh of reusable metadata. NFO-only libraries bypass this policy.
- Series sibling binding tolerates auxiliary-ID differences only when both
  files share the positive TMDb ID of the TMDb-owned canonical Series. It
  preserves missing auxiliary values and still rejects different TMDb IDs;
  non-TMDb groups keep the full conflict check. Regression tests:
  `TestTMDbOnlinePriorityMoviesAndSeries` (online values, missing-value retention,
  IMDb correction and stale-NFO retries) and
  `TestSeriesInventoryBindsOwnEpisodesAndReusesSnapshot` (TVDB correction plus
  different-TMDb rejection).
- `Media.SeriesID` maps to `series_hint` and may contain a directory hash; never
  pass it as `UpsertCanonical`'s preferred primary key. Provider and local
  persistence use `preferredScrapeMetadataID`: resolve existing Series ownership
  only through `Media.MetadataID` and its Season/Episode parent chain. Unlinked
  files resolve/create by provider or local identifiers. Database errors propagate.
  PostgreSQL regressions must use a real `localSeriesIdentity` hint, no automatic
  metadata fixtures, and verify canonical episode visibility plus idempotent
  rescraping of direct Series, Season, and Episode attachments.
- `showDirFromEpisodePath` accepts explicit `(DoVi)` and `(HDR10)` season-folder
  suffixes case-insensitively, so version folders share the show directory and
  local identity. Do not strip arbitrary parenthesized suffixes such as a year
  or `(Interview)`. Keep Windows separators and filename-ID isolation covered
  by `TestShowDirFromEpisodePath` and the path-hint tests. A path/lookup provider
  ID is not proof of a canonical `MetadataIdentifier` association; inspect both
  when diagnosing duplicate works.
- `TMDbProvider.GetMovieMatch` and `GetTVMatch` mark their `Match` as containing
  complete TMDb details. Persistence saves languages, countries, and genres
  from that match without issuing a duplicate `GetDetails` request. Search-only
  matches remain unmarked and still fetch extended details once.
- Before regular provider scraping, an existing media link is revalidated by the current provider identifiers. If the exact movie or `Series -> Season -> Episode` metadata resolves to the same canonical ID, the media is marked `scrape_status=matched` without provider calls or writes to `metadata_items` and related canonical tables. `IncludeMatched` and `RefreshWeakMatched` bypass this shortcut.
- Series and Season metadata only locate an existing Episode; a newly discovered Episode is scraped independently. An existing Episode with a generated placeholder title (for example `第 1 集` or `Episode 1`) is reusable for seven days after `metadata_items.updated_at`; after that it must be scraped again. Missing or incomplete episode hierarchy always continues through provider scraping.
- Provider no-match may import existing NFO and sidecar images. Provider error or timeout must set an error state and must not fall back to local metadata.
- Movie and series identifiers include `EntityKind`; equal numeric IDs across kinds or providers must not collide.
- Season rows require a parent Series, `season_num >= 0`, and `episode_num = 0`. Episode rows require a parent Season, `season_num = 0`, and `episode_num > 0`. Movie and Series rows have no parent or episodic position.
- NFO season parsing preserves the raw field: explicit integer `0` is a valid
  Special season, not an omitted value. `mergeEpisodeMetadata` accepts only
  explicit nonnegative integers; empty, absent, invalid and negative values do
  not override an existing season. Regression fixtures must combine an actual
  `tvshow.nfo` season -1 with an episode NFO season 0 and exercise scrape retry,
  not merely repair a database row before the later NFO read.
- An unowned provider identifier may be attached to the current metadata. A corrected ID from the same provider/kind replaces the stale ID on that metadata in the same transaction.
- If an identifier already belongs to another metadata, automatic graph merge requires an explicit provider crosswalk or user confirmation. Title/year/similarity evidence alone must not authorize merge.
- Graph merge recursively pairs Series children by season and episode number, moves media and metadata-owned state, deduplicates user relations, then hard-deletes the unreferenced source metadata.
- Lists, permissions, pagination, search, playback display text, and Emby display text read `MediaView`. File identity, path, scan fingerprint, and STRM URL read the embedded `Media`; duration, target size, real container, bitrate, dimensions, and codecs read `MediaProbeMetadata`.
- A local `.strm` keeps the sidecar in `Media.Path` and its supported absolute media target in `Media.STRMURL`. Scan, manual reprobe, and asynchronous PlaybackInfo repair probe the target while persisting technical facts to its one-to-one probe row; stale target results must be discarded.
- STRM HTTP(S) targets treat every raw `#` as a legacy unescaped path character
  and normalize it to `%23` before persistence or consumption. Existing `%23`
  and query values remain unchanged. This normalization is shared by scanning,
  probing, path mapping, direct playback redirects, and Emby target-container
  projection so historical rows require no migration.
- Only a `Media` whose path ends in `.strm` may
  apply `ffprobe.path_mappings`. Rules require a credential-free HTTP(S)
  prefix with a host and an absolute local prefix. Matching compares scheme,
  host, and complete decoded URL path segments; query values never enter the
  local path. After the STRM compatibility normalization above, no raw fragment
  remains. The longest matching URL path wins, and the
  joined path must remain below its configured local prefix.
- A mapped readable regular file uses the local `ProbeFile` path without remote
  delay. Invalid/unmatched rules and unavailable mapped files preserve the
  original URL, delay, and `ProbeHTTP` behavior. Source validation rereads the
  mapping after ffprobe and before opening the persistence transaction; do not
  query the regular setting repository from inside that transaction.
- A successful full probe atomically upserts the complete document and its typed summary after rechecking the source identity. Projection first clears every typed summary value, then derives all fields from the current document and local target identity so absent values become unknown instead of retaining stale facts. Scanner, scraper, organizer, and playback never persist technical facts on `media`. Failed, partial, or stale probes must not replace the previous valid complete document, and probe failure must never fall back to `ffmpeg -i`.
- An ffprobe execution failure may include a bounded single-line stderr summary
  after replacing its exact local path or remote URL and applying task-log URL
  sanitization. It retains the original execution error for classification and
  never exposes a signed query or grows one task detail without a fixed limit.
- Local probing and automatic damaged-file cleanup follow the fixed-handle,
  quarantine/recheck and scanner coordination contract in
  [Session and Transfer Safety](./session-and-transfer-safety.md#probe-cleanup-safety).
- Global track backfill scans every non-deleted `Media` across libraries, probes
  only missing, outdated, or invalid complete documents, and skips valid current
  documents. A positive `limit` caps actual probe attempts; valid skipped rows
  do not consume that limit, while an omitted/zero limit processes all rows.
  An omitted/empty `library_id` selects every library; a valid ID restricts the
  same conditional backfill to that library and is resolved before task creation.
  The task center exposes the server-owned action and rejects a new global run
  while another probe task is active in the current process.
- `POST /api/media/:id/probe` always forces a fresh probe and may overwrite the
  current valid document only after successful source validation. UI callers
  must present an explicit overwrite confirmation. Library and task-center
  backfill entrypoints remain conditional and never force valid documents.
- Probe JSON must never contain the input filename/path/URL, signed query, request headers, cookies, authorization values, route tokens, attachments, or arbitrary metadata. Unknown schema versions, malformed JSON, duplicate/negative stream indexes, attached pictures, and unsupported stream types are invalid and leave technical facts unknown while allowing lazy repair.
- Emby `PlaybackInfo` must enumerate every visible sibling `Media` version before scheduling asynchronous track repair. The playback-layer in-flight map deduplicates by `Media.ID`; the `FFprobeService` limiter remains the only actual probe concurrency limit.
- Emby `PlaybackInfo.DateCreated` must be present at the response top level as well as on each `MediaSource`. Compatibility fields must be verified at the exact JSON layer consumed by the client; a same-named field on the item or nested source does not satisfy a top-level contract.
- Emby detail and PlaybackInfo batch-load valid probe documents and map every embedded video/audio/subtitle by its absolute ffprobe stream index. Sidecar subtitles are rediscovered and deterministically indexed after the highest embedded index on every response.
- One PlaybackInfo request reuses its visible sibling views, one batch probe read,
  and one sidecar discovery per sibling for both selection validation and
  `MediaSources` mapping. Reuse is request-local; never cache these
  user-filtered views or filesystem results across requests.
- `GET /api/media/:id` attaches an optional `tracks` array through `MediaService.GetMedia` only. Each `MediaTrack` is a typed whitelist projection of video/audio/subtitle facts with the original absolute `index`; it excludes probe paths, URLs, headers, credentials, arbitrary tags, and unsupported stream types. Missing or invalid probe data omits the array, while list/search responses do not load or expose it.
- Emby paginated browse/list payloads use scalar media fields only: they do not load complete probe documents, scan sidecar subtitles, or schedule lazy track repair.
- Embedded subtitles remain non-external `MediaStreams` and receive no extraction `DeliveryUrl`. Supported external SRT/ASS/SSA/VTT sidecars receive a controlled token-aware `DeliveryUrl`; delivery revalidates the current stream/index and never accepts a caller-supplied filesystem path.
- GET query and POST body playback selections preserve omitted, `0`, and `-1`. Values below `-1`, missing explicit audio indexes, unknown subtitle indexes, and media-source IDs outside the visible sibling set are rejected.
- PlaybackInfo, user policy, server capability, and device-profile responses are direct-only: `SupportsTranscoding=false`, conversion/remux fields are false or omitted, transcoding profiles are empty, and no transcoding URL is returned.
- Audio and subtitle selection changes only the selected indexes carried through PlaybackInfo and the unchanged original stream. A client that cannot decode the source container or codec receives no HLS, remux, or conversion fallback.
- An external absolute HTTP/HTTPS target is redirected unchanged, including its
  existing query, unless its post-mapping URL matches a configured
  `playback.redirect_resolve_prefixes` entry. Matching is case-sensitive after
  trimming each line; empty, relative, credential-bearing, and non-HTTP/HTTPS
  entries do not participate.
- A matched target is requested once with GET, `Range: bytes=0-0`, and the
  exact player `User-Agent`; no Authorization, Cookie, stored provider header,
  or player header is forwarded. Automatic redirect following is disabled. The
  first 3xx `Location` is resolved against the requested URL and accepted only
  as an absolute credential-free HTTP/HTTPS URL; later hops are client-owned.
- A successful resolved target is cached in memory for one hour by the actual
  post-mapping source URL and exact `User-Agent`. Failures, timeouts, non-3xx
  responses, and missing or invalid locations are not cached and return the
  original URL. Player-facing redirects remain HTTP 302 with `no-store`.
- External original and resolved targets receive no MediaStation token or
  `media_id`. A same-origin internal `/api/stream/:id` redirect may receive only
  the existing short-lived token scoped to that media ID. Redirect logs expose
  no query value, signed URL, credential, Cookie, Authorization, or playback
  token.
- For a local `.strm`, Emby item and source `Container`/`Path` must describe the resolved `Media.STRMURL` target and must never expose the `.strm` sidecar as the playable path. Source bitrate comes only from the valid probe document/summary and is omitted when unknown.
- Emby Playing/Progress/Stopped may use a positive request `RunTimeTicks`; when it is absent they use probe `DurationMS`. If both are unknown, the route succeeds without writing history, events, or a fabricated duration.
- Emby `MediaSource.Name` is a version label derived from the real source filename (the resolved STRM target for local STRM). Remove the extension, title/year, season/episode markers, and preserve the remaining technical release markers; use `默认版本` when no label remains.
- A successful single-media manual scrape response must be read after persistence from `MediaView`; returning the raw `Media` row can expose the previous scan title or omit shared metadata fields. A failed or empty refresh is an internal error, not a successful `null` response.
- `MediaView` uses an inner join to `metadata_items`; persisted unresolved media
  remains in the raw `media` table but is absent from metadata-backed display
  reads, and scan hints never replace canonical display identity.
- Pending/error recovery flows must use their raw-media DTO and `Media.ID`
  directly. They must not prefetch `MediaView`, because the unresolved target is
  intentionally absent until a successful apply binds canonical metadata.
- A missing-poster filter selects an item only when its selected poster has no
  valid `ArtworkAsset`. A missing-Chinese-title filter evaluates the final
  displayed title and selects it only when that title contains no Han character.
  Movie filters run before count and pagination. Series/anime filters run after
  complete Series-card aggregation against the representative card, so they
  never select or count individual Episodes. When both filters are enabled,
  both conditions must match.
- The task-center `media_scrape` action may reset only unfinished scrape states;
  it must not expose a bulk option that resets successfully matched media.
- Web PlaybackInfo may locally fall back to a visible raw `Media` when that inner
  join excludes an unresolved row. It fills the non-persistent flat technical
  projection only from `MediaProbeMetadata`; missing probe data stays unknown.
- Duplicate detection and current reports use probe summary size for both
  primary selection and response projection. Missing or failed probe summary
  reads produce size zero.
- Emby movie, Series, Season and Episode item identity and user state always use real `MetadataItem.ID`. Concrete `MediaSource` identity and the last or preferred playable version use `Media.ID`; there is no media-ID identity fallback or virtual Series/Season ID.
- Emby `/Items`, `/Items/Counts`, and search hint totals must count logical metadata items after applying the same visibility, type, and library filters used by the payload. Version collapse must happen before user-visible pagination, so multiple playable versions cannot consume a page or inflate `TotalRecordCount`.
- Physical Emby library membership is derived from a valid, user-visible
  `Media{LibraryID, MetadataID}` relation. Metadata with no Media is retained as
  catalog data but does not belong to a physical library; one Metadata may
  independently belong to multiple physical libraries.
- Count, ordering, offset, and limit must run on grouped Metadata IDs before
  visible `MediaView` versions are batch-loaded. Count and page queries must
  share the same library, type, search, and user-visibility predicates.
- Series and Season scope is derived through visible
  `Episode -> Season -> Series` metadata hierarchy, and Episode versions are
  collapsed by Episode Metadata ID before counts or pagination.
- After a logical item is selected, item detail and PlaybackInfo enumerate all
  sibling Media versions visible to the current user, including versions in
  separate physical libraries. Every sibling query must reapply allowed and
  allowed/hidden library filters.
- After an Emby logical page is selected, People, provider identifiers, and
  visible sibling Media versions must be batch-loaded for the page rather than
  queried inside the item payload loop.
- A library-scoped top-level item uses the requested container as `ParentId`;
  a global projection does not assign the shared Metadata to a physical library.
- Do not cache or reuse a Series/Season payload containing a user-filtered
  Episode collection across users; only user-independent data may use a global
  cache.
- Emby stream endpoints may receive either a metadata item ID or a concrete media source ID. They must resolve the request to a visible playable `Media.ID` before serving unchanged bytes or returning the persisted protocol-neutral redirect.
- Emby clients may call `/SearchHints`, `/Search/Hints`, and their user-scoped or lowercase variants; these routes must project shared metadata titles and IDs, not raw media scan fields.
- Provider identifier projection must return at most one joined row per media and must correlate the reduction to the current `mi.id` and `mi.kind` (for example with `LEFT JOIN LATERAL`). Never aggregate the full active identifier table before joining, and never join the raw one-to-many identifier table into paginated media queries.
- Selected artwork is copied into `DataDir`; remote URLs and source paths are provenance only and are never served as the authoritative runtime image.
- Selected artwork remains entity-owned: Series uses poster/backdrop, Season
  uses poster, and Episode uses still. Read-only Web/Emby projections may use
  the Series poster for a missing Season poster, and an existing Episode
  backdrop followed by the Series backdrop then poster for a missing Episode
  still. This display fallback must not create a
  selection or complete the child artwork checkpoint.
- A missing Episode release date is projected from the nearest lower-numbered
  Episode with a date in the same Season. Never persist the projected date or
  cross a Season boundary; leave it empty when no earlier dated Episode exists.
- Scanner, scraper, metadata edit, and organizer metadata flows only read NFO/poster/fanart/thumb sidecars. They must not create, overwrite, move, or delete them.
- Scan and scrape flows must not move, rename, delete, deduplicate, or reclassify playable media files or change their library/path placement. Only an explicit organize operation may invoke `ReclassifyMisclassifiedMedia` or other filesystem transfer helpers.
- Organizer inputs use `organizeMediaViews`: a canonical Episode with a non-empty
  `SeriesTitle` uses that title for directory/file naming. Its display `Title`
  remains the Episode title; do not alter generic `mediaViewsAsMedia` consumers.
  Single-file organizing, directory lookup and reclassification share this
  conversion. `TestOrganizePipelineRenamesAfterScrape` verifies the final path;
  `TestOrganizeMediaViewsKeepsSeriesNamingSeparateFromEpisodeDisplay` covers
  missing Series titles, Movies and unchanged display data.
- Deleting a library transactionally hard-deletes its `Media`, `LibraryRoot`, and `Library` rows. It preserves shared metadata, metadata-owned user state, identifiers, managed artwork, and all on-disk media files.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Season has no Series parent, negative season, or episode position | Repository validation error and database CHECK rejection |
| Episode has no Season parent, nonzero season position, or non-positive episode | Repository validation error and database CHECK rejection |
| Movie/Series has a parent or season/episode identity | Repository validation error and database CHECK rejection |
| Pending media has no metadata ID | Store SQL `NULL`; keep it out of `MediaView` until enrichment binds metadata |
| Task-center manual recovery targets unresolved media | Open from `MediaScrapeIssue.id/title`; do not require a metadata-backed detail read |
| Web PlaybackInfo targets a visible pending media | Return raw file identity plus probe technical fields; do not add it to normal MediaView lists |
| Duplicate report has no probe summary | Return `size_bytes=0`; do not serialize legacy technical fields or the complete Media model |
| Media has a non-null unknown metadata ID | Database foreign-key rejection |
| Metadata-owned user state has an empty/missing metadata ID | Database NOT NULL/CHECK/foreign-key rejection |
| A new provider ID is unowned | Attach it to the current metadata; replace a stale ID for the same provider/kind |
| Provider identifiers resolve to different metadata rows without explicit merge authority | Reject without changing either metadata graph |
| Explicit provider crosswalk or user-confirmed identity resolves to another metadata | Transactionally merge references and hierarchy, then hard-delete the unreferenced source |
| Provider returns no match | Try read-only local fallback; otherwise set `no_match` |
| Provider request fails | Set `error`; preserve existing canonical data and do not import local fallback |
| Movie-library media contains historical season/episode/series hints | Classify it as Movie; after a successful root scan clear the hints, preserve a Movie link, and unlink/requeue a non-Movie link |
| TV-library filename lacks a case-insensitive `SxxExx` marker | Keep season and episode hints at zero; do not apply compatibility episode patterns |
| A regular scrape path or `provider=all` query resembles an adult code | Do not call `AdultProvider`; continue regular external-ID and provider lookup |
| An explicit adult manual/organize operation has a valid code | Allow `AdultProvider` lookup and persist the selected adult match normally |
| Artwork import fails | Return the error and keep the currently selected managed asset |
| Provider metadata implies a different category/library | Persist metadata and artwork only; preserve the media path and library ID |
| User cannot view the library | Filter in `MediaView` query before pagination or playback response creation |
| `missing_poster=1` and the selected poster asset is absent | Include the final Movie or Series card |
| `missing_chinese_title=1` and the final displayed title contains no Han character | Include the final Movie or Series card |
| Both missing-metadata filters are enabled | Include only cards satisfying both conditions |
| `POST /api/libraries/:id/scrape` or a `repair-rescrape` route is requested | Return route-not-found; do not reset matched media |
| Catalog Metadata has no valid Media | Keep it persisted, but exclude it from physical Emby library results |
| Local media size and nonzero nanosecond mtime both match | Count it as skipped; do not read local/derived metadata, upsert, probe, or emit an update detail |
| Local media fingerprint is missing or differs | Continue the normal scan write and report the fingerprint reason |
| One Metadata has Media in allowed and hidden libraries | Return one logical item and only MediaSources from allowed libraries |
| Many Media versions share one Metadata | Count and paginate the Metadata once; return all visible versions in detail/playback |
| Any library, root, or media hard-delete fails | Roll back the whole library deletion and return an error |
| Playback repair sees multiple visible versions | Schedule each missing version asynchronously; do not reject siblings merely because the playback reservation map is full |
| Local probe queue is full | Wait for queue capacity until the caller context is canceled; release the per-path reservation on cancellation |
| Local STRM source is exposed through Emby | Resolve the target for source path/container/name; never return the `.strm` text path as a playable source |
| Probe JSON is malformed, outdated, or fails structural validation | Treat technical facts as unknown and schedule lazy repair without copying legacy `media` values |
| Probe source changes while ffprobe is running | Reject the result transactionally; update neither typed summary nor complete JSON |
| STRM URL matches a valid track probe mapping and the local file is readable | Probe the mapped local file immediately and persist its local size |
| Track probe mapping is invalid, unmatched, escapes its local prefix, or maps to an unavailable/non-regular file | Preserve the normalized URL and use the existing delayed remote probe |
| Track probe mapping changes while ffprobe is running | Reject the stale result; update neither typed summary nor complete JSON |
| FFprobe is missing, times out, or returns invalid output | Return a probe error, preserve the previous valid document, and never start FFmpeg |
| A probe task is already active when global backfill is requested | Return `409`; do not create another global execution |
| Global backfill `limit` is negative or not an integer | Return `400`; do not create a task execution |
| Global backfill `library_id` does not resolve | Return `404`; do not create a task execution |
| Explicit audio/subtitle selection is invalid | Return bad request; never silently map a different track |
| Embedded or sidecar subtitle index no longer resolves | Return not found and require a refreshed PlaybackInfo response |
| Embedded subtitle is selected | Keep it as a source stream without an extraction URL or server-side conversion |
| External sidecar subtitle is selected | Return only the controlled token-aware delivery URL for the revalidated sidecar index |
| External HTTP/HTTPS target matches no redirect prefix | Return the original URL unchanged in HTTP 302; append no internal token or `media_id` |
| STRM or post-mapping URL matches a redirect prefix | Return the first valid 3xx `Location` in HTTP 302 and cache it for one hour by source URL plus exact `User-Agent` |
| Redirect pre-resolution fails or returns an invalid target | Cache nothing and return the original URL in HTTP 302 |
| Client cannot decode the original source | Offer no HLS, remux, or transcode URL; let direct playback fail clearly |
| PlaybackInfo resolves a visible concrete media | Return its non-null `DateCreated` at the response top level; do not rely only on item/source dates |
| Emby progress omits runtime and probe duration exists | Use probe `DurationMS` for history, completion, and event writes |
| Emby progress omits runtime and probe duration is unknown | Return success without writing history, events, or a fabricated duration |

### 5. Good / Base / Bad Cases

- Good: two paths with the same TMDb movie identity link to one movie metadata row, share Emby favorite/played/resume state, and keep separate media IDs, paths, codecs, sizes, and media sources.
- Good: one movie present in two physical libraries appears once in each scoped
  list and once in a global list; an unrestricted detail response includes both
  concrete MediaSources.
- Good: two files for the same series/season/episode link to one Episode whose parent is a real Season whose parent is the Series; only the Series can be favorited, not the Season or Episode.
- Good: a Douban-only movie creates/reuses metadata without a TMDb ID; a later explicit TMDb crosswalk either attaches the unowned ID or safely merges into its existing owner.
- Base: an unresolved file has `metadata_id = NULL` and remains absent from
  `MediaView` until provider or eligible local persistence binds canonical
  metadata.
- Good: task-center manual recovery opens from the issue DTO, then search and
  apply submit its real `Media.ID`.
- Bad: opening manual recovery calls `GET /api/media/:id` first and fails before
  the user can select a provider match.
- Good: Web PlaybackInfo can still return a stream URL for that unresolved file,
  while its duration, size, container, dimensions, and codecs come only from probe.
- Bad: change the global metadata join to `LEFT JOIN` to fix Web PlaybackInfo,
  because that makes unresolved scan hints visible to every MediaView consumer.
- Good: scanner binds an existing exact canonical identity without updating its
  title, details, source, or identifiers.
- Bad: scanner creates or overwrites `source=local` metadata before provider
  lookup, or treating `Media.ID` as an item identity fallback.
- Bad: copying provider title, genres or artwork URL into each `Media` row.
- Bad: applying NFO title or artwork after a successful provider match.
- Bad: copying scanner, TMDb runtime, or legacy `media` technical columns into
  `media_probe_metadata` as if they were ffprobe facts.
- Good: `/media/STRM-115/Movie.mkv` follows the regular provider chain without a JavDB/JavBus request.
- Good: explicit `provider=adult`, `source=adult`, or `mediaType=adult` can still request adult metadata.
- Bad: treating `provider=all`, a parent directory, or a scan title that resembles a code as consent to contact an adult provider.
- Bad: joining all `MetadataIdentifier` rows directly and then applying `COUNT`, `OFFSET`, or `LIMIT`.
- Bad: paginating Media rows and collapsing versions afterward, because early
  versions can consume the page and hide later logical works.
- Good: deleting a local library physically removes its library/root/media rows while the referenced metadata and user state remain.
- Good: repeated scans of an unchanged `.strm` skip it without rebuilding a
  cleared `local_metadata_hint` or starting another automatic scrape.
- Base: an NFO or directory hint changes while the media file fingerprint does
  not; the media row and its existing hint remain untouched until the media
  file itself changes.
- Bad: treating local or path-derived metadata differences as file updates,
  which creates an update/scrape/hint-clear loop for unchanged media.
- Bad: using GORM's scoped `Delete` for a library or its media and leaving rows in the recycle bin.
- Good: a scan or scrape changes title, identifiers, artwork and scrape status while the playable file path and library ID remain unchanged.
- Good: a Series whose representative card has no poster and an English-only
  final title appears once when both missing-metadata filters are enabled.
- Bad: filter Episodes before Series aggregation, combine the two filters with
  OR, or reintroduce a library-wide endpoint that resets matched media.
- Bad: calling `ReclassifyMisclassifiedMedia` after a scrape and silently moving or deleting a local media/STRM file.
- Good: a two-version local STRM item schedules both target files, persists each document and typed summary to its probe row, and exposes matching target container/path/name/bitrate.
- Good: a signed remote STRM URL maps by its decoded path to a mounted local
  file; the query signature is ignored and the local probe starts immediately.
- Good: a historical STRM target containing raw `#` in directory and file names
  is normalized to `%23` before scan persistence, probe mapping, direct redirect,
  and Emby container projection; its signed query remains unchanged.
- Base: a correctly encoded `%23` target passes through without double encoding.
- Base: no track probe mapping matches, or the mounted file is temporarily
  unavailable; the existing delayed remote probe remains usable.
- Bad: reuse or reverse `playback.path_mappings`, apply a track mapping to a
  non-STRM media row, or build a local filename from URL query values.
- Bad: parse a raw-`#` STRM target before compatibility normalization, or accept
  a mapped directory merely because `os.Stat` succeeds.
- Good: task-center global backfill repairs missing documents across libraries,
  skips valid documents while hard-deleted media is absent, and keeps
  `total = completed + skipped + failed`.
- Good: a limit of 500 skips any number of valid documents and attempts at most
  500 missing, outdated, or invalid documents; a later run continues to the
  next remaining candidates.
- Good: selecting one library with no limit repairs every missing document in
  that library without probing media from another library.
- Bad: limiting sibling scheduling by the playback reservation map or deriving a source label from the `.strm` sidecar/title, which leaves versions unprobed or displays `strm`/title metadata.
- Good: PlaybackInfo for a concrete media returns the same `Media.CreatedAt` at the top level and on that media source; bad: adding the field only to `MediaSource` while the client reads `PlaybackInfo.DateCreated`.
- Good: an unmatched external HTTPS STRM target keeps its signed query
  byte-for-byte in the 302 `Location` while receiving no internal token or
  media ID.
- Good: a configured media-gateway prefix resolves only the first relative 302
  `Location`; a repeated request with the same source URL and `User-Agent` uses
  the one-hour cache while another `User-Agent` resolves independently.
- Base: a client cannot decode the original codec and reports a direct-play failure; the server does not create an alternate stream.
- Bad: advertising HLS or a transcoding URL that the direct-only server cannot and must not serve.

### 6. Tests Required

- Schema: create `Series -> Season 0 -> Episode`; allow a NULL media metadata ID;
  reject invalid parent kinds, duplicate season/episode identities, unknown
  non-null foreign keys, and deletion of referenced metadata.
- Scanner identity: assert exact movie and existing episode identities bind
  without mutating canonical metadata; assert unresolved scans create no local
  metadata row and remain absent from `MediaView`.
- Scanner classification: assert movie libraries ignore episodic fields and use
  movie provider routes; TV libraries accept only the bounded `SxxExx` marker;
  anime/variety retain compatibility parsing. Assert a successful movie-root
  rescan reconciles dirty fields and reports the count while preserving correct
  Movie links and requeueing incorrect or failed bindings.
- Scanner fingerprint: assert unchanged `.strm` and ordinary files skip without
  touching `updated_at`; NFO-only changes still skip; size/mtime changes update
  with the matching reason; a zero legacy mtime is backfilled once.
- Identity: allow equal external IDs across provider or entity kind; deduplicate equal canonical identities.
- Identity: replace a stale unowned identifier for the same provider/kind, preserve other provider identifiers, and reject an occupied identifier unless merge is explicitly authorized.
- Merge: move multiple media versions, favorites, playlists and history; recursively merge Series children; assert duplicate user state is resolved and source metadata is physically gone.
- Query: add multiple identifiers for one metadata/provider/kind and assert media count, page length, and order remain unchanged; assert the generated query correlates identifier reduction to the current metadata and contains no global identifier `GROUP BY`.
- Visibility: adult-library restrictions must hide list, search, detail, and PlaybackInfo results before pagination/response mapping.
- Library missing metadata: assert missing-poster, missing-Chinese-title, and
  combined filtering keep rows and total consistent; assert Series/anime are
  filtered and counted only after aggregation.
- Scrape routes: assert the library-wide scrape and repair-rescrape routes are
  absent, while task-center scraping resets only unfinished rows.
- Web PlaybackInfo: assert a visible unresolved media returns `200`, probe summary
  fields fill the non-persistent flat projection, and missing probe data returns zeros.
- Duplicate report: assert Detect and Current use probe sizes and JSON exposes
  only the `DuplicateMedia` whitelist.
- Playback/Emby: assert item display `Name` comes from shared metadata while each `MediaSource.Name` comes from the real source filename with title, year, season/episode markers, and extension removed; source path comes from `Media`, while technical container/codecs come from the probe row.
- Playback/Emby: assert multiple media versions expose one metadata item ID, share user state, and retain distinct media source IDs.
- Playback/Emby: count request reads and assert metadata/concrete PlaybackInfo
  performs one sibling resolution, one batch probe read, no per-media probe or
  media reload, and still returns the same selected indexes and sources.
- Playback/Emby: JSON-round-trip PlaybackInfo and assert top-level `DateCreated` is present, non-null, parseable, and equal to the selected concrete media's creation time.
- Playback/Emby: assert `/Videos/{metadata_id}/{stream,original}` resolves to a concrete visible media source ID before serving bytes, and assert all HLS/transcode route variants are absent.
- Playback/Emby: assert local STRM scan, manual reprobe, and missing-metadata PlaybackInfo use the real target, persist target size/track facts only in the probe row, deduplicate and bound background probes, and reject stale target results.
- Probe storage: assert safe typed JSON round-trips every video/audio/subtitle absolute index and disposition while excluding input URLs, credentials, arbitrary tags, attachments, and structurally invalid streams; hard media deletion must cascade to the one-to-one probe row.
- Schema migration: assert `media.duration_sec`, `size_bytes`, `container`, `width`, `height`, `video_codec`, and `audio_codec` are absent after migration and remain absent after another `AutoMigrate`; `scan_file_size_bytes` and `scan_file_mtime_ns` must remain.
- Media detail projection: assert `GET /api/media/:id` returns only whitelisted track fields with absolute indexes, omits malformed/missing probe data, and leaves paginated list/search payloads without `tracks` or probe loads.
- Backfill: cover more than one keyset page, cross-library global execution,
  soft-deleted exclusion, valid-record skips, version/corruption repair,
  failure accounting, request-independent context, duplicate-run rejection,
  all-library versus selected-library scope, limited probe-attempt accounting
  where skips do not consume the limit, and
  `total = completed + skipped + failed` for completed runs.
- Playback selection: cover GET and POST omitted/zero/negative values, explicit invalid audio/subtitle indexes, default-audio choice, two simultaneous selections, source-version selection, and stop/restart isolation without conversion.
- Subtitles: add/remove sidecars between PlaybackInfo calls, assert embedded streams have no extraction URL, deliver external sidecars by controlled index, and reject stale or wrong-type indexes without exposing backing paths or credentials.
- Direct source delivery: cover local GET/HEAD/Range/206 and invalid ranges;
  unchanged unmatched external targets; prefix parsing; STRM and post-mapping
  resolution; relative first-hop locations; one-hour expiry; exact
  `User-Agent` isolation; failed-resolution passthrough without caching;
  upstream header isolation; sanitized logs; and media-scoped tokens only on
  internal `/api/stream/:id` redirects.
- Probe execution: put an `ffmpeg` sentinel first on `PATH`; probe success/failure, PlaybackInfo, original playback, and subtitle delivery must never execute it.
- Probe execution: assert remote delay samples are whole seconds in the inclusive
  two-to-five-second range and local probing has no delay.
- Probe path mapping: assert schema exposure, scheme/host/path-segment matching,
  longest-prefix selection, decoded path joining, raw-`#` normalization without
  `%23` double encoding, query exclusion, traversal rejection, STRM-only gating,
  unavailable/non-regular-file remote fallback, local runner selection, and
  source rejection after a mapping change.
- Probe execution failures: assert stderr is bounded to one line, keeps a useful
  reason and the wrapped execution error, and omits local source paths, remote
  URLs, and signed query values.
- Playback/Emby: assert all visible sibling versions are scheduled, duplicate media IDs are not probed concurrently, bitrate is omitted when the probe value is missing, and target-derived source name/container/path never expose the STRM sidecar.
- Scanner queue: assert a full local probe queue waits for capacity and a canceled context releases the reserved path without enqueuing a stale task.
- Playback/Emby: assert `/Items` totals, `/Items/Counts`, and `/SearchHints` count shared metadata once while still exposing every concrete version as a `MediaSource`.
- Playback/Emby: assert Series and Season IDs are real metadata IDs, Episode parent IDs follow the stored hierarchy, and no virtual or media-ID fallback is emitted.
- Playback/Emby: place one Metadata in two independent libraries and assert it
  is listed once per scoped library and once globally, catalog-only Metadata is
  absent, and hidden-library sibling MediaSources are excluded.
- Playback/Emby: assert Latest selects its logical Metadata page before loading
  versions, so any number of versions for the first work cannot displace the
  next work.
- Playback/Emby: assert Series and Season Episode counts collapse multiple Media
  versions of one Episode Metadata to one logical Episode.
- Scrape state: test provider match, definitive no-match with local fallback, and provider error without fallback.
- Automatic identity: assert zero provider requests for title-only media and
  organize input; direct-ID success, failure without name fallback, explicit
  second-provider ID fallback, TMDb 404, and mismatched returned-ID rejection.
- TMDb request reuse: known movie/Series IDs make one detail request and retain
  languages, countries, and genres; search-only matches make one search plus one
  extended-details request and retain the same fields.
- Scrape provider boundary: assert regular enrichment, `provider=all` manual search, and non-adult organize make zero adult-provider requests; retain positive coverage for explicit adult manual search and adult organize.
- Manual apply API: assert the response contains the newly persisted shared title while the media path and library ID remain unchanged.
- Task-center manual recovery: assert an unresolved issue opens without a media
  detail request and both search and apply submit the issue's `Media.ID`.
- Artwork: delete/ignore cache and remote source after import; `/api/artwork/:assetID` must still serve the DataDir copy.
- Sidecars: snapshot NFO/poster/fanart/thumb before scan/scrape/organize and assert content and paths are unchanged afterward.
- Media files: snapshot playable paths before every scrape entrypoint and assert file existence, path, library ID and bytes are unchanged afterward.
- Library deletion: query with `Unscoped` and assert library/root/media rows are gone, metadata remains, and a failed child delete rolls back all rows.
- Run AutoMigrate plus MediaView query/constraint integration tests against an isolated PostgreSQL test schema.

### 7. Wrong vs Correct

#### Wrong

```go
repo.DB.Table("media AS m").
    Joins("LEFT JOIN metadata_identifiers AS mid ON mid.metadata_id = m.metadata_id").
    Offset(offset).Limit(limit)
```

This expands a media row when an entity has multiple identifiers and corrupts count and pagination.

#### Correct

```go
repo.DB.Table("media AS m").
    Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id").
    Joins(`LEFT JOIN LATERAL (
        SELECT MIN(CASE WHEN mid.provider = 'tmdb' THEN mid.external_id END) AS tmdb_external_id
        FROM metadata_identifiers AS mid
        WHERE mid.metadata_id = mi.id AND mid.entity_kind = mi.kind
    ) AS ids ON TRUE`).
    Offset(offset).Limit(limit)
```

The inner metadata join enforces display eligibility: raw unresolved media is
persisted but intentionally absent until `metadata_id` is filled. The identifier
projection must preserve one output row per media before filtering, counting,
sorting, and pagination.

Web PlaybackInfo is the narrow exception: fall back in that handler to raw file
identity and probe summary. Do not weaken the shared join or return raw technical
columns. Duplicate reports likewise return a dedicated whitelist DTO rather than
`model.Media`.

Task-center manual recovery is another explicit raw-media boundary:

```typescript
// Wrong: unresolved media is intentionally absent from MediaView.
const media = await mediaAPI.get(issue.id)
setManualTarget(media)

// Correct: the issue DTO already carries the required raw Media identity.
setManualTarget(issue)
```

Emby logical pagination must also happen before version loading:

```go
// Wrong: versions consume the physical page before logical deduplication.
views := repo.ListByLibrariesFiltered(ctx, libraryIDs, offset, limit, filter)
items := preferredMetadataViews(views)

// Correct: page grouped Metadata IDs, then load visible versions for that page.
items, total, err := metadataPage(ctx, scopedQuery, userID, order, offset, limit)
```

Scanner persistence must not manufacture local canonical metadata:

```go
// Wrong: turns an unresolved scan hint into canonical metadata before lookup.
metadata, err := metadataRepo.UpsertCanonical(ctx, localItem, identifiers, "")
media.MetadataID = metadata.ID

// Correct: bind only an exact existing canonical result; otherwise persist NULL.
metadata, err := findExistingMediaMetadata(ctx, media)
if metadata != nil {
    media.MetadataID = metadata.ID
    media.ScrapeStatus = "matched"
}
```

Scanner change detection must stop at the media file fingerprint:

```go
// Wrong: unchanged files update because a transient scan hint was cleared.
skip := sameSize && sameMTime && !localMetadataChanged

// Correct: derived metadata is not read until the media file itself changed.
skip := storedMTimeNS != 0 && sameSize && sameMTime
```

For library deletion, scoped GORM deletion is also incorrect:

```go
// Wrong: leaves library and media rows soft-deleted.
tx.Where("library_id = ?", id).Delete(&model.Media{})

// Correct: permanently removes file rows without deleting shared metadata.
tx.Unscoped().Where("library_id = ?", id).Delete(&model.Media{})
```

For media detail tracks, returning the persisted probe document directly is also incorrect:

```go
// Wrong: exposes the storage-shaped probe document instead of the response whitelist.
return c.JSON(http.StatusOK, probeDocument)

// Correct: attach the typed whitelist only to the single-media detail view.
media, err := svc.Media.GetMedia(ctx, id)
return c.JSON(http.StatusOK, media)
```

Scrape completion must not trigger organization:

```go
// Wrong: metadata refresh can move or delete the playable file.
result, err := scraper.EnrichLibraryDetailedWithOptions(ctx, libraryID, options)
if err == nil && result.Processed > 0 {
    _, err = organizer.ReclassifyMisclassifiedMedia(ctx, MediaCategoryReclassifyOptions{LibraryIDs: []string{libraryID}})
}

// Correct: scraping owns metadata and managed artwork only.
_, err := scraper.EnrichLibraryDetailedWithOptions(ctx, libraryID, options)
```

Library-wide repair must not be exposed as a forced matched reset:

```go
// Wrong: a page action can silently queue every successfully matched item again.
authed.POST("/libraries/:id/scrape", scrapeLibraryHandler(svc))

// Correct: normal scans and task-center media_scrape own automatic/retry work;
// the forced library-wide route remains absent.
```

Adult scraping must also remain explicit:

```go
// Wrong: a regular scrape infers adult intent from a path such as STRM-115.
if code := AdultCodeFromMediaPath(media.Path); code != "" {
    match, err := scraper.adult.Search(ctx, code)
}

// Correct: only an explicit adult entrypoint calls the adult provider.
if _, explicitAdult := providers["adult"]; explicitAdult {
    matches := scraper.manualAdultMatches(ctx, media, query)
}
```

For STRM playback repair, reservation and probe concurrency are separate concerns:

```go
// Wrong: the playback reservation map becomes a second global probe limiter.
if len(e.trackProbeInFlight) >= normalizeFFprobeMaxConcurrent(limit) {
    return false
}

// Correct: reserve only by media ID; FFprobeService owns concurrency.
if _, busy := e.trackProbeInFlight[mediaID]; busy {
    return false
}
```

Global backfill must also reuse the shared probe path:

```go
// Wrong: the task-center action owns another ffprobe parser or persistence path.
probe := ffprobeDirectly(media.Path)

// Correct: global selection changes only scope; each candidate uses the shared owner.
probe, err := mediaProbe.ProbeMedia(ctx, media.ID)
```

Legacy STRM URL compatibility must precede every path parse or redirect:

```go
// Wrong: url.Parse treats the first raw # and everything after it as a fragment.
target, err := url.Parse(media.STRMURL)

// Correct: normalize once through the shared STRM rule, then parse or redirect.
target, err := url.Parse(normalizeSTRMHTTPURL(media.STRMURL))
```

Track probe source validation must not open a second database connection from
inside the media persistence transaction:

```go
// Wrong: a single-connection test pool can deadlock while tx owns the connection.
tx.Transaction(func(tx *gorm.DB) error {
    mappings, _ := repo.Setting.Get(ctx, FFprobePathMappingsSettingKey)
    return validateProbeSource(tx, mappings)
})

// Correct: snapshot the latest mapping after ffprobe, then open the transaction.
mappings := mediaProbe.probePathMappings(ctx)
db.Transaction(func(tx *gorm.DB) error {
    return validateProbeSource(tx, mappings)
})
```

Playback compatibility fields must be placed at the exact response layer:

```go
// Wrong: a nested source date does not satisfy clients reading the response top level.
return map[string]any{"MediaSources": sources}

// Correct: keep the source fields and expose the selected media date at PlaybackInfo top level.
return map[string]any{"MediaSources": sources, "DateCreated": formatEmbyDateTime(media.CreatedAt)}
```

Direct-only capability projection must also match the routes the server owns:

```go
// Wrong: advertise a conversion route that does not exist.
source["SupportsTranscoding"] = true
source["TranscodingUrl"] = "/Videos/" + media.ID + "/master.m3u8"

// Correct: expose only unchanged-source delivery.
source["SupportsTranscoding"] = false
delete(source, "TranscodingUrl")
source["DirectStreamUrl"] = "/Videos/" + media.ID + "/stream." + container
```

## Scenario: Emby Multipart Media

### 1. Scope / Trigger

- Apply when changing local scan naming, media-version grouping, Emby item or
  PlaybackInfo projection, direct playback identity, or playback progress for
  files split into multiple physical parts.

### 2. Signatures

- Persistent file relation: `Media{PartGroupKey string, PartIndex int}`.
- Player discovery: `GET /Videos/:id/AdditionalParts` plus `/emby` and lowercase
  route variants.
- Item projection: `PartCount` is present only for an active group with more
  than one visible Part.

### 3. Contracts

- One `Media` row continues to represent one independently playable physical
  file. Part count and the primary Part are derived; no parent ID or count is
  persisted.
- Only a trailing `cd|dvd|part|pt|disc|disk` marker followed by a positive
  number or `a-d` is a candidate. A group requires the same library, real
  case-sensitive directory, normalized base name and marker type, at least two
  members, and unique indexes. A singleton remains ordinary media.
- Version collections collapse each active Part group to its lowest visible
  `PartIndex` before choosing or exposing versions. A concrete request for a
  later Part returns only that physical source; a logical item or primary Part
  may still enumerate all version primaries.
- AdditionalParts selects the current/preferred version, returns only members
  after its primary Part in index order, and uses each member's concrete
  `Media.ID`, duration, streams and `DirectStreamUrl`.
- Visibility filtering applies before PartCount and AdditionalParts output.
  Playback progress keeps the request `MediaSourceId`; a Part DTO reuses a
  stored position only when that history row belongs to the same concrete ID.
- Full scans, root scans, path ingest and file removal reconcile affected
  groups. Removing all but one member clears the remaining Part fields.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| One candidate file | Keep `PartGroupKey=''`, `PartIndex=0`, and the unstripped scan title |
| Duplicate index in one candidate group | Reject the whole group |
| Same spelling under case-distinct Linux directories | Keep separate groups |
| Logical item has several versions with Parts | Expose one MediaSource per version primary |
| AdditionalParts target has no active visible group | Return `{Items: [], TotalRecordCount: 0}` |
| PlaybackInfo targets a later Part ID | Return exactly that concrete source |
| Progress names a visible later Part as `MediaSourceId` | Persist that Part's `Media.ID` without cross-Part position conversion |

### 5. Good / Base / Bad Cases

- Good: 1080p and 2160p each have Part 1/2; the item exposes two MediaSources,
  `PartCount=2`, and AdditionalParts for the preferred version returns its Part 2.
- Base: `Movie Part 1.mkv` exists alone and behaves like an ordinary file.
- Bad: expose Part 2 as another version, merge files from `/Media` and `/media`,
  or copy Part 1's position into Part 2's DTO.

### 6. Tests Required

- Pure parsing covers marker variants, separators, case, numbers, `a-d`, bad
  boundaries, duplicate indexes, and case-distinct directory group keys.
- Scanner tests cover grouping, singleton restoration, nested root scans,
  deletion reconciliation, and unchanged movie/episode identity.
- Emby service tests cover multi-version x multipart MediaSources, PartCount,
  ordered AdditionalParts, concrete URLs, logical versus concrete PlaybackInfo,
  and concrete progress identity.
- Handler tests cover `/emby`, unprefixed and lowercase routes plus token
  attachment to each additional Part's direct stream URL.

### 7. Wrong vs Correct

```go
// Wrong: every physical Part becomes an alternate version.
sources := mediaSourcesForViews(ctx, visibleViews, true, false)

// Correct: version projection keeps only the primary member of each Part group.
sources := mediaSourcesForViews(ctx, collapseMediaPartViews(visibleViews), true, false)
```

```go
// Wrong: a request for Part 2 expands back to all versions.
return mediaVersionSiblings(ctx, part2, userID)

// Correct: a later concrete Part remains one independently playable source.
return []model.MediaView{*part2}
```

## Scenario: Top-Level Metadata Search

### 1. Scope / Trigger

- Apply when changing Web search, Emby `SearchTerm`, OpenSearch mappings,
  searchable-media eligibility, or Media/Metadata index synchronization.

### 2. Signatures

- Candidate boundary:
  `SearchMetadataIDs(ctx, query, offset, limit, MetadataSearchFilter) -> metadata IDs, total`.
- Non-empty search candidate cap: `maxMetadataSearchCandidates = 100`.
- OpenSearch document ID is the top-level `MetadataItem.ID`; its fields are
  `id`, `kind`, `title`, `original_name`, `overview`, `genres`, and
  derived `library_ids`, plus ordinary `person_ids` and lowercase `person_names`.
- The active OpenSearch alias is `mediastation_metadata`; versioned concrete
  indexes are built before an atomic alias switch. Ordinary schema version 2 adds
  person fields; HongGuo retains its independent version and mapping.

### 3. Contracts

- Search candidates, totals, offsets, and limits use top-level Movie/Series
  Metadata. Media IDs, paths, scan titles, streams, and playback-version facts
  never enter the search contract or index.
- A Movie is eligible only while at least one active Media directly references
  it. A Series is eligible only through an active
  `Series -> Season -> Episode -> Media` chain. Season, Episode, unresolved
  Media, and Metadata without playable content are not candidates.
- `library_ids` is the only Media-derived document field. Restricted searches
  require an intersection with visible libraries; an explicitly restricted
  empty library set returns no candidates.
- Web searches `title`, `original_name`, `overview`, and `genres`. Emby searches
  `title` and `original_name`, with an additional ordinary person-name/
  original-name literal substring expansion to credited Movie/Series. Every
  normalized title term must match, and
  PostgreSQL LIKE metacharacters are escaped literally.
- Non-empty title queries always ask OpenSearch or PostgreSQL for candidates at
  `offset=0`, capped at 100. PostgreSQL revalidates the candidate IDs and loads
  current title, original name, overview, genres, and year; one shared Go
  comparator sorts the complete candidate set before applying the caller's
  offset/limit. The returned total is the revalidated candidate count and is
  therefore at most 100 for title-only search. Ordinary person-work expansion
  additionally retains every revalidated related work before pagination.
  Empty-query browsing keeps database pagination and its
  uncapped logical total.
- Search terms use AND between term groups and OR only between one term's
  equivalent forms. Standard decimal integers and canonical Chinese numbers
  from 0 through 100 expand both ways; numeric forms are atomic. Other Han text
  requires every analyzed Han token in one field, so `死神` cannot match a title
  containing only `死` or only `神`.
- Emby compatibility normalizes a `SearchTerm` made of exactly one Unicode
  character followed by `%` to that character because Yamby/Emby players append
  this wildcard automatically. Every other `%` remains a literal search term.
- Final rank tiers are exact title/original match, ordered full containment in
  one title/original field, then other all-token matches. Exact matches sort by
  year and Metadata ID. The other tiers sort by shared field coverage, match
  position/span, the last valid 0–100 title number descending, year descending,
  and Metadata ID. OpenSearch `_score` only selects its finite candidate set;
  it is not a cross-backend final score.
- Web search pages and suggestions preserve the ranked Metadata order after
  loading one playable representative per Metadata. Do not pass these results
  through `groupMediaVersions`: its creation-time sort overwrites relevance.
  Regression coverage must make title matches older than overview-only matches.
- Representative loading must be candidate-driven: ordinary candidates expand
  through `logicalMetadataCandidates`, with a lateral media lookup (`OFFSET 0`
  preserves the lookup boundary). Keep the existing movie / series-episode
  eligibility and latest-file ordering; do not admit direct Series/Season files.
- NFO search materializes keyword-matched items before checking file visibility.
  Batch representative loading uses the same binding / ancestor / library
  filters as `NFOItemViews`, orders by season, episode and path, and projects the
  requested item through `nfoPresentation`. Do not loop over candidates calling
  `NFOItemViews` and `NFOPresentation` independently.
- SearchHints sets `Fields=BasicSyncInfo` before reusing Items. Its output fields,
  total and ordering remain identical to projecting full Items; People,
  ProviderIds and MediaSources are not needed for the hints envelope.
- Emby `SearchTerm` treats supported `IncludeItemTypes` as an OR set. Movie and
  Series keep the existing title path and additionally expand ordinary person
  names to credited works. Explicit Person candidates come from PostgreSQL
  name/original-name search. Person keyword searches exclude HongGuo people;
  no-keyword Persons browsing retains existing source behavior. NFO-only and
  HuangGuo AI people are not included in this expansion.
  Unsupported types are ignored beside a supported type and return an empty
  envelope when requested alone. Mixed candidates use the shared rank, cap of
  100 for non-related candidates, and in-memory pagination. No-term hierarchy
  browsing remains unchanged; ordinary PersonIds work pages use the full
  person-work candidate path and preserve file/state qualification.
- OpenSearch hits are revalidated through PostgreSQL before ranking and response
  mapping. Stale, invisible, unplayable, or no-longer-matching candidates are
  omitted before the capped total and page are computed.
- Media create, delete, rebind, and library move refresh both old and new
  top-level IDs after commit. Metadata content, parent, and merge changes do the
  same. A full rebuild replays dirty IDs before switching the alias; it does not
  delete the retired Media index. Person credit replacement and person-name
  translation refresh affected work IDs after commit. Shared-name changes
  refresh all related works; unchanged credit snapshots need not refresh all
  works sharing a person. Refresh failures disable person-index recall until
  a successful rebuild; dirty identities are replayed before alias activation.

### Ordinary Person Work Search

- Items with SearchTerm or PersonIds support only Movie, Series and Person
  query types. Remove unsupported types before source/hierarchy dispatch; an
  unsupported-only query returns an empty envelope without database access.
  In particular, Episode person queries are compatibility no-ops, not an
  invitation to expand person search to episodes. Mixed types retain the
  supported subset. Normal episode browsing without these filters is unchanged.
  Cover this boundary with `TestEmbyUnsupportedPersonAndSearchTypesSkipDatabase`
  and the mixed-query cases in `TestEmbyOrdinaryPersonWorksCompletePagesAndRevalidation`.
- `PersonWorkSearchBackend.SearchPersonWorkIDs` queries the ordinary work alias
  using exact `person_ids` or escaped lowercase keyword `person_names` substrings.
  Read lightweight IDs in 500-ID keyset batches, not a capped first page or a
  deep `from` window; timeout/failed-shard responses must fall back to PostgreSQL.
- Movie credits belong to the Movie. Series person search unions Series and
  Season credits, not Episode credits. Same names never merge provider identities.
- `PersonWorkQuery` revalidates live people, current credits, kinds, visible
  libraries and playable files. Person work qualification includes files directly
  attached to Series or Season. Index documents include those library memberships;
  ordinary title-only search retains its previous episode-chain qualification.
- Emby PersonIds Movie/Series pages reuse stable sort, optional counts, state
  eligibility and page-only hydration. A direct-file-only Series gets a page-local
  summary even when the normal episode-summary loader returns no group.
- Regression coverage: `TestOpenSearchPersonWorkCursorAndLiteralName`,
  `TestOpenSearchPersonWorkRejectsIncompleteResults`,
  `TestPersonSearchDocumentsFollowCreditsAndNames`,
  `TestPersonSearchDocumentsIncludeDirectSeriesFiles`,
  `TestEmbyOrdinaryPersonWorksCompletePagesAndRevalidation`, and
  `TestEmbyPersonWorksPreserveDirectSeriesAndSeasonFiles`.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| OpenSearch alias is missing, incompatible, or the request fails | Use PostgreSQL Metadata search |
| Restricted visibility resolves to no library | Return empty IDs, items, and total |
| Movie loses its last Media | Delete its search document |
| Series loses all playable Episode and direct Series/Season Media | Delete its search document |
| Media changes Metadata or library | Refresh old and new top-level projections after commit |
| OpenSearch returns a stale/invisible ID | Omit it during PostgreSQL revalidation |
| Title-only search has more than 100 backend matches | Rank and expose only the selected 100 candidates; total is capped at 100 |
| Ordinary person matches more than 100 works | Retain all revalidated related works and paginate the combined ranked set |
| Search is `44` or `四十四` | Match both complete numeric forms; do not match `四十` as a numeric alias |
| Emby player sends a one-character term such as `古%` | Normalize it to `古`; keep standalone or multi-character `%` terms literal |
| Search requests `Person,Movie` | Return matching Person, title-matched Movies and ordinary credited Movies in one ranked page |
| Search requests `Person,MusicAlbum` | Ignore unsupported MusicAlbum and return matching Person items |
| Requested offset is outside the candidate set | Return an empty page with the qualified candidate total |
| Search requests only Season/Episode | Return an empty Emby search envelope |

### 5. Good / Base / Bad Cases

- Good: two 1080p/4K Media versions produce one Movie hit and consume one page
  slot; one Series with many Episodes also produces one Series hit.
- Good: `死神2` outranks a less relevant `新死神10`; equal-relevance
  `死神10`, `死神9`, and `死神2` use descending title numbers.
- Good: Yamby/Emby sends `古%` after one-character input and receives the same
  results as `古` without changing other literal `%` searches.
- Good: `Person,Movie` search for an exact person name ranks that Person ahead
  of a containing movie title, followed by credited works whose titles do not
  contain the name.
- Base: OpenSearch is unavailable; PostgreSQL returns the same Metadata-grained
  eligibility and applies the same Go ranking to its finite candidate set.
- Bad: index one document per Media and collapse versions after pagination.
- Bad: page OpenSearch/PostgreSQL first and reorder only the returned page, or
  compare OpenSearch `_score` with a separate PostgreSQL score.
- Bad: make a catalog-only Metadata searchable or match a Media path/scan title.

### 6. Tests Required

- OpenSearch HTTP mocks assert the exact mapping, Metadata `_id`, Web/Emby field
  sets, term AND, numeric OR/phrase, fixed 0/100 candidates, stable score/ID
  selection, `kind`, `library_ids`, restricted-empty, readiness, bulk, delete,
  and alias-switch payloads contain no Media fields.
- PostgreSQL tests assert Movie multi-version and Series multi-Episode collapse,
  no-Media exclusion, unresolved-Media exclusion, library visibility, adult-library restrictions,
  token-group filtering, capped total, ordering, and in-memory pagination.
- `search_loading_test.go` compares NFO batch presentation and candidate
  visibility against the single-item path, including hidden/adult libraries,
  missing artwork/title and keyword matching; the batch executes one query.
  Query-count callbacks must ignore GORM DryRun subquery construction.
  `emby_source_search_test.go` compares hints against full Items and asserts
  fewer executed relation queries. Use real EXPLAIN plans, not SQL shape alone,
  to validate selective media reads.
- Pure ranking tests assert canonical 0–100 conversion, whole numeric runs,
  strict tier order, relevance before title number, number/year/ID tie-breaks,
  irrelevant single-token exclusion, and safe page boundaries.
- Synchronization tests assert create, last-Media delete, rebind, library move,
  Metadata parent change, and graph merge refresh every affected top-level ID.
- Emby tests assert Movie/Series/Person OR results, unsupported-type ignoring,
  ordinary credit expansion, >100 related-work paging, no-term browse stability, Season/Episode empty search,
  ParentId behavior, multi-term AND, one-character `%` suffix normalization,
  other literal `\\`/`%`/`_` terms, SearchHints, and logical total.

### 7. Wrong vs Correct

```go
// Wrong: paginate backend hits and then reorder only one page.
metadataIDs, total := searchBackend.SearchMetadataIDs(ctx, query, offset, limit, filter)
sortCurrentPage(metadataIDs)

// Correct: rank the capped, revalidated Metadata candidate set before paging.
candidates, _, err := searchBackend.SearchMetadataIDs(ctx, query, 0, 100, filter)
groups := buildMetadataSearchTermGroups(MediaSearchTerms(query))
metadataIDs, total, err := mediaViewRepo.rankMetadataSearchIDs(
    ctx, query, groups, candidates, offset, limit, filter,
)
views, err := mediaViewRepo.FindMetadataSearchRepresentatives(ctx, metadataIDs, visibility)
```

## Scenario: NFO-Only Library Metadata

### 1. Scope / Trigger

- Apply when scanning, scraping, manually matching, or grouping media from
  `nfo_movie` and `nfo_tv` libraries.

### 2. Signatures

- Library types: `LibraryTypeNFOMovie = "nfo_movie"` and
  `LibraryTypeNFOTV = "nfo_tv"`.
- Policy owner: `libraryUsesNFOOnly(*model.Library) bool`.
- Persistence path: `ReadLocalMetadata` -> `applyLocalMetadataMatch`.

### 3. Contracts

- Scanner may persist the encoded local metadata hint and episode position, but
  must not merge path hints or copy NFO provider IDs into `Media.lookup_*`.
  Therefore an NFO provider ID cannot bind existing network canonical metadata.
- Scraper accepts only a valid NFO. Missing NFO becomes `no_match`; parse or
  persistence failure becomes `error`; neither condition falls back to a provider.
- `nfo_tv` processes every media file with its own episode NFO. A representative
  episode's metadata must never be synchronized across the candidate group.
- Provider manual search/apply is rejected server-side. Recovery is to repair or
  add the NFO and reset the existing media row to `pending`.
- Local persistence retains the existing Movie and Series/Season/Episode kinds
  and never edits playable files or sidecars.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Valid movie or episode NFO | Persist local canonical metadata and mark `matched` with source `local_nfo` |
| NFO is absent | Mark `no_match`; make zero provider requests |
| NFO cannot be parsed or persisted | Mark `error`; preserve a safe reason |
| NFO or path contains a provider ID | Keep it out of scanner canonical lookup fields |
| Provider manual search/apply is requested | Reject before any provider call |

### 5. Good / Base / Bad Cases

- Good: two episodes read two different episode NFO files and retain distinct
  titles, positions, and canonical Episode rows.
- Base: a personal clip has no NFO and remains visible as `no_match` for repair.
- Bad: copy TMDb IDs from an NFO into scanner lookup fields or synchronize one
  representative Episode across an NFO series group.

### 6. Tests Required

- Cover absent, malformed, and valid movie NFO plus show and per-episode NFO.
- Assert path/NFO provider IDs do not bind network metadata and provider call
  counts remain zero.
- Assert provider manual search/apply is rejected for both NFO-only types.

### 7. Wrong vs Correct

```go
// Wrong: turns an NFO external ID into a network canonical binding during scan.
applyLocalScanHints(media, localNFO)

// Correct: keep local NFO as scraper input and copy only episode position.
applyLocalEpisodeMetadata(media, localNFO)
media.LocalMetadataHint = encodeLocalMetadataHint(localNFO)
```

## Scenario: Web Media Detail Version Display

### 1. Scope / Trigger

- Apply when changing the Web media detail version list, track display, or asynchronous probe flow.

### 2. Signatures

- Authenticated read API: `GET /api/media/:id/versions -> MediaView[]`.
- Existing detail APIs: `GET /api/media/:id -> MediaView` and
  `POST /api/media/:id/probe/ensure -> MediaView`.

### 3. Contracts

- Versions are visible `MediaView` siblings with the same non-null
  `MetadataID`; the query reapplies allowed and hidden library filters.
- The URL media owns title, favorite state, playback, casting, and management
  actions. A selected sibling owns only the displayed video, audio, and subtitle
  options; no selector changes a playback target or parameter.
- Detail rendering does not wait for probing. Missing track data is repaired
  asynchronously and only the media-info region shows loading or probe failure.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Current media is absent, unresolved, or invisible | `404 not found` |
| No visible sibling remains after filtering | `404 not found` |
| Version query fails | `500` |
| Ensure probe cannot inspect the source | `422 media probe failed`; keep the base detail visible |

### 5. Good / Base / Bad Cases

- Good: selecting another visible version refreshes only the three track selectors.
- Base: one visible version has no tracks yet; show its base detail and probe locally.
- Bad: replace the URL media with the selected sibling or list versions by title heuristics.

### 6. Tests Required

- Handler/service tests cover multiple siblings, hidden-library filtering, and
  missing current media.
- Web verification covers stale-request cancellation, local probe status,
  unchanged play/cast IDs, empty selector states, and responsive keyboard use.

### 7. Wrong vs Correct

```typescript
// Wrong: a display-only choice silently retargets every detail action.
setMedia(selectedVersion)

// Correct: keep action state stable and isolate the selector projection.
setSelectedMedia(selectedVersion)
```

## Scenario: Media Detail Provider Links

### 1. Scope / Trigger

- Apply when changing the single-media detail projection or provider links in
  the Web detail page. List, search, and Emby projections remain unchanged.

### 2. Signatures

- `GET /api/media/:id` may add `metadata_kind`, `tmdb_snapshot`,
  `douban_snapshot`, `tmdb_status`, `douban_status`, and `series_tmdb_id` to
  the existing canonical `tmdb_id` / `douban_id` fields.
- Snapshot flags, provider statuses, and `series_tmdb_id` are `gorm:"-"`
  detail-only fields. Douban status values are `missing`, `partial`,
  `degraded`, or `complete`; TMDb retains the other three values.

### 3. Contracts

- IDs come from canonical `MetadataIdentifier` rows, never `Media.lookup_*`
  scan hints. Snapshot flags remain compatibility fields that mean only that
  the matching provider snapshot exists.
- `missing` means no snapshot; `partial` means a snapshot exists without that
  provider's local entity-owned image, or a Douban snapshot still uses the
  legacy/fallback wrapper; `complete` means the current snapshot and provider
  image both exist. Status describes local cache coverage, not every optional
  upstream field.
- `degraded` means the stored Douban snapshot came from the explicit
  `/subject/{id}` permission fallback. The persisted degraded flag takes
  precedence over payload/artwork completeness and must not be inferred from
  missing fields.
- Movie links use `/movie/{tmdb_id}`; Series links use `/tv/{tmdb_id}`.
  Season/Episode links display the entity's own TMDb ID but use the canonical
  Series TMDb ID plus season/episode numbers in the `/tv/...` deep link.
- Missing required IDs omit the link. External links open a new window with
  `noopener noreferrer`.
- Episodes with a canonical Series TMDb ID expose their own snapshot status
  even without an Episode TMDb ID. Missing Episode snapshots show a red badge
  with visible text `未获取到 TMDB 单集信息`; do not infer upstream absence from
  missing artwork or an ancestor snapshot. An ID-less placeholder has no deep
  link. A successful Episode snapshot removes the red warning on detail reload;
  missing artwork remains the ordinary `partial` warning.
- Web links hide the raw ID, use compact provider monograms plus distinct
  status icons, and expose the provider/status through `title`, `aria-label`,
  and screen-reader text. Rating is always visible and uses `-` when non-positive.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Provider ID is absent | Omit that provider link |
| Provider snapshot is absent | Return/show `missing` with an empty-circle icon |
| Episode snapshot is absent and canonical TMDb context exists | Show a red badge and visible missing-information text, including when its own TMDb ID is absent |
| Snapshot exists but provider image is absent | Return/show `partial` with a warning icon |
| Douban snapshot is explicitly degraded | Return/show `degraded` with a permission-limit warning, regardless of artwork |
| Current snapshot and provider image exist | Return/show `complete` with a checked-circle icon |
| Douban snapshot uses a `subject` / `data` wrapper | Keep it `partial` even when a Douban image exists |
| Season/Episode Series TMDb ID is absent | Omit the TMDb deep link |
| Snapshot or identifier lookup fails | Fail the detail request; do not report a false state |

### 5. Good / Base / Bad Cases

- Good: an Episode opens its Series/season/episode deep link while its own
  snapshot and still determine the TMDb cache status.
- Good: a restricted Douban detail stores a usable subject snapshot, displays
  an explicit degraded warning, and offers an administrator retry action.
- Base: a provider ID exists without a snapshot; its compact link shows the
  missing icon and no raw ID.
- Bad: infer snapshot presence from an ID, use an Episode ID as `/tv/{id}`, or
  add provider snapshot joins to shared list queries.

### 6. Tests Required

- Service: provider snapshot compatibility flags, all four Douban statuses,
  legacy Douban classification, explicit degraded precedence, provider-owned
  image state, and Series TMDb ID projection.
- Placeholder regression: `TestSeriesInventoryMissingEpisodesBindAndRecover`
  verifies ID-less detail status, retry reuse, 404 checkpoint preservation, and
  in-place recovery to `partial`. `check-series-presentation.mjs` verifies the
  red badge in standalone/embedded Episodes and removal after snapshot recovery.
- Web: Movie/Series/Season/Episode link construction, omitted/raw-hidden IDs,
  accessible status icons including the degraded warning, missing-rating
  fallback, safe external attributes, lint, and TypeScript production build.

### 7. Wrong vs Correct

```go
// Wrong: scan hints and current Episode ID are not the Series website identity;
// snapshot existence alone is not cache completeness.
media.SeriesTMDbID = media.LookupTMDbID
media.TMDbStatus = "complete"

// Correct: explicit snapshot quality takes precedence over artwork completeness.
media.DoubanStatus = "partial"
if snapshot.Degraded {
    media.DoubanStatus = "degraded"
} else if providerArtwork {
    media.DoubanStatus = "complete"
}
```

## Scenario: Manual Douban Binding

### 1. Scope / Trigger

- Administrator-selected Movie/Series association with immediate enrichment; never reuse graph-merging scrape/apply for this action.

### 2. Signatures

- `GET /api/metadata/:id/douban/search?query=...` returns `{items: ExternalMediaResult[]}`.
- `POST /api/metadata/:id/douban/bind` accepts `{douban_id, media_type: "movie" | "tv", force: boolean}` and returns `{status: "complete" | "degraded"}`.
- `MetadataIdentifier.DoubanEntityKind` / `douban_entity_kind`: non-null varchar(16), default empty, added by AutoMigrate. Manual binding stores `movie` or `series`; the identifier's existing `entity_kind` remains the local kind.

### 3. Contracts

- Search defaults to the canonical title and allows at most 200 characters. Manual binding reads `window.__DATA__` JSON from `https://search.douban.com/movie/subject_search?cat=1002&search_text=...` without executing scripts, selecting at most five deduplicated `search_subject` entries. Never substitute `subject_suggest` for complete manual search: `卑微` has no suggestions but complete search finds `4049664`. Missing/malformed data or HTTP failure returns an explicit 502 search-unavailable error, not an empty result. Numeric IDs and official Douban `/subject/{id}` links bypass search; never fetch a user-supplied URL. Resolve candidates through `/subject/{id}` to confirm type; unresolved types remain visible but non-actionable with a retry notice.
- Apply revalidates response ID, `type` and optional `is_tv`, then fetches the corresponding complete detail. Download images outside the transaction; lock canonical metadata and recheck its timestamp and identifiers before committing binding, fill-only fields, candidate and snapshot together.
- Forced binding only relaxes the local/provider kind mismatch. Preserve local kind, children, coordinates, files, other provider IDs and existing selected artwork. Compare TMDb IDs only within the same kind.
- Background enrichment uses the stored actual kind only for an explicitly confirmed manual association; empty values retain local-kind routing. Recheck binding under the metadata lock after network I/O; discard stale responses without updating cooldown.
- Metadata editing rejects a changed `douban_id` before saving fields; unchanged legacy submissions remain compatible. Manual apply refreshes the media projection/cache after commit.

### 4. Validation & Error Matrix

- Non-admin: 403. Missing metadata: 404. Invalid ID (not 1–20 digits, leading zero), unknown/contradictory type or ineligible local kind: 400.
- Actual type differs from selected type, unconfirmed cross-type request, occupied identifier or concurrent binding change: 409.
- Missing provider entry: 404. Other fetch/save failure: 502; preserve the previous database association.

### 5. Good / Base / Bad Cases

- Good: explicitly force a local Series to a verified Douban movie and immediately store its snapshot without changing any Season/Episode.
- Base: same-kind apply needs no confirmation and fills only gaps (including the existing complete-response Chinese-title precedence).
- Bad: infer actual type from suggest, silently try the opposite type after a background 404, or save the new ID before fetching its data.

### 6. Tests Required

- `TestDoubanBindingImmediateAndForced`: all four local/provider combinations, network failure and transaction rollback, immediate fields/artwork/snapshot, child preservation and persisted background routing.
- `TestDoubanBindingSearchUsesSubjectType`, `TestDoubanBindingRejectsUnknownAndChangedType`, `TestDoubanBackgroundRejectsResponseAfterRebind`, `TestDoubanBindingRequiresAdmin`.
- `node web/scripts/check-douban-binding.mjs`: automatic search, unknown rejection, mismatch confirmation/cancel, force payload, pending suppression, error retention, refresh and abort.

### 7. Wrong vs Correct

- Wrong: write `douban_id`, wait for a later scan and treat suggest's `movie` label as verified.
- Correct: verify subject type, require explicit cross-type confirmation, then atomically bind and enrich the canonical work.

## Scenario: Douban Movie/Series Secondary Enrichment

### 1. Scope / Trigger

- Apply when persisting a Movie with a Douban ID, running historical Douban
  Movie/Series enrichment, manually enriching a Movie or Series, storing provider
  snapshots, or editing canonical metadata.
- The periodic and administrator single-item flows both
  support `MetadataKindMovie` and canonical `MetadataKindSeries`; Season and
  Episode never enter this flow.

### 2. Signatures

- Raw detail carrier: `Match.RawJSON []byte` and original success time
  `Match.DoubanFetchedAt time.Time`; keep both when forwarding cached details.
- Regular Douban detail boundary: `DoubanProvider.GetMatchByID(ctx, doubanID) (*Match, error)`.
- Enrichment-only detail boundary:
  `DoubanProvider.GetEnrichmentMatchByID(ctx, doubanID, entityKind) (*Match, degraded bool, error)`.
- Primary enrichment endpoints are
  `https://m.douban.com/rexxar/api/v2/movie/{doubanID}` for Movie and
  `https://m.douban.com/rexxar/api/v2/tv/{doubanID}` for Series. Their explicit
  permission fallback is `/rexxar/api/v2/subject/{doubanID}`;
  `subject_abstract` remains fallback only for regular scraping and
  episode-count reads.
- Single-item admin API: `POST /api/media/:id/douban-enrichment`; `:id` accepts
  the same concrete Media or canonical Metadata identity as the detail API.
- Episode count boundary: `GetEpisodeCountByID` reads `episodes_count` from the
  same mobile detail response.
- Snapshot writes:
  `SaveProviderData(ctx, metadataID, provider, payload, fetchedAt)` and
  `UpsertDegradedProviderSnapshot(ctx, metadataID, provider, payload, fetchedAt)`.
- Non-Douban snapshot identity remains `(metadata_id, provider)` with JSONB
  payloads. Douban details now live in `douban_subjects`, keyed by external ID
  and effective provider kind; the snapshot methods return a compatibility
  projection from the current unique identifier. Do not read/write legacy
  Douban rows in `metadata_provider_snapshots`.
- Candidate discovery:
  `ListDoubanMovieEnrichmentAfter(ctx, afterID, refreshBefore, limit) []DoubanMovieEnrichmentCandidate`.
- Candidate artwork:
  `MetadataArtworkCandidate{MetadataID, AssetID, ArtworkType, SourceProvider, SourceURL}`
  is unique on `(metadata_id, artwork_type, source_provider)`.
- Periodic job: `douban_movie_enrichment`; settings
  `metadata.douban_movie_enrichment_enabled` (default `false`) and
  `metadata.douban_movie_enrichment_interval_seconds` (default 86,400).
- Display name: `豆瓣信息补齐`. Preserve the job/key, settings, cursor and
  legacy execution name/filter `豆瓣电影信息补齐` for history continuity.
- Metadata edit payload `MediaMetadataUpdate` contains no `poster_url` or
  `backdrop_url` fields; the web edit dialog neither renders nor submits them.

### 3. Contracts

- `GetMatchByID` preserves the complete valid response, including unknown
  fields, marks the match source as `douban`, and retains the existing
  mobile-to-`subject_abstract` fallback for regular scraping compatibility.
- Batch and single-item enrichment call `GetEnrichmentMatchByID` and never
  request or save `subject_abstract`. Only HTTP 403 or a valid JSON response
  whose top-level `code` / `error_code` equals `1000` requests
  `/subject/{id}`. A successful subject response is returned and persisted with
  `degraded=true`. HTTP 404 remains permanent; HTTP 429, 5xx, network/timeout,
  empty/invalid JSON, and other error objects remain retryable and never start
  the subject fallback.
- Subject requests use the same current Cookie resolution and error
  classification without recursive fallback. A failed subject request writes
  no fields, artwork, or snapshot and preserves its not-found/retryable result.
- Normal persistence passes an already-fetched Douban detail into enrichment;
  it must not issue the same detail request again just to save fields/artwork.
- Search results never trigger secondary enrichment. A periodic candidate must
  already own exactly one same-kind `(douban, movie)` or `(douban, series)`
  identifier. No search, title guess,
  cross-kind ID, or ambiguous identifier is accepted.
- TMDb remains primary. Douban fills only empty canonical overview,
  original-name, rating, year, release-date, languages, countries, and genres.
  Title is the sole precedence exception: a Chinese Douban title may replace a
  non-Chinese title, preserving the old title as original name when needed.
  Source, existing non-empty fields, and provider IDs stay unchanged.
- A degraded subject response only fills empty canonical fields. It never uses
  the Chinese-title precedence exception to replace a non-empty title and never
  clears a field omitted from the subject payload.
- If both sources have the same entity kind and provide a TMDb ID that conflicts with the canonical TMDb
  identifier, skip local enrichment, including artwork and compatibility writes.
  Independently validated shared subject data may already have been saved.
- A historical candidate reuses valid cached details or makes one Movie or TV detail request according
  to its canonical kind, unless its identifier has an explicitly confirmed
  `douban_entity_kind` from manual binding. Save a
  complete or degraded valid response in the shared subject table before local
  field/artwork persistence. Artwork failures retain the successful detail and
  its original fetching time for reuse. A successful permission fallback advances
  the cursor and continues the batch; request/parsing failures never advance
  the successful fetching time. Local completion never extends detail freshness.
- Canonical graph merge moves provider snapshots and artwork candidates to the
  surviving metadata; when the same provider/candidate key already exists, the
  surviving target row wins before the source metadata is hard-deleted.
- A Douban poster is downloaded immediately to managed local artwork and saved
  as a provider candidate whose `SourceURL` is the normalized official large
  image URL. A configured CDN is a temporary `ImageProxy` transport only.
  Existing selection always wins; only an absent selection is atomically
  promoted. Public responses continue to use only `/api/artwork/:assetID`,
  never a remote URL.
- Historical passes use current unique same-local-kind Douban identifiers,
  routing by confirmed `douban_entity_kind` when present. Retry timestamps apply independently of detail freshness. Useful cached titles,
  overviews or posters keep local gaps eligible even while details are fresh;
  local gaps without useful cached values are rechecked after 24 hours. Legacy
  partial details are eligible after that local cooldown. Other empty fields
  are filled during enrichment and never trigger a request alone.
  Passes retain metadata-ID keyset pagination, batch size 100, serial processing,
  two-second inter-item delay and the existing persisted cursor. Retryable
  upstream failures stop before advancing the failed item's cursor. Not-found
  entries advance the cursor. Metrics count actual upstream calls rather than
  cache accesses; shared Provider admission also spaces fallback/route attempts.
- Series enrichment writes only its own fields, poster candidate/selection and
  Douban snapshot. Never change child metadata, identifiers, artwork, snapshots,
  season/episode coordinates, hierarchy, inventory or Media attachments.
- Single-item enrichment resolves the current detail view first, operates on
  its canonical Movie/Series `MetadataID`, uses the matching Movie/TV endpoint,
  and never reads or changes the batch cursor. It returns `status=complete` or
  `status=degraded`; retryable upstream failures return HTTP 429. A complete
  retry upserts `degraded=false`, while another permission fallback refreshes
  the subject payload and keeps `degraded=true`.
- Artwork URL removal is enforced at both UI and backend DTO boundaries, so
  metadata editing cannot clear or replace the current selection.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Search candidate is returned but not accepted | Write no provider snapshot |
| Periodic or manual target is not a Movie/Series | Skip without provider or artwork I/O |
| Movie/Series has zero or multiple same-kind Douban IDs | Skip as ambiguous |
| Series has one Douban Series ID and is manually retried | Request `/tv/{id}` without touching the Movie batch cursor |
| Movie has one Douban ID and no snapshot | Fetch details once, then store the full response |
| Movie/TV detail returns HTTP 403 or JSON code `1000` | Request `/subject/{id}` once and persist a successful response with `degraded=true` |
| Movie/TV detail returns 404, 429, 5xx, a network error, or another JSON error | Preserve not-found/retryable semantics and do not request subject |
| Subject fallback fails | Persist no new fields, artwork, or snapshot; preserve the subject error classification |
| Fresh degraded detail has useful values for local gaps | Complete local fields/artwork from the cache without another request |
| Manual retry gets a complete Movie/TV response | Replace the payload and clear `degraded` atomically |
| Manual retry is still permission-restricted | Refresh the subject payload and keep `degraded=true` |
| Complete detail is within its TTL (default seven days), or degraded detail within at most one day | Reuse it unless explicitly forced |
| Douban poster, overview, and Chinese title have no local gaps | Skip enrichment; the independent data-refresh job can still refresh expired details |
| Local gaps have no useful cached values | Recheck after the 24-hour local cooldown; shared detail freshness still decides network access |
| Mobile detail fails during regular scraping but abstract succeeds | Save the wrapper as partial; complete-detail callers cannot reuse it as complete data |
| Mobile detail has a retryable failure during batch enrichment | Stop the batch before the current cursor write; request no later candidate |
| The next batch runs after a retryable failure | Retry the same candidate first |
| Mobile detail explicitly reports not found | Record a permanent failure, advance that candidate, and continue |
| Single-item enrichment has a retryable upstream failure | Return HTTP 429 without changing the batch cursor |
| Network/parsing/shared subject persistence fails | Preserve the previous successful `fetched_at` |
| Local field/artwork persistence fails after a successful detail fetch | Retain the saved detail and its original fetching time |
| Detail contains unknown fields | Preserve them in the valid JSONB document |
| Same-kind detail TMDb ID conflicts with canonical TMDb ID | Skip local fields, artwork and compatibility writes; keep independently validated shared data |
| Canonical field is non-empty | Preserve it; only a complete response may use the Chinese-title rule |
| Current artwork selection exists | Save local candidate and preserve selection |
| Current artwork selection is absent | Atomically promote the local candidate |
| Detail or image request fails during normal TMDb persistence | Keep TMDb persistence successful; log metadata ID only |
| Edit payload includes legacy image URL keys | Ignore them because the typed DTO has no matching fields |

### 5. Good / Base / Bad Cases

- Good: a TMDb Movie with one Douban ID stores the mobile raw Douban response, fills a
  missing Chinese title/overview, localizes its poster as a candidate, and keeps
  the existing TMDb selection.
- Good: a restricted Movie/TV detail stores only the real subject response as
  degraded; local completion reuses useful cached values and the data-refresh
  job can retry after expiry without replacing an existing complete response.
- Base: all three trigger fields are complete, so enrichment makes no request;
  independently, data refresh checks expiry using the shared TTL and backoff.
- Bad: query Douban by title, enrich Season/Episode in this task, treat every upstream
  failure as permission denial, infer degraded state from missing fields,
  overwrite a non-empty field/current image, expose a remote image URL, or scan
  history with offset pages.

### 6. Tests Required

- Provider unit: Movie/TV URL and Referer, regular abstract fallback, exact
  HTTP 403 / JSON code `1000` subject fallback, non-permission no-fallback,
  subject-failure classification, typed retryable/not-found errors, nested
  rating/image, episode count, source, IDs, projected lists, and unknown raw
  fields survive detail parsing; normal persistence makes one detail request.
- Repository/PostgreSQL: Movie/Series same-kind unique-ID keyset discovery, ambiguity skip,
  no-snapshot admission, local cooldown, cached local-gap admission,
  no-local-gap exclusion, useful degraded cache reuse, complete-upsert degraded
  clearing, candidate uniqueness, and atomic selection preservation/promotion.
- Service: complete and degraded fill-only rules, Chinese-title replacement,
  subject failure without a snapshot, Movie/Series kind boundaries, TMDb
  mismatch rejection, complete recovery, failure without cooldown advancement,
  degraded continuation, snapshot-only logging, retryable stop/retry cursor
  behavior, permanent-not-found continuation, and bounded cursor progress.
- `TestDoubanEnrichmentBatchPreservesSeriesChildren` verifies the actual batch
  uses the TV endpoint, fills Series fields/poster, respects cooldown, and
  preserves all child metadata, identifiers, artwork, snapshots and Media rows.
- API/web: editing ordinary metadata preserves artwork; the administrator-only
  single-item action covers Movie/Series complete/degraded responses, ineligible
  metadata, pending suppression, refresh, manual recovery, and HTTP 429
  messaging; the detail view displays the accessible degraded warning and retry
  label. TypeScript build proves both status enums and that the edit form and
  payload contain no artwork URL fields.

### 7. Wrong vs Correct

```go
// Wrong: a secondary provider overwrites authoritative non-empty fields or
// hides a permission fallback inside an apparently complete snapshot.
metadata.Overview = doubanDetail.Overview
repo.SaveProviderData(ctx, metadata.ID, "douban", subject.RawJSON, fetchedAt)

// Correct: the Provider saves shared data before local completion. Compatibility
// writes preserve the original fetching time; local failures retain the detail.
fetchedAt := detail.DoubanFetchedAt
if degraded {
    repo.UpsertDegradedProviderSnapshot(ctx, metadata.ID, "douban", detail.RawJSON, fetchedAt)
} else {
    repo.SaveProviderData(ctx, metadata.ID, "douban", detail.RawJSON, fetchedAt)
}
fillMissingDoubanFields(ctx, metadata.ID, metadata.Kind, detail, degraded)
repo.SaveCandidate(ctx, metadata.ID, "poster", "douban", source, asset)
```

## Source completion and local season identity

- Ordinary display fields, credits and images are read from source-center projections.
  Metadata retains identity, hierarchy and bindings; it has no hydration checkpoints
  or catalog job queue. `catalog_hydration_jobs` and the six metadata hydration/artwork
  checkpoint columns are retired and are not created by the current schema.
- TMDb movie/series/season/episode maintenance owns source completeness. Summary
  quality alone is insufficient even when basic display fields look complete.
  `tmdb_data_people` supplements missing source credits and person details without
  creating metadata or media. See the background-task contract for evidence and retry.
- TMDb/Douban source image workers own downloads and retry, including person avatars.
  No separate metadata artwork worker or handoff checkpoint remains.
- Scanning still creates stable season/episode identities and binds each file to its
  own coordinates. Reuse the saved season inventory; refresh once per group when a
  requested episode is absent. Never copy ancestor descriptions into child fields.
- Missing episodes may have local coordinate placeholders without fabricated provider
  identifiers or snapshots. Only season 0 HTTP 404 tolerates a missing season; other
  HTTP failures remain errors. A later valid inventory fills identifiers while keeping
  local identity and file attachment stable.
- Source presentation retains the identity's source marker while its provider record
  is unavailable. This is not a fallback for title/overview or other retired fields.
- Series cards use series presentation without changing the underlying episode playback
  target. Existing canonical series identity wins over title/year grouping hints.
- Provider failures and task logs never expose URLs, query strings or credentials.
- Regression: `TestSeriesInventoryBindsOwnEpisodesAndReusesSnapshot`,
  `TestSeriesInventoryMissingEpisodesBindAndRecover`,
  `TestSeriesInventoryOnlySpecialsTolerateNotFound`, and `TestTMDbPeopleMaintenance*`.

## Scenario: TMDb Season / Episode Metadata Recheck

### 1. Scope / Trigger

- Apply when changing the periodic/manual repair of historical Season/Episode
  title, overview, release date, credits, or own poster/still artwork.

### 2. Signatures

- Checkpoint: `MetadataItem.TMDbEpisodeCheckedAt *time.Time` maps to indexed,
  nullable `metadata_items.tmdb_episode_checked_at` through the explicit GORM
  tag `column:tmdb_episode_checked_at`; do not rely on acronym inference.
- Season checkpoint: `MetadataItem.TMDbSeasonCheckedAt *time.Time` maps to
  indexed, nullable `metadata_items.tmdb_season_checked_at`. Seasons and Episodes
  have independent 72-hour cooldowns; graph merge preserves the newer checkpoint.
- Candidate boundary:
  `ListTMDbEpisodeMetadataRecheckAfter(ctx, afterID, checkedBefore, limit)` and
  `ListTMDbSeasonMetadataRecheckAfter(ctx, afterID, checkedBefore, limit)` return
  `TMDbMetadataRecheckCandidate` with the explicit entity kind.
- Provider boundary:
  `GetTVEpisodeDetails(ctx, seriesTMDbID, seasonNum, episodeNum)`.
- Scheduler job: `tmdb_episode_metadata_recheck`, default disabled, default 24 hours.
- Task display: `TMDb 季/集信息补全/复查`; retain the existing definition key,
  settings, execution name and history filter (`TMDb 集信息补全/复查`) for continuity.

### 3. Contracts

- Candidates are Episode metadata with direct `media`, one valid Series TMDb
  identifier, an expired/null checkpoint, and at least one missing requirement:
  empty overview, empty release date, or absent valid still.
- Independently scan Seasons with direct media or playable Episode descendants,
  one valid Series TMDb ID and at most one own TMDb ID. Missing
  overview, release date, poster, own identifier or own snapshot admits a Season
  after its cooldown, even when all child Episodes are complete. Include Season 0;
  exclude metadata without media and do not create missing hierarchy or inventory.
- Empty/generated Season/Episode titles never trigger recheck or count toward
  remaining-gap logs or incomplete metrics. Other gaps still trigger the same
  request, which may update the title using the existing non-empty projection.
- Season responses must have a positive ID, valid raw JSON, matching season number
  and matching own ID when one is known. Reuse the non-empty field projection,
  loaded-credit persistence, insert-if-absent artwork and identifier/snapshot
  transaction. Do not change catalog checkpoints, file facts or Media attachments.
- One execution keyset-pages every candidate in batches of 200 without a
  persisted cursor or request cap. Manual runs obey the same 72-hour cooldown.
- Each page runs at most three complete rechecks concurrently. Workers own
  separate metrics; one collector merges counters and emits completion-order
  details. Cancellation stops queued work and drains in-flight results before
  task completion; one failed item must not stop unrelated candidates.
- `SaveTMDbMetadataRecheck(ctx, item)` accepts only Season/Episode with a check
  time and writes only title, overview, release date, rating, year and that
  kind's monotonic checkpoint. It must not save parent/coordinates/file facts
  or refresh top-level search documents: those documents contain only
  Movie/Series fields. Other metadata updates retain normal index synchronization.
  Reject missing rows instead of recreating deleted metadata. Keep the existing
  task-level media-cache invalidation after successful checks.
  Verify the bounded/canceled worker flow and restricted persistence with
  `TestTMDbMetadataRecheckPageBoundsConcurrencyAndCancellation` and
  `TestSaveTMDbMetadataRecheckOnlyWritesChildDisplayFields`.
- One provider response supplies non-empty title, overview, release date,
  rating, year, loaded credit scopes, and still. Non-empty provider values may
  overwrite current values; empty values never clear stored values.
- A successful provider and persistence flow writes the checkpoint even if
  required fields remain missing. Provider, credits, artwork, metadata, or
  checkpoint failure leaves it unchanged; already-written partial results remain
  and are retried idempotently.
- Episode recheck persists the returned positive TMDb Episode ID and raw detail
  snapshot with `ReplaceIdentifierWithSnapshot` before advancing the checkpoint.
  This also upgrades an existing hierarchy-only placeholder without replacing
  its metadata ID or Media attachments; snapshot persistence failure remains
  retryable.
- Automatic still persistence uses `SaveCatalogSelection` semantics. A
  concurrent manual/local selection wins and counts as a satisfied still.
- Season and Episode are excluded from `tmdb_artwork_missing_recheck` but remain eligible
  for `tmdb_artwork_local_repair` when an existing selected artwork loses its file.
- Detail logs contain only updates, still-missing results, concurrent skips, or
  failures and identify Series/SxxExx/TMDb ID without paths, URLs, or credentials.
- Startup migration copies `tm_db_episode_checked_at` into the canonical column
  only when the canonical value is null, then drops the legacy column in the
  same transaction. An absent legacy column is an idempotent no-op.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Episode has no direct media | Exclude it |
| Season has no direct media or playable Episode descendants | Exclude it |
| Episodes are complete but their Season lacks fields, poster, ID or snapshot | Repair the Season independently |
| Season response has a different number or known ID | Fail before persistence; retain the previous checkpoint |
| Only a Season/Episode title is missing or generated | Send no provider request; do not count it as incomplete |
| Episode overview, release date, and still are complete | Send no provider request |
| Last successful check is within 72 hours | Exclude it for scheduled and manual runs |
| Provider returns nil, 404, network, limit, or decode error | Record a sanitized failure and do not advance the checkpoint |
| Provider returns a successful response with some empty fields | Preserve stored non-empty fields, log remaining gaps, and advance the checkpoint |
| Any persistence step fails | Keep completed partial writes, do not advance the checkpoint, and continue later candidates |
| Still save races with a manual/local selection | Preserve the concurrent selection and treat still as satisfied |
| Metadata graphs merge | Preserve the newer Episode checkpoint |
| Only `tm_db_episode_checked_at` exists | Preserve its values in `tmdb_episode_checked_at`, then drop the legacy column |
| Both checkpoint columns contain values | Preserve the canonical `tmdb_episode_checked_at` value |
| Legacy checkpoint column is absent | Complete startup migration without schema changes |

### 5. Good / Base / Bad Cases

- Good: an Episode with `Episode 1`, no release date, and direct media receives
  one details request; concrete fields and still are saved, then it cools for 72 hours.
- Good: a hierarchy-only Season 0 gains its own ID, snapshot and poster in place;
  complete child Episodes make no requests and keep their Media attachments.
- Base: TMDb still has no overview or still; the successful response advances
  the checkpoint and logs only those remaining gaps.
- Good: upgrading a database with the legacy GORM-derived checkpoint column
  preserves its values and leaves only the canonical indexed column.
- Bad: scan metadata without media, persist a cross-execution cursor, call the
  discovery catalog worker, or overwrite a concurrent manual still.
- Bad: depend on GORM to infer `TMDb` as one acronym or leave both checkpoint
  columns active after migration.

### 6. Tests Required

- PostgreSQL: direct-media filter, non-title completeness, title-only exclusion,
  missing release date/still, valid Series identity, cooldown, deduplication,
  keyset pagination, exact canonical schema column, legacy value preservation,
  canonical-value precedence, legacy-column removal, repeated migration, and
  newer-checkpoint merge.
- Service: non-empty overwrite including release date/year, remaining-gap
  detection, checkpoint success/failure behavior, still concurrency, per-item
  failure isolation, sanitized detail logs, and normal no-change silence.
- Scheduler/definition: stable key, default disabled, 24-hour interval, and
  manual action mapping.
- `TestListTMDbSeasonMetadataRecheckAfterFiltersAndPages`: complete-child Seasons,
  direct Season attachments, special/generated/empty-title exclusion, NULL overview, missing
  date/poster/snapshot/ID, cooldown, no-media/ambiguous exclusion and keyset pages.
- `TestTMDbMetadataRecheckRepairsSeasonsAndEpisodes`: mixed success/failure,
  Season identity/snapshot/credits/local poster, preserved existing fields/images,
  unchanged Media/inventory, cooldown expiry, 404 retry, identity mismatch and
  snapshot failure without checkpoint advancement.

### 7. Wrong vs Correct

```go
// Wrong: send Episode repair through catalog discovery and overwrite selections.
queueCatalogHydration(episode)
repo.SaveSelection(ctx, episode.ID, model.ArtworkTypeStill, "tmdb", source, asset)

// Correct: query only repair candidates and let concurrent selections win.
candidates := repo.ListTMDbEpisodeMetadataRecheckAfter(ctx, afterID, checkedBefore, 200)
repo.SaveCatalogSelection(ctx, episode.ID, model.ArtworkTypeStill, "tmdb", source, asset)
```

Season candidates use their own scan and poster type; deriving them only from
the Episode candidate list misses Seasons whose Episodes are already complete.

```go
// Wrong: GORM splits the unrecognized TMDb acronym into tm_db.
TMDbEpisodeCheckedAt *time.Time `gorm:"index"`

// Correct: the model and hand-written SQL share one explicit column name.
TMDbEpisodeCheckedAt *time.Time `gorm:"column:tmdb_episode_checked_at;index"`
```

## Scenario: Persisted Emby People Images

### 1. Scope / Trigger

- Apply this contract when changing `Person` profile images, credit
  persistence, or Emby person image routes.
- Person profile images are managed assets, but they are not
  `MetadataArtwork`; a person has one current image and keeps its provider
  source URL as provenance only.

### 2. Signatures

- `Person.ProfileURL` is the source reference.
- `Person.ProfileImageKey` is a relative key under `DataDir`, such as
  `catalogs/tmdb/people/ab/cd/ef/<hash>.jpg`.
- `PeopleImageStore.Import(ctx, provider, source) (key, error)` validates and atomically
  stores image bytes.
- `PeopleImageStore.ImportCached(ctx, provider, source)` reuses successful remote imports
  through a private bounded in-process `RuntimeCacheService` for 24 hours.
  Credit preparation first batch-resolves provider identities and reuses valid
  persisted remote images whose `ProfileImageSourceURL` matches the incoming URL,
  including after restart. Explicit TMDb refresh uses direct Import only on a miss.
- `PeopleImageStore.ServePerson(ctx, writer, request, personID)` serves only a
  validated local key and reports whether the ID belongs to a person.
- Public image contract remains `GET /Items/{id}/Images/Primary` and its
  `/emby` and case variants.

### 3. Contracts

- Person bytes live at `App.DataDir/catalogs/<source>/people/aa/bb/cc/`;
  the key uses content SHA-256 and deduplicates within the source. See
  [source-owned original storage](./image-variants.md#scenario-source-owned-persistent-originals)
  for legacy migration and source-scoped import caches.
- Outside catalog hydration, profile downloads happen before credit persistence writes the person and
  bypass `cache/images`; image network errors are warnings and do not fail the
  authoritative scrape.
- Normal credit persistence reuses a successful remote URL only while the local
  file is valid and its computed storage key matches the cached key. Cache expiry,
  eviction or process restart causes another direct import only when no matching
  persisted person image is available. Local source files
  are always reread; failures never create a success mapping. Automatic remote
  import failures instead cache a failure flag for one minute; caller cancellation
  does not. Explicit Import bypasses cooldown, and success replaces the flag.
  Cache hits never extend failure expiry. Do not infer a
  successful URL/key mapping from `ProfileURL` and key alone: a failed refresh
  can retain an old key alongside a new source URL. Catalog downloads additionally
  require persisted `ProfileImageSourceURL == ProfileURL` and a usable local file.
- A successful refresh replaces the key only after validation and atomic write;
  a failed refresh preserves the prior key and file.
- There is no startup image migration or request-time lazy download. The
  scraper persistence and its asynchronous catalog artwork phase own remote
  person-image ingestion.
- The Emby JSON shape remains `ImageTags.Primary = person.ID`; the image handler
  serves local bytes directly. A missing, invalid, or failed image returns the
  existing transparent placeholder.
- `cache/images` cleanup must not remove files under `DataDir/people`.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Empty or unsupported profile source | Do not import; serve the placeholder |
| Non-image, malformed, or oversized bytes | Reject import; preserve any prior key |
| Existing file has the expected size but different bytes | Rewrite it atomically |
| Source download fails or times out | Log a warning, preserve the prior key, and let scraping succeed |
| Profile source changes | Import the new image before updating the key/source pair |
| Key escapes the people root | Reject it and serve the placeholder |
| Person has no usable local key | Serve the placeholder without network I/O |
| Successful source is cached and local bytes match its hash key | Reuse without a network request |
| Cached local bytes are missing, corrupt, or belong to another image | Download and atomically repair before returning the key |
| `cache/images` contains bytes or a failure marker for the source | Ignore it and perform direct import |

### 5. Good / Base / Bad Cases

- Good: repeated references to one image resolve to one sharded content-hash
  path and Emby serves the bytes from that path.
- Base: a profile URL exists but its download is unavailable; metadata commits
  and the old local image remains available without request-time retry.
- Bad: returning `ProfileURL` to `ImageProxy` from the Emby person route when a
  local key is missing.
- Bad: storing people images in `cache/images` or attaching them to
  `MetadataArtwork`, which couples them to media artwork cleanup and schema.

### 6. Tests Required

- Storage: sharded path, SHA-256 deduplication, atomic persistence, malformed
  input rejection, and same-size corruption repair.
- Persistence: successful credit image key write, source/key replacement, and
  failed refresh preservation.
- Remote import: assert no image or failure marker is created under
  `cache/images`.
- `TestPersistCreditsReusesRemoteProfile`: separate works reuse one download;
  missing/wrong local bytes are repaired, explicit refresh reuses unchanged images after restart,
  source changes are fetched, failure cooldown suppresses repeat requests,
  expiry restores automatic requests and explicit recovery bypasses cooldown.
- `TestRefreshCreditsImportsOnlyChangedOrBrokenProfiles`: unchanged URLs retain
  translations without downloads; missing/corrupt files, changed URLs and new
  identities download; a failed changed URL keeps old provenance and is retried.

### Shared remote fetch and provider routing

- `ImageProxy.fetchRemoteImageUncached` coalesces only overlapping network
  requests by exact URL and `directOnly`. It does not cache completed results,
  change URL validation or mix direct-only and fallback-enabled routes. Every
  waiter can cancel independently; an owner canceled by its caller releases
  surviving waiters to retry. A transport timeout with a live caller is a shared
  failure, not an instruction to retry indefinitely. No network-wide mutex.
- `applyFanartArtwork(ctx, match, mediaType)` receives the resolved media kind.
  Movie uses `/movies/{TMDbID}`; TV uses `/tv/{TVDBID}`. No TVDB ID means no TV
  artwork request, never a Movie request with a Series TMDb ID.
- Tests: `TestRemoteImageFetchCoalescesAndCancels` checks request sharing and
  owner/waiter cancellation; `TestFanartArtworkUsesMatchingMediaKind` checks
  mutually exclusive endpoints and missing-TVDB behavior. Run with `-race`.
- Wrong: lock only the cache write after concurrent duplicate downloads, or
  call Movie artwork whenever any TMDb ID exists. Correct: share the active
  fetch before I/O and dispatch provider endpoints using the resolved kind.
- Emby: local bytes for GET and HEAD, `/Items` and `/emby/Items` prefixes,
  uppercase/lowercase route variants, and transparent placeholder on failure.
- Compatibility: existing movie/series artwork tests and cache cleanup behavior
  remain unchanged.

### 7. Wrong vs Correct

#### Wrong

```go
// The handler falls through to ImageProxy and may fetch or expose a remote URL.
raw, _ := svc.Emby.ImageURL(ctx, personID, "primary")
return svc.ImageProxy.Serve(ctx, w, r, raw)
```

#### Correct

```go
// Resolve the person first and serve only the validated DataDir/people key.
if isPerson, err := svc.PeopleImages.ServePerson(ctx, w, r, personID); isPerson {
    if err != nil {
        serveTransparentPlaceholder(w)
    }
    return
}
```

## Scenario: Provider Centers as the Only Source Store

- TMDb raw responses live in `tmdb_payloads`, associated with independent
  `tmdb_subjects` or `tmdb_person_profiles`; Douban responses live in
  `douban_subjects`. Local `metadata_identifiers` link display metadata to them.
- `ProviderData` is a read DTO, never an AutoMigrate model. `FindProviderData`
  and `ListProviderDataAfter` project center payloads through current identities.
  No old-table fallback is allowed. Summary responses retain the degraded flag;
  newer base/season-episode data overlays full detail without erasing its extras.
- `SaveProviderData`/`SaveDegradedProviderData` route only supported providers to
  their centers. `ReplaceIdentifierWithData` changes the local identifier and
  saves validated source data in one transaction. Failures roll back both.
- Accepted NFO/cross-provider ingestion may call `ensureTMDbSourceData` for
  immediate Movie/Series detail. Unconfigured sources skip it; failure does not
  undo accepted local metadata. `InsertMissingTMDbData` locks and rechecks the
  identity after fetching; concurrent source data wins. Retry happens through
  subsequent ingestion or source-center refresh, never startup legacy backfill.
- Local metadata deletion/merge moves local associations only. It never deletes
  independent center data. Shared person identities, media, user state and source-owned assets remain business records.
- Startup maintains only the current schema; retired source tables and settings
  were removed by a completed one-time operation. There is no startup retirement
  or import chain. Center tables and local associations remain intact. Removed
  historical task definitions/actions must not be accepted by the task API.
- Verify with real PostgreSQL: clean and repeated startup, current schema baseline,
  data/identifier preservation, missing-provider results, center reads/updates,
  optional fill concurrency, manual identity rollback and local graph merge.


## Scenario: Web Series Detail Hierarchy

### 1. Scope / Trigger

Series detail separates canonical Series presentation from selected Episode and concrete playback file. Cross-layer reads, favorites and manual edits must preserve these identities.

### 2. Signatures

- `GET /api/media/:id/series` returns `{series, favourite}`.
- `GET /api/media/:id/season` returns `{season}` (nullable). Resolve only through
  the visible file's canonical `MediaView.SeasonID`, validate Season kind,
  and return Season-owned title, season number and managed artwork. Missing,
  hidden or unbound media returns no Season; never use scan hints or substitute
  Series/Episode artwork. This is read-only and never creates metadata.
- `PUT /api/media/:id/series/favorite` accepts `{favourite: boolean}`.
- `GET /api/media/:id/credits?scope=series` reads Series credits.
- Manual metadata updates accept optional `scope: "series"`.
- Library Series listing accepts exact `series_id` or `key`; episode responses include user-scoped `history`.

### 3. Contracts

- Media route IDs are visible concrete file IDs, never the returned Series metadata ID. Series presentation contains its own title, overview, providers and artwork, without representative-file technical fields.
- Series edits update canonical Series metadata once; they must not rewrite Episode titles, coordinates or file linkage. Default movie/episode updates remain unchanged.
- Resolve Series through the validated file's `MediaView.SeriesID`, which supports direct Series, Season and Episode attachments. Read own metadata with `FindSeriesPresentation`; do not require an Episode representative from the search query. Validate file visibility using library permissions. A direct Series attachment may legitimately have no Season/Episode rows.
- Missing episode coordinates are displayed as an associated file, not `E0` or episode zero. Do not create hierarchy records just to satisfy detail-page rendering.
- History covers all visible episode identities, not only the recent-history page. Administrative actions retain all concrete files even when episode cards are deduplicated.
- URL season/episode/version selects the current file; version must belong to the selected logical episode. Series version selection retargets playback, unlike the existing movie display-only version contract.

### 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Missing, hidden, restricted or non-Series file scope | No Series result; scoped handler returns 404 |
| Missing favorite boolean | 400; explicit false is valid |
| Unknown edit scope or Series edit with season/episode coordinates | Reject update |
| History/database read fails | Return error, not an empty successful episode response |
| Invalid URL selection | Normalize to available season/episode/version |
| Selected file detail fails | Show retry; do not display or play the previous file |

### 5. Good/Base/Bad Cases

- Good: edit the Series overview while preserving every Episode overview and file binding.
- Base: without history, start at the first regular-season episode; use specials only when no regular season exists.
- Bad: use representative Episode metadata as Series content, or pass a Series metadata ID to playback.

### 6. Tests Required

- `TestMediaSeriesDetailOwnsMetadataAndUserScope`: canonical metadata, visibility, edit isolation, favorite identity and user-scoped history. Requires `MEDIASTATION_TEST_POSTGRES_DSN`; a skip is not database validation.
- `TestMediaSeriesDetailSupportsEveryAttachmentLevel`: Series-only fixture before any Season/Episode exists, then Season and Episode attachments, library restrictions and rejection of metadata IDs as concrete file IDs.
- `node web/scripts/check-series-detail.mjs`: deduplication, URL selection, specials, resume and concrete-version identity.
- Browser checks: refresh/back restoration, version failure isolation, mobile overflow and theme contrast.

### 7. Wrong vs Correct

- Wrong: loop through all episode files and save the Series edit payload to each.
- Correct: submit one visible file ID with `scope: "series"`, resolve its canonical Series, and leave episode/file linkage untouched.
- Wrong: reuse Episode-only search representative eligibility to decide whether Series detail exists. Correct: validate the current file, resolve its existing parent chain, and project the Series record directly.

## Direct source presentation

- `metadata_items` owns stable identity, hierarchy, library membership and
  checkpoints. Basic title/overview/date/year/rating fields are read-only DTO
  fields; current schema does not create those columns.
- `metadata_presentations` merges local TMDb first and Douban only for missing
  values. TMDb and Douban ratings remain independent. `metadata_artwork_presentations`
  reads center asset relationships. There is no old-value or manual-copy fallback.
- Ordinary editing corrects provider bindings only. Parent rebinding locks the
  hierarchy in ancestor order, checks the observed revision, detaches descendants'
  old source identities and resets checkpoints while retaining local IDs/state.
- Retired artwork/credit copy tables are absent from AllModels. Source tables,
  image assets, translations and public person identity still have responsibilities.
- Identity-only qualification/count/hierarchy queries use identity tables.
  Load source projections only for display-dependent filters/sorts and final
  candidates. Verify actual PostgreSQL plans, not query counts alone.
- Persistent metadata-index jobs refresh search and invalidate caches, with
  revision-safe completion and retries. They do not copy presentation data.
- Current schema only: no startup import, legacy projection, old-image-key
  conversion or historical task compatibility. Old development schemas must be
  recreated separately; development verification never mutates a user's database.
