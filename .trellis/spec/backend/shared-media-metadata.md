# Shared Media Metadata Contract

## Scenario: Explicit TMDb Metadata Refresh

### 1. Scope / Trigger

- The detail-page action `刷新tmdb信息` refreshes only its current canonical entity.

### 2. Signatures

- Admin-only `POST /api/metadata/:id/tmdb/refresh`, without a request body.
- `ScraperService.RefreshMetadataTMDb(ctx, metadataID) error`.

### 3. Contracts

- Read the unique same-kind TMDb identifier from metadata, never Media scan hints.
- Always fetch fresh details, regardless of existing snapshots or catalog checkpoints.
- Validate the returned entity ID; Season/Episode coordinates come from canonical parents.
- Update the current entity's display fields, loaded credit scopes, managed TMDb images,
  and raw snapshot. Preserve identity, other provider identifiers, NSFW, and hierarchy.
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

### 1. Scope / Trigger

- Apply this contract when changing TMDb/NFO credit collection, shared people,
  Emby People responses, people backfill, or AI localization.
- `Person` owns shared identity, `PersonIdentifier` owns provider identity, and
  `MetadataCredit` owns the relationship between one person and one work.

### 2. Signatures

- Shared identity: `Person{Name, OriginalName, NormalizedName, Overview, ProfileURL, Source}`.
- Provider identity: `PersonIdentifier{PersonID, Provider, ExternalID}` with
  uniqueness on `(provider, external_id)`.
- Work relationship: `MetadataCredit{MetadataID, PersonID, Type, OriginalRole, Role, SortOrder}`.
- Supported types: `Actor`, `GuestStar`, `Director`, and `Writer`.
- Provider-neutral scrape payload: `PersonCredit` plus `LoadedCreditTypes`.
- Backfill API: admin-only `POST /api/libraries/:id/people-backfill`.
- Translation cache identity: `(kind, context_key, source_text, target_language, prompt_version)`.

### 3. Contracts

- TMDb people are reused only through TMDb person IDs. NFO-only people are
  reused by normalized local name and are never merged into TMDb people by
  name alone.
- A loaded credit type is an authoritative snapshot, including an explicitly
  empty snapshot. A type absent from `LoadedCreditTypes` must preserve existing
  rows. NFO may fill only a provider-loaded type whose provider snapshot is empty.
- Credit replacement is transactional and idempotent. `MetadataCredit` uses
  `PermanentBase`; removed relationships are physically deleted, while an
  unchanged source role preserves its translated display role. Metadata graph
  merge moves and deduplicates credits before deleting the source metadata.
- Movie, Series, Season, and Episode expose only their own ordered credits.
  An empty entity-owned credit type stays empty and never inherits an ancestor.
- Credit source/display roles and translation source/display values use `text`;
  existing PostgreSQL columns must be upgraded explicitly during migration.
- Credit persistence is independent from translation. The configured periodic
  sweep discovers untranslated rows and processes at most 1,000 deduplicated
  translation groups per execution.
- Person-name translation carries up to three related works. Role translation
  carries the current title, original title, year, and media kind. Requests use
  Responses API without tools and contain at most 100 unique entries per batch.
- Cache hits apply without an AI request. A successful request that returns no
  valid Chinese text for an entry saves an empty negative cache under the same
  prompt-versioned identity; later sweeps skip its request and write-back.
  Writes update a target only while its original and display values still equal
  the request snapshot.
- AI request failure, malformed top-level output, or timeout never saves a
  cache, rolls back, or fails the authoritative metadata scrape. Pending rows
  remain for a later periodic sweep.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| TMDb person ID already exists | Reuse its `Person`; do not create a name-based duplicate |
| NFO name matches a TMDb person | Keep separate identities unless an explicit external ID connects them |
| Loaded type has no credits | Remove stale credits of that type |
| Type is not loaded | Preserve existing credits of that type |
| Provider type is non-empty and NFO also has rows | Keep provider rows; do not union guessed identities |
| Season/Episode has no credit rows for one type | Keep that type empty; do not query an ancestor |
| AI is disabled/unconfigured or request fails | Keep original display values and let scraping succeed |
| AI request succeeds but one entry has no valid Chinese result | Keep its original display value and negative-cache the entry until the prompt version changes |
| Original changes while AI request is running | Reject the stale conditional write |
| Provider role exceeds 255 characters | Persist and cache it without truncation |

### 5. Good / Base / Bad Cases

- Good: repeated TMDb scrape reuses the same person ID and replaces only the
  loaded credit types while preserving translated display values whose source
  text did not change.
- Good: Episode guest stars and crew come only from the Episode provider
  response; Series actors remain on the Series.
- Base: AI localization is disabled; original people and role values remain
  fully usable through Emby.
- Bad: merge people solely because normalized names match across local and TMDb.
- Bad: call AI before committing credits, or overwrite a re-scraped role with a
  response generated for its previous original value.
- Bad: let translation failure roll back authoritative credit persistence.

### 6. Tests Required

- Repository: provider-ID reuse, local-name isolation, loaded-type replacement,
  empty snapshots, hard deletion without tombstones, merge deduplication,
  translated-role preservation, and long roles.
- Provider/NFO: movie/Series cast and crew, Episode guest stars and crew, and
  provider-empty versus provider-missing type behavior.
- Emby: item `People`, no Season/Episode inheritance, Persons pagination/search,
  person detail, image proxy, uppercase/lowercase routes, and ID filtering.
- Backfill: only metadata without current credits and with usable TMDb identity;
  verify task progress and request-independent service context.
- Translation: scheduled-only discovery, cache hit without AI, 100-entry
  splitting, a 1,000-group execution limit with pending remainder, work context,
  valid Chinese filtering, invalid-result negative caching, cancellation, and
  stale-write rejection.
- PostgreSQL: assert role/cache fields resolve to `text`; when a test DSN is
  available, migrate legacy `varchar(255)` columns and round-trip a long role.

### 7. Wrong vs Correct

```go
// Wrong: provider and local identities can collide on a common name.
person := findOrCreateByNormalizedName(input.Name)

// Correct: stable provider IDs own remote identity; local names stay local.
person := findOrCreateByProviderID(input.Provider, input.ExternalID)
```

```go
// Wrong: a late AI result can overwrite refreshed provider data.
db.Model(&credit).Update("role", translated)

// Correct: apply only to the unchanged source/display snapshot.
db.Model(&credit).
    Where("original_role = ? AND role = ?", original, original).
    Update("role", translated)
```

## Scenario: Canonical Metadata and Managed Artwork

### 1. Scope / Trigger

- Apply this contract whenever scanner, scraper, media queries, playback, Emby, search, or artwork storage changes.
- `Media` owns playable-file facts and unresolved scan hints. `MetadataItem` owns shared display metadata. `ArtworkAsset` owns persistent image bytes.
- This is a fresh-install schema. Do not add legacy field dual writes or historical backfills unless a separate migration task requires them.

### 2. Signatures

- Canonical entities: `MetadataItem{Kind, ParentID, SeasonNum, EpisodeNum, Title, ..., NSFW, Source}`.
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
- Artwork response: `/api/artwork/:assetID`; originals live under `App.DataDir/artwork/sha256/...`.
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
- `TMDbProvider.GetMovieMatch` and `GetTVMatch` mark their `Match` as containing
  complete TMDb details. Persistence saves languages, countries, and genres
  from that match without issuing a duplicate `GetDetails` request. Search-only
  matches remain unmarked and still fetch extended details once.
- Before regular provider scraping, an existing media link is revalidated by the current provider identifiers. If the exact movie or `Series -> Season -> Episode` metadata resolves to the same canonical ID, the media is marked `scrape_status=matched` without provider calls or writes to `metadata_items` and related canonical tables. `IncludeMatched` and `RefreshWeakMatched` bypass this shortcut.
- Series and Season metadata only locate an existing Episode; a newly discovered Episode is scraped independently. An existing Episode with a generated placeholder title (for example `第 1 集` or `Episode 1`) is reusable for seven days after `metadata_items.updated_at`; after that it must be scraped again. Missing or incomplete episode hierarchy always continues through provider scraping.
- Provider no-match may import existing NFO and sidecar images. Provider error or timeout must set an error state and must not fall back to local metadata.
- Movie and series identifiers include `EntityKind`; equal numeric IDs across kinds or providers must not collide.
- Season rows require a parent Series, `season_num >= 0`, and `episode_num = 0`. Episode rows require a parent Season, `season_num = 0`, and `episode_num > 0`. Movie and Series rows have no parent or episodic position.
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
- A mapped readable regular file uses the existing local `Probe` path without remote
  delay. Invalid/unmatched rules and unavailable mapped files preserve the
  original URL, delay, and `ProbeHTTP` behavior. Source validation rereads the
  mapping after ffprobe and before opening the persistence transaction; do not
  query the regular setting repository from inside that transaction.
- A successful full probe atomically upserts the complete document and its typed summary after rechecking the source identity. Projection first clears every typed summary value, then derives all fields from the current document and local target identity so absent values become unknown instead of retaining stale facts. Scanner, scraper, organizer, and playback never persist technical facts on `media`. Failed, partial, or stale probes must not replace the previous valid complete document, and probe failure must never fall back to `ffmpeg -i`.
- An ffprobe execution failure may include a bounded single-line stderr summary
  after replacing its exact local path or remote URL and applying task-log URL
  sanitization. It retains the original execution error for classification and
  never exposes a signed query or grows one task detail without a fixed limit.
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
  share the same library, type, search, NSFW, and user-visibility predicates.
- Series and Season scope is derived through visible
  `Episode -> Season -> Series` metadata hierarchy, and Episode versions are
  collapsed by Episode Metadata ID before counts or pagination.
- After a logical item is selected, item detail and PlaybackInfo enumerate all
  sibling Media versions visible to the current user, including versions in
  separate physical libraries. Every sibling query must reapply allowed and
  hidden library plus NSFW filters.
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
- Series, Season, and Episode image projections are entity-owned. Series uses
  poster/backdrop, Season uses poster, and Episode uses still; MediaView, Emby
  payloads, image routes, and virtual artwork caches must not fall back to an
  ancestor.
- Scanner, scraper, metadata edit, and organizer metadata flows only read NFO/poster/fanart/thumb sidecars. They must not create, overwrite, move, or delete them.
- Scan and scrape flows must not move, rename, delete, deduplicate, or reclassify playable media files or change their library/path placement. Only an explicit organize operation may invoke `ReclassifyMisclassifiedMedia` or other filesystem transfer helpers.
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
| User cannot view NSFW/library | Filter in `MediaView` query before pagination or playback response creation |
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
- Good: two files for the same series/season/episode link to one Episode whose parent is a real Season whose parent is the Series; the Season can be favorited independently.
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
- Bad: copying provider title, genres, NSFW, or artwork URL into each `Media` row.
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
- Visibility: shared `NSFW` must hide list, search, detail, and PlaybackInfo results before pagination/response mapping.
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
  `id`, `kind`, `title`, `original_name`, `overview`, `genres`, `nsfw`, and
  derived `library_ids`.
- The active OpenSearch alias is `mediastation_metadata`; versioned concrete
  indexes are built before an atomic alias switch.

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
  only `title` and `original_name`; every normalized term must match, and
  PostgreSQL LIKE metacharacters are escaped literally.
- Non-empty queries always ask OpenSearch or PostgreSQL for candidates at
  `offset=0`, capped at 100. PostgreSQL revalidates the candidate IDs and loads
  current title, original name, overview, genres, and year; one shared Go
  comparator sorts the complete candidate set before applying the caller's
  offset/limit. The returned total is the revalidated candidate count and is
  therefore at most 100. Empty-query browsing keeps database pagination and its
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
- Emby `SearchTerm` treats supported `IncludeItemTypes` as an OR set. Movie and
  Series keep the OpenSearch/PostgreSQL media path; Person candidates come only
  from PostgreSQL name/original-name search and never expand to credited works.
  Unsupported types are ignored beside a supported type and return an empty
  envelope when requested alone. Mixed candidates use the shared rank, cap of
  100, and in-memory pagination. No-term hierarchy browsing remains unchanged.
- OpenSearch hits are revalidated through PostgreSQL before ranking and response
  mapping. Stale, invisible, unplayable, or no-longer-matching candidates are
  omitted before the capped total and page are computed.
- Media create, delete, rebind, and library move refresh both old and new
  top-level IDs after commit. Metadata content, parent, and merge changes do the
  same. A full rebuild replays dirty IDs before switching the alias; it does not
  delete the retired Media index.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| OpenSearch alias is missing, incompatible, or the request fails | Use PostgreSQL Metadata search |
| Restricted visibility resolves to no library | Return empty IDs, items, and total |
| Movie loses its last Media | Delete its search document |
| Series loses its last playable Episode Media | Delete its search document |
| Media changes Metadata or library | Refresh old and new top-level projections after commit |
| OpenSearch returns a stale/invisible ID | Omit it during PostgreSQL revalidation |
| Non-empty search has more than 100 backend matches | Rank and expose only the selected 100 candidates; total is capped at 100 |
| Search is `44` or `四十四` | Match both complete numeric forms; do not match `四十` as a numeric alias |
| Emby player sends a one-character term such as `古%` | Normalize it to `古`; keep standalone or multi-character `%` terms literal |
| Search requests `Person,Movie` | Return matching Person and Movie items in one ranked page; do not return unrelated credited works |
| Search requests `Person,MusicAlbum` | Ignore unsupported MusicAlbum and return matching Person items |
| Requested offset is outside the candidate set | Return an empty page with the capped total |
| Search requests only Season/Episode | Return an empty Emby search envelope |

### 5. Good / Base / Bad Cases

- Good: two 1080p/4K Media versions produce one Movie hit and consume one page
  slot; one Series with many Episodes also produces one Series hit.
- Good: `死神2` outranks a less relevant `新死神10`; equal-relevance
  `死神10`, `死神9`, and `死神2` use descending title numbers.
- Good: Yamby/Emby sends `古%` after one-character input and receives the same
  results as `古` without changing other literal `%` searches.
- Good: `Person,Movie` search for an exact person name ranks that Person ahead
  of a containing movie title without expanding the person's credits.
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
  no-Media exclusion, unresolved-Media exclusion, library visibility, NSFW,
  token-group filtering, capped total, ordering, and in-memory pagination.
- Pure ranking tests assert canonical 0–100 conversion, whole numeric runs,
  strict tier order, relevance before title number, number/year/ID tie-breaks,
  irrelevant single-token exclusion, and safe page boundaries.
- Synchronization tests assert create, last-Media delete, rebind, library move,
  Metadata parent change, and graph merge refresh every affected top-level ID.
- Emby tests assert Movie/Series/Person OR results, unsupported-type ignoring,
  no credit expansion, no-term browse stability, Season/Episode empty search,
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
  `MetadataID`; the query reapplies NSFW plus allowed and hidden library filters.
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
- Web links hide the raw ID, use compact provider monograms plus distinct
  status icons, and expose the provider/status through `title`, `aria-label`,
  and screen-reader text. Rating is always visible and uses `-` when non-positive.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Provider ID is absent | Omit that provider link |
| Provider snapshot is absent | Return/show `missing` with an empty-circle icon |
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

## Scenario: Douban Movie Secondary Enrichment

### 1. Scope / Trigger

- Apply when persisting a Movie with a Douban ID, running historical Douban
  Movie enrichment, manually enriching a Movie or Series, storing provider
  snapshots, or editing canonical metadata.
- The periodic flow remains Movie-only. The administrator single-item flow
  supports `MetadataKindMovie` and canonical `MetadataKindSeries`; Season and
  Episode never enter this flow.

### 2. Signatures

- Raw detail carrier: `Match.RawJSON []byte`.
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
  `UpsertProviderSnapshot(ctx, metadataID, provider, payload, fetchedAt)` and
  `UpsertDegradedProviderSnapshot(ctx, metadataID, provider, payload, fetchedAt)`.
- Snapshot identity is `(metadata_id, provider)`, payload storage is JSONB,
  and `degraded` is a non-null boolean whose default is `false`.
- Candidate discovery:
  `ListDoubanMovieEnrichmentAfter(ctx, afterID, refreshBefore, limit) []DoubanMovieEnrichmentCandidate`.
- Candidate artwork:
  `MetadataArtworkCandidate{MetadataID, AssetID, ArtworkType, SourceProvider, SourceURL}`
  is unique on `(metadata_id, artwork_type, source_provider)`.
- Periodic job: `douban_movie_enrichment`; settings
  `metadata.douban_movie_enrichment_enabled` (default `false`) and
  `metadata.douban_movie_enrichment_interval_seconds` (default 86,400).
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
  already own exactly one `(douban, movie)` identifier; a manual Series must
  own exactly one `(douban, series)` identifier. No search, title guess,
  cross-kind ID, or ambiguous identifier is accepted.
- TMDb remains primary. Douban fills only empty canonical overview,
  original-name, rating, year, release-date, languages, countries, and genres.
  Title is the sole precedence exception: a Chinese Douban title may replace a
  non-Chinese title, preserving the old title as original name when needed.
  Source, NSFW, existing non-empty fields, and provider IDs stay unchanged.
- A degraded subject response only fills empty canonical fields. It never uses
  the Chinese-title precedence exception to replace a non-empty title and never
  clears a field omitted from the subject payload.
- If both sources provide a TMDb ID and it conflicts with the canonical TMDb
  identifier, skip the entire Douban write, including snapshot and artwork.
- A historical candidate always starts with one Movie detail request. Save a
  complete or degraded valid response and advance `fetched_at` only after field
  and artwork persistence succeed. A successful permission fallback advances
  the cursor and continues the batch; request, parsing, field, artwork, or
  snapshot failures do not advance the cooldown.
- Canonical graph merge moves provider snapshots and artwork candidates to the
  surviving metadata; when the same provider/candidate key already exists, the
  surviving target row wins before the source metadata is hard-deleted.
- A Douban poster is downloaded immediately to managed local artwork and saved
  as a provider candidate whose `SourceURL` is the normalized official large
  image URL. A configured CDN is a temporary `ImageProxy` transport only.
  Existing selection always wins; only an absent selection is atomically
  promoted. Public responses continue to use only `/api/artwork/:assetID`,
  never a remote URL.
- Historical passes exclude every explicit `degraded=true` Douban snapshot.
  Otherwise, they admit movies with one Douban Movie identifier when no
  snapshot exists, or when the snapshot is older than 24 hours and is a legacy
  wrapper, lacks mobile `intro` / image fields, lacks Douban-owned local poster
  artwork, or canonical data lacks overview or a Chinese title. Other empty
  fields are filled only during such a request and never trigger one alone.
  Passes use metadata-ID keyset pagination, a maximum batch of 20, serial
  processing, a two-second inter-item delay, and a persisted cursor. They never
  join `media`. A retryable upstream failure stops the current execution before
  the failed item's cursor write and before the short-page cursor reset; the
  next execution therefore retries that same item first. Explicit not-found and
  local permanent ambiguity advance the cursor and continue. Metrics and logs
  distinguish requests, field updates, new posters, degraded snapshots,
  snapshot-only refreshes, permanent failures/skips, and upstream pauses.
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
| Periodic candidate is not a Movie, or manual target is not a Movie/Series | Skip without provider or artwork I/O |
| Movie has zero or multiple Douban Movie IDs | Skip as ambiguous |
| Series has one Douban Series ID and is manually retried | Request `/tv/{id}` without touching the Movie batch cursor |
| Movie has one Douban ID and no snapshot | Fetch details once, then store the full response |
| Movie/TV detail returns HTTP 403 or JSON code `1000` | Request `/subject/{id}` once and persist a successful response with `degraded=true` |
| Movie/TV detail returns 404, 429, 5xx, a network error, or another JSON error | Preserve not-found/retryable semantics and do not request subject |
| Subject fallback fails | Persist no new fields, artwork, or snapshot; preserve the subject error classification |
| Periodic candidate has `degraded=true` | Exclude it regardless of age, payload shape, artwork, or missing fields |
| Manual retry gets a complete Movie/TV response | Replace the payload and clear `degraded` atomically |
| Manual retry is still permission-restricted | Refresh the subject payload and keep `degraded=true` |
| Snapshot is less than 24 hours old | Make no provider request |
| Stale current snapshot, Douban poster, overview, and Chinese title are complete | Make no provider request |
| Stale snapshot is legacy or lacks intro/image/Douban artwork | Refresh and fill only missing canonical data |
| Mobile detail fails during regular scraping but abstract succeeds | Save the wrapper as partial and retry only after 24 hours |
| Mobile detail has a retryable failure during batch enrichment | Stop the batch before the current cursor write; request no later candidate |
| The next batch runs after a retryable failure | Retry the same candidate first |
| Mobile detail explicitly reports not found | Record a permanent failure, advance that candidate, and continue |
| Single-item enrichment has a retryable upstream failure | Return HTTP 429 without changing the batch cursor |
| Refresh or persistence fails | Preserve the previous `fetched_at` |
| Detail contains unknown fields | Preserve them in the valid JSONB document |
| Detail TMDb ID conflicts with canonical TMDb ID | Skip snapshot, fields, and artwork |
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
  degraded; periodic Movie enrichment then skips it until an administrator
  manually obtains a complete response.
- Base: all three trigger fields are complete, or the latest successful check is
  less than 24 hours old; no network request is made.
- Bad: query Douban by title, periodically scan Series, treat every upstream
  failure as permission denial, infer degraded state from missing fields,
  overwrite a non-empty field/current image, expose a remote image URL, or scan
  history with offset pages.

### 6. Tests Required

- Provider unit: Movie/TV URL and Referer, regular abstract fallback, exact
  HTTP 403 / JSON code `1000` subject fallback, non-permission no-fallback,
  subject-failure classification, typed retryable/not-found errors, nested
  rating/image, episode count, source, IDs, projected lists, and unknown raw
  fields survive detail parsing; normal persistence makes one detail request.
- Repository/PostgreSQL: Movie-only unique-ID keyset discovery, ambiguity skip,
  no-snapshot admission, 24-hour exclusion, stale incomplete admission,
  complete exclusion, explicit degraded exclusion, complete-upsert degraded
  clearing, candidate uniqueness, and atomic selection preservation/promotion.
- Service: complete and degraded fill-only rules, Chinese-title replacement,
  subject failure without a snapshot, Movie/Series kind boundaries, TMDb
  mismatch rejection, complete recovery, failure without cooldown advancement,
  degraded continuation, snapshot-only logging, retryable stop/retry cursor
  behavior, permanent-not-found continuation, and bounded cursor progress.
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
repo.UpsertProviderSnapshot(ctx, metadata.ID, "douban", subject.RawJSON, fetchedAt)

// Correct: reuse the accepted response, fill gaps, and persist its explicit
// quality state only after field/artwork writes succeed.
fillMissingDoubanFields(ctx, metadata.ID, metadata.Kind, detail, degraded)
repo.SaveCandidate(ctx, metadata.ID, "poster", "douban", source, asset)
if degraded {
    repo.UpsertDegradedProviderSnapshot(ctx, metadata.ID, "douban", detail.RawJSON, fetchedAt)
} else {
    repo.UpsertProviderSnapshot(ctx, metadata.ID, "douban", detail.RawJSON, fetchedAt)
}
```

## Scenario: Durable TMDb Catalog Hydration

### 1. Scope / Trigger

- Apply when changing TMDb discover persistence, canonical TV hierarchy,
  provider snapshots, catalog jobs, or Series/Season/Episode artwork.

### 2. Signatures

- Canonical item checkpoints:
  `MetadataItem{RuntimeSec, CatalogMetadataHydratedAt, CatalogArtworkHydratedAt, CatalogHydratedAt}`.
- Raw response: `MetadataProviderSnapshot{MetadataID, Provider, Payload JSONB, FetchedAt}`.
- Durable work: `CatalogHydrationJob{Provider, EntityKind, ExternalID, MetadataID, Status, Stage, Attempts, NextAttemptAt, LastError}`.
- Queue boundary:
  `QueueCatalogHydrationContext(context.Context, []ExternalMediaResult) error`.
- Episode display ownership:
  `MetadataItem{Title, OriginalName, Overview}` contains only the Episode's own
  values; `MediaView{SeriesID, SeriesTitle, SeasonID}` projects hierarchy
  context separately. There is no persisted or serialized `episode_title`.
- Legacy title migration: non-empty `metadata_items.episode_title` values are
  trimmed into Episode `title`, then the legacy column is dropped in the same
  PostgreSQL transaction.
- Snapshot backfill boundary:
  `ListProviderSnapshotsAfter(context.Context, provider, kinds, afterID, limit)`
  keyset-pages provider snapshots and their owned metadata.
- Artwork diagnostics:
  `ListMissingCatalogArtworkAfter(context.Context, afterID, limit)` keyset-pages
  TMDb-owned Movie/Series/Season/Episode metadata and reports the applicable
  missing poster, backdrop, or still flags without reading `media`.
- Missing-image state:
  `MetadataArtworkRecheck{MetadataID, ArtworkType, LastNoImageAt}` is unique per
  metadata/type and records only a successful TMDb response with no owned image.
- Local repair boundary:
  `ListTMDbArtworkSelectionsAfter(context.Context, afterSelectionID, limit)`
  keyset-pages current TMDb selections. A repaired asset replaces the selection
  only while its ID, asset, provider, and source URL still match the snapshot.
- Missing-image recheck boundary:
  `ListTMDbArtworkRecheckMetadataAfter(context.Context, afterMetadataID, limit)`
  admits metadata with an artwork checkpoint or per-type state and excludes
  never-hydrated metadata with neither.
- Periodic jobs: `tmdb_artwork_local_repair` and
  `tmdb_artwork_missing_recheck`; both are disabled by default with independent
  24-hour schedules. Each execution keyset-pages all current candidates without
  a persisted cursor or a total scan/request cap.

### 3. Contracts

- Discover requests synchronously upsert only valid TMDb Movie/Series jobs and
  then return; provider details, credits, profile images, and artwork run in one
  service-lifetime worker.
- The database job keyed by `(provider, entity_kind, external_id)` is queue
  truth. A wake channel only shortens sleep. Startup changes `running` jobs to
  retryable work.
- Series details create every Season shell, including Season 0. One seasons
  turn hydrates one Season and all Episodes listed by that Season response,
  then yields to another job.
- Season and Episode shells keep `Overview` empty. Only the entity's own detail
  response may populate it during full hydration; inventory summaries and
  ancestor descriptions are never copied into child metadata.
- Every TMDb Series, Season, and Episode stores its own TMDb identifier, typed
  display fields, credits, complete provider response JSONB, and selected
  original image bytes. Episode runtime is seconds on `MetadataItem`.
- Episode `Title` stores the best title for that Episode. The Series name is
  exposed only through the real parent hierarchy as `MediaView.SeriesTitle`;
  Episode `OriginalName`, `Overview`, identifiers, and artwork never inherit
  from Series or Season. NFO/scanner episode-title hints are internal input and
  must be normalized into `MetadataItem.Title`, never serialized as a second
  API field.
- Startup compatibility migration copies each non-blank legacy Episode
  `episode_title` into `title` before dropping the column. A blank legacy value
  preserves the existing title, non-Episode rows are unchanged, and repeated
  startup skips the absent column.
- Season/Episode title and overview localization considers only that entity's
  TMDb response and translations. Prefer a concrete top-level Chinese value,
  then `zh-CN`, `zh-SG/HK/TW`, other Chinese, `en-US`, and other translations.
  Generated labels such as `Episode 1`, `第 1 集`, `Season 1`, and `Specials`
  do not beat a concrete translation; generate a numbered fallback only when
  no concrete entity-owned title exists.
- Worker startup keyset-pages TMDb Season/Episode JSONB snapshots in batches
  and reapplies the same localization rules without network requests. It
  updates only `source=tmdb` metadata, never manual metadata, and skips writes
  when the owned display fields are already current. A malformed or failed
  individual snapshot is logged by metadata ID and skipped so later rows and
  pages still run; list-query or context errors terminate the pass.
- Own metadata and own artwork checkpoints advance independently. Episode full
  completion requires both; Season waits for all expected Episodes; Series and
  its job wait for all expected Seasons. Explicitly absent image paths satisfy
  artwork completion, while a failed download does not.
- A Catalog artwork retry removes only that source URL's fresh image-proxy
  failure marker before importing. It preserves successful cached bytes and
  does not change the generic browser proxy's negative-cache behavior.
- Provider/log/job errors contain endpoint/entity/status only. Never persist or
  log request URLs, query strings, API keys, headers, signed image URLs, or
  credentials.
- Catalog-only metadata creates no `Media` and stays outside physical Emby
  library membership until a visible Media links to it.
- Artwork completeness is entity-owned: Movie/Series require poster and
  backdrop, Season requires poster, and Episode requires still. A candidate
  must have its own valid TMDb identifier, an empty artwork checkpoint, and a
  missing selection-to-asset join. Parent fallback images never satisfy it.
- Artwork repair jobs read only the canonical metadata graph and never create,
  claim, revive, or wake `catalog_hydration_jobs`. First-time artwork hydration
  remains owned by the catalog ingestion worker.
- Catalog image persistence is insert-if-absent. An existing valid selection
  from any source wins both the pre-download check and the transactional write
  race; explicit manual import keeps overwrite behavior. Metadata editing does
  not accept artwork URLs and cannot clear a selection.
- Local repair scans only current TMDb selections with HTTP(S) source URLs. A
  missing file is downloaded from its saved URL first; only an exact HTTP 404
  permits one TMDb detail lookup. Other HTTP statuses, connection failures,
  timeouts, invalid content, and fresh negative-cache results remain retryable
  failures and preserve the selection.
- Missing-image recheck is per metadata/type with a 24-hour cooldown. Movie and
  Series share one detail request for due poster/backdrop types; Season owns its
  poster and Episode owns its still. A successful empty result advances only
  the affected type; provider or download failure never advances the timestamp.
- Automatic writes use insert-if-absent for an empty selection and conditional
  replacement for a dangling TMDb selection. A concurrent manual/local choice
  always wins. Neither repair job changes catalog checkpoints or non-artwork
  metadata.
- `media`, `metadata_items`, `metadata_identifiers`,
  `metadata_provider_snapshots`, `catalog_hydration_jobs`,
  `metadata_artworks`, `artwork_assets`, and `metadata_credits` use
  `PermanentBase` and hard deletion. Upgrade removes their legacy `deleted_at`
  columns transactionally, rejects referenced metadata tombstones, purges media
  and credit tombstones, restores tombstoned assets as ordinary assets,
  detaches surviving catalog jobs from retired metadata, and never deletes
  image files.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| TMDb ID is non-positive or media type is unsupported | Create no job and send no wake signal |
| Duplicate discover rows | Keep one durable job identity |
| HTTP 429 | Honor valid `Retry-After`; otherwise use bounded, cancelable backoff |
| Worker stops while a job is running | Reset it to retry on next startup |
| Required own image download fails | Preserve the old own selection and leave artwork/full checkpoints empty |
| Catalog retry follows a recent proxy image failure | Retry the upstream URL without deleting a successful cached image |
| Provider explicitly returns no own image | Mark own artwork complete without ancestor fallback |
| One Episode is incomplete | Keep its Season, Series, and durable job incomplete |
| Episode/Season top-level title is a generated label | Continue through its own translations before accepting or generating a fallback |
| Snapshot belongs to manual or non-TMDb metadata | Leave its display fields unchanged |
| Snapshot-localized fields already match | Perform no metadata update |
| One snapshot cannot decode or update | Log that metadata ID and continue with later rows; never log its payload |
| Legacy Episode has a non-blank `episode_title` | Trim it into `title`, then remove the legacy column atomically |
| Legacy `episode_title` is blank or belongs to a non-Episode | Preserve the current `title` |
| Legacy column is already absent | Skip the compatibility migration successfully |
| A saved TMDb image URL returns 404 | Query TMDb for that metadata and image type only |
| A saved image URL returns 403, 429, another 4xx/5xx, or a network error | Preserve the selection and record a retryable failure; do not query TMDb |
| TMDb successfully returns no owned image | Upsert the per-type no-image time and wait 24 hours |
| Metadata has no artwork checkpoint and no per-type state | Exclude it from missing-image recheck |
| A catalog image write races with a manual/local selection | Preserve the manual/local selection; the downloaded asset may remain unselected |
| An automatic repair races with a manual/local selection | Preserve the manual/local selection; the downloaded asset may remain unselected |
| A selected TMDb local file is missing | Preserve the relationship until validated bytes are stored and a conditional replacement succeeds |
| A selected asset path is invalid, unreadable, or fails for a non-missing I/O reason | Record a per-item retryable failure and preserve the selection |
| Legacy soft-deleted metadata still has Media, user-state, credit, event, or active-child references | Abort and roll back the schema migration |

### 5. Good / Base / Bad Cases

- Good: discovering one Series eventually stores `Series -> Season 0/1 ->
  Episode`, each with its own TMDb ID, raw JSON, credits, and original image.
- Good: an Episode stores its localized name in `Title`, while clients receive
  the parent show name from `SeriesTitle`.
- Base: a Series has no Seasons or an entity has no image path; explicit empty
  inventories/scopes complete without synthetic children or inherited images.
- Base: a historical TMDb Episode snapshot is relocalized at startup without a
  provider request; a manual Episode with the same shape remains untouched.
- Bad: an in-memory map is queue truth, a root-only timestamp suppresses child
  hydration, or a Season/Episode copies Series artwork or people.
- Bad: storing the Series name in `Episode.Title` or copying the Series
  `OriginalName`, overview, or provider IDs into an Episode projection.
- Bad: a browser-facing image failure marker suppresses the durable job's own
  scheduled retry for the full negative-cache TTL.
- Good: one Series with many missing Season/Episode images produces one root
  job and later fills only each entity's own image.
- Good: an available saved URL restores its missing local file without a TMDb
  metadata request; a 404 may refresh only that image source.
- Good: a per-type empty TMDb result is retried after 24 hours without admitting
  never-hydrated metadata.
- Bad: join `media`, use offset pagination, enqueue catalog work, or let an
  automatic repair overwrite a manual selection.
- Bad: trust the selection-to-asset join without checking the managed local
  file, or use the remote source URL as the display fallback.

### 6. Tests Required

- Provider: Series/Season/Episode inventories, raw unknown-field retention,
  original image URLs, external IDs, runtime, 429 retry/cancel, and errors with
  no URL/API key.
- Repository/PostgreSQL: JSONB round-trip, job uniqueness, claim/retry/recovery,
  bounded root priority, child completion gating, and idempotent provider-ID
  attachment/merge.
- Service: Season 0 plus all Episodes, partial resume, no Media creation, own
  artwork/profile bytes, image retry past a fresh proxy failure marker, and
  bottom-up completion.
- Localization/backfill: placeholder-title rejection, locale priority,
  entity-owned overview fallback, keyset pagination beyond one batch,
  per-item failure isolation, manual-source protection, and idempotent no-op
  updates.
- Migration/API: backfill a non-empty legacy Episode title, preserve blank and
  non-Episode titles, drop the column idempotently, and omit `episode_title`
  from serialized media payloads.
- MediaView/Emby: Episode-owned title and identifiers, parent `SeriesTitle`, own
  Episode stills, own Season posters/people, no virtual cache fallback, and
  catalog-only exclusion from physical libraries.
- Artwork repair/recheck: PostgreSQL tests cover both keyset scans without a
  `media` table, never-hydrated exclusion, task-one state handoff, per-type
  cooldown, conditional selection races, full-sweep pagination, and exact retired
  setting cleanup. Service tests cover old-URL success without TMDb, exact 404
  detection, one request for multiple due types, and item-local failures.
- Run both artwork queries with `EXPLAIN (ANALYZE, BUFFERS)` on production-scale
  PostgreSQL data before adding any new index.

### 7. Wrong vs Correct

```go
// Wrong: a wake-only in-memory queue loses work on restart.
pending[key] = discoverItem

// Correct: persist the idempotent identity, then coalesce only the wake signal.
repo.Metadata.EnqueueCatalogJob(ctx, "tmdb", entityKind, externalID)
```

```go
// Wrong: Season silently displays the Series poster.
seasonPoster := series.PosterURL

// Correct: missing own Season artwork remains empty.
seasonPoster := metadataArtworkURL(ctx, season.ID, model.ArtworkTypePoster)
```

```go
// Wrong: automatic catalog import overwrites the current selection.
repo.SaveSelection(ctx, metadataID, artworkType, "tmdb", source, asset)

// Correct: automatic work only fills an absent or dangling selection.
repo.SaveCatalogSelection(ctx, metadataID, artworkType, "tmdb", source, asset)
```

```go
// Wrong: overload Episode.Title with hierarchy context.
episode.Title = series.Title

// Correct: keep the Episode title on the entity and project context separately.
episode.Title = localizedEpisodeTitle
view.SeriesTitle = parentSeries.Title
```

## Scenario: TMDb Episode Metadata Recheck

### 1. Scope / Trigger

- Apply when changing the periodic/manual repair of historical Episode title,
  overview, release date, credits, or still artwork.

### 2. Signatures

- Checkpoint: `MetadataItem.TMDbEpisodeCheckedAt *time.Time` maps to indexed,
  nullable `metadata_items.tmdb_episode_checked_at` through the explicit GORM
  tag `column:tmdb_episode_checked_at`; do not rely on acronym inference.
- Candidate boundary:
  `ListTMDbEpisodeMetadataRecheckAfter(ctx, afterID, checkedBefore, limit)`.
- Provider boundary:
  `GetTVEpisodeDetails(ctx, seriesTMDbID, seasonNum, episodeNum)`.
- Scheduler job: `tmdb_episode_metadata_recheck`, default disabled, default 24 hours.

### 3. Contracts

- Candidates are Episode metadata with direct `media`, one valid Series TMDb
  identifier, an expired/null checkpoint, and at least one missing requirement:
  empty/generated title, empty overview, empty release date, or absent valid still.
- One execution keyset-pages every candidate in batches of 200 without a
  persisted cursor or request cap. Manual runs obey the same 72-hour cooldown.
- One provider response supplies non-empty title, overview, release date,
  rating, year, loaded credit scopes, and still. Non-empty provider values may
  overwrite current values; empty values never clear stored values.
- A successful provider and persistence flow writes the checkpoint even if
  required fields remain missing. Provider, credits, artwork, metadata, or
  checkpoint failure leaves it unchanged; already-written partial results remain
  and are retried idempotently.
- Automatic still persistence uses `SaveCatalogSelection` semantics. A
  concurrent manual/local selection wins and counts as a satisfied still.
- Episode is excluded from `tmdb_artwork_missing_recheck` but remains eligible
  for `tmdb_artwork_local_repair` when an existing selected still loses its file.
- Detail logs contain only updates, still-missing results, concurrent skips, or
  failures and identify Series/SxxExx/TMDb ID without paths, URLs, or credentials.
- Startup migration copies `tm_db_episode_checked_at` into the canonical column
  only when the canonical value is null, then drops the legacy column in the
  same transaction. An absent legacy column is an idempotent no-op.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Episode has no direct media | Exclude it |
| Title, overview, release date, and still are complete | Send no provider request |
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
- Base: TMDb still has no overview or still; the successful response advances
  the checkpoint and logs only those remaining gaps.
- Good: upgrading a database with the legacy GORM-derived checkpoint column
  preserves its values and leaves only the canonical indexed column.
- Bad: scan metadata without media, persist a cross-execution cursor, call the
  discovery catalog worker, or overwrite a concurrent manual still.
- Bad: depend on GORM to infer `TMDb` as one acronym or leave both checkpoint
  columns active after migration.

### 6. Tests Required

- PostgreSQL: direct-media filter, four-field completeness, generated title,
  missing release date/still, valid Series identity, cooldown, deduplication,
  keyset pagination, exact canonical schema column, legacy value preservation,
  canonical-value precedence, legacy-column removal, repeated migration, and
  newer-checkpoint merge.
- Service: non-empty overwrite including release date/year, remaining-gap
  detection, checkpoint success/failure behavior, still concurrency, per-item
  failure isolation, sanitized detail logs, and normal no-change silence.
- Scheduler/definition: stable key, default disabled, 24-hour interval, and
  manual action mapping.

### 7. Wrong vs Correct

```go
// Wrong: send Episode repair through catalog discovery and overwrite selections.
queueCatalogHydration(episode)
repo.SaveSelection(ctx, episode.ID, model.ArtworkTypeStill, "tmdb", source, asset)

// Correct: query only repair candidates and let concurrent selections win.
candidates := repo.ListTMDbEpisodeMetadataRecheckAfter(ctx, afterID, checkedBefore, 200)
repo.SaveCatalogSelection(ctx, episode.ID, model.ArtworkTypeStill, "tmdb", source, asset)
```

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
- `Person.ProfileImageKey` is a relative key under `DataDir/people`, such as
  `sha256/ab/cd/<hash>.jpg`.
- `PeopleImageStore.Import(ctx, source) (key, error)` validates and atomically
  stores image bytes.
- `PeopleImageStore.ServePerson(ctx, writer, request, personID)` serves only a
  validated local key and reports whether the ID belongs to a person.
- Public image contract remains `GET /Items/{id}/Images/Primary` and its
  `/emby` and case variants.

### 3. Contracts

- Person bytes live at `App.DataDir/people/sha256/<first-two>/<next-two>/`;
  the key is content SHA-256 based and deduplicates identical bytes.
- Profile downloads happen before credit persistence writes the person and
  bypass `cache/images`; image network errors are warnings and do not fail the
  authoritative scrape.
- A successful refresh replaces the key only after validation and atomic write;
  a failed refresh preserves the prior key and file.
- There is no startup image migration or request-time lazy download. The
  scraper persistence flow is the only remote person-image ingestion path.
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

## Scenario: TMDb Detail Snapshots and One-Time Backfill

### 1. Scope / Trigger

- Apply this contract whenever a Movie, Series, Season, or Episode TMDb detail
  request is added or changed, a manual TMDb identifier is edited, or missing
  provider snapshots are migrated.
- A snapshot is the valid raw JSON returned by a complete TMDb detail endpoint.
  Projected metadata and downloaded artwork are not substitutes for it.

### 2. Signatures

- Snapshot write:
  `MetadataRepository.UpsertProviderSnapshot(ctx, metadataID, "tmdb", payload, fetchedAt)`.
- Strict manual identity write:
  `MetadataRepository.ReplaceIdentifierWithSnapshot(ctx, metadataID, "tmdb", entityKind, externalID, payload, fetchedAt)`.
- Backfill discovery:
  `CountMissingTMDbSnapshots(ctx)` and
  `ListMissingTMDbSnapshotsAfter(ctx, afterID, limit)`.
- Backfill execution:
  `ScraperService.StartTMDbSnapshotBackfill(ctx, automatic)` and
  `BackfillTMDbSnapshots(ctx, progress)`.
- One-time completion setting:
  `internal.tmdb_snapshot_backfill_completed=true`.
- Stable task definition/action/kind: `tmdb_snapshot_backfill`.
- Metrics: integer `processed`, `total`, `succeeded`, `failed`, and `remaining`.

### 3. Contracts

- Every successfully fetched complete TMDb detail writes a valid raw snapshot.
  Movie/Series use their own TMDb ID; Season/Episode requests use the ancestor
  Series ID plus season/episode coordinates and validate the returned entity ID
  when an expected child ID exists.
- Normal scrape and manual match first preserve the accepted canonical match.
  A later optional detail or snapshot failure is best-effort and must not roll
  back that accepted match. Search-page JSON alone is never a detail snapshot.
- Editing a TMDb ID is strict: fetch and validate the complete detail before any
  identity write, then replace the identifier and snapshot in one repository
  transaction. Any detail or snapshot error preserves both prior values.
- A zero TMDb ID removes only the identifier. Existing canonical metadata and
  snapshots are retained. Deleting media, a library root, or a library likewise
  does not delete canonical metadata, identifiers, or snapshots. Metadata graph
  merge keeps its existing snapshot move/deduplication behavior.
- Backfill candidates are all Movie/Series/Season/Episode metadata with a valid
  positive, same-kind TMDb identifier and no TMDb snapshot. Discovery never
  joins `media`; unlinked canonical metadata remains eligible.
- Backfill uses bounded metadata-ID keyset pages and serial provider requests.
  It writes snapshots only, isolates per-item failures, and uses snapshot
  existence as the business checkpoint. Task rows and log text are observability,
  never resume state.
- Automatic startup runs only while the completion setting is absent. Only a
  complete enumeration writes the setting. Cancellation or a fatal count/page
  query leaves it absent; a complete pass writes it even when individual items
  failed. Those failures are retried only by the task-center manual action.
- Automatic and manual runs share one task-kind mutex. They create the persisted
  task execution before background work. The definition exposes current metrics
  while running and the latest terminal metrics afterward; the Web task page
  keeps its existing three-second refresh.
- `remaining` is `max(total - processed, 0)` during the pass and is forced to
  zero after complete enumeration. Failed checked items remain in `failed`, not
  in `remaining`. Provider errors are sanitized before task details or finish.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Raw detail JSON is empty or invalid | Do not write a snapshot |
| Normal scrape optional detail/snapshot fails | Keep the accepted match and log a sanitized warning |
| Manual ID detail fetch or ID validation fails | Reject the new ID; preserve prior identifier and snapshot |
| Manual snapshot database write fails | Roll back the identifier replacement in the same transaction |
| Candidate has no usable ancestor Series ID/coordinates | Count it as one failed item and continue |
| One provider request or snapshot write fails | Increment `processed` and `failed`; continue the pass |
| Count/page query fails | Fail the task and leave the completion setting absent |
| Service context is canceled | Mark the task interrupted and leave the completion setting absent |
| Enumeration completes with failed items | Set completion, force `remaining=0`, and mark the task failed |
| Enumeration completes with no failed items | Set completion and mark the task completed |
| Automatic start sees completion=true | Create no task and make no TMDb request |
| Manual start sees completion=true | Start a new run over the currently missing snapshots |
| Another snapshot backfill is active | Return a conflict; do not create a second execution |

### 5. Good / Base / Bad Cases

- Good: a search-selected Movie is linked immediately, its successful complete
  detail overwrites no unrelated metadata and saves the raw response snapshot.
- Good: a pass processes four kinds, records one failed provider item, writes
  the completion setting, and later exposes only that missing item to manual retry.
- Base: the first upgraded start finds no candidates; it records a zero-metric
  completed task and writes the completion setting.
- Base: a media file is deleted; its canonical metadata, TMDb identity, and
  snapshot remain available for discovery/history reuse.
- Bad: parse task logs as a cursor, join candidates through `media`, or schedule
  a periodic full-library snapshot scan.
- Bad: save a manually entered TMDb ID first and fetch its snapshot afterward.

### 6. Tests Required

- Provider persistence: known-ID and search-detail Movie/Series snapshots;
  Season/Episode snapshots; invalid/empty raw JSON; repeat upsert behavior.
- Repository/PostgreSQL: four-kind candidate count and keyset pages without a
  `media` table, unlinked metadata eligibility, existing-snapshot exclusion,
  and identifier rollback after a snapshot-stage database failure.
- Manual edit: Movie and hierarchical Episode success, provider/JSON failure,
  returned child-ID mismatch, and prior identifier/snapshot preservation.
- Backfill: all four kinds, per-item failure isolation, snapshot-only writes,
  successful checkpoint exclusion, cancellation without completion, complete
  pass with failures, automatic no-rerun, and manual retry after completion.
- Task/API/UI: stable definition, same-kind conflict, current/latest metrics,
  manual action status, three-second refresh, lint, and production build.

### 7. Wrong vs Correct

```go
// Wrong: a partial identity remains when the snapshot write fails.
repo.ReplaceIdentifier(ctx, metadataID, "tmdb", kind, externalID)
repo.UpsertProviderSnapshot(ctx, metadataID, "tmdb", payload, fetchedAt)

// Correct: identifier and valid snapshot are one strict manual transaction.
repo.ReplaceIdentifierWithSnapshot(ctx, metadataID, "tmdb", kind, externalID, payload, fetchedAt)
```

```go
// Wrong: task history is migration state and complete passes rerun at startup.
afterID := latestTask.Metrics["cursor"]

// Correct: snapshot existence checkpoints items; one setting checkpoints the pass.
candidates := repo.ListMissingTMDbSnapshotsAfter(ctx, afterID, pageSize)
```

## Scenario: Web Series Detail Hierarchy

### 1. Scope / Trigger

Series detail separates canonical Series presentation from selected Episode and concrete playback file. Cross-layer reads, favorites and manual edits must preserve these identities.

### 2. Signatures

- `GET /api/media/:id/series` returns `{series, favourite}`.
- `PUT /api/media/:id/series/favorite` accepts `{favourite: boolean}`.
- `GET /api/media/:id/credits?scope=series` reads Series credits.
- Manual metadata updates accept optional `scope: "series"`.
- Library Series listing accepts exact `series_id` or `key`; episode responses include user-scoped `history`.

### 3. Contracts

- Media route IDs are visible concrete file IDs, never the returned Series metadata ID. Series presentation contains its own title, overview, providers and artwork, without representative-file technical fields.
- Series edits update canonical Series metadata once; they must not rewrite Episode titles, coordinates or file linkage. Default movie/episode updates remain unchanged.
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
- `node web/scripts/check-series-detail.mjs`: deduplication, URL selection, specials, resume and concrete-version identity.
- Browser checks: refresh/back restoration, version failure isolation, mobile overflow and theme contrast.

### 7. Wrong vs Correct

- Wrong: loop through all episode files and save the Series edit payload to each.
- Correct: submit one visible file ID with `scope: "series"`, resolve its canonical Series, and leave episode/file linkage untouched.
