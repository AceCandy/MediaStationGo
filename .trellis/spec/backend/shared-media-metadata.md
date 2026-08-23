# Shared Media Metadata Contract

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
- Credit replacement is transactional and idempotent. Metadata graph merge
  moves and deduplicates credits before deleting the source metadata.
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
  empty snapshots, soft-delete restoration, merge deduplication, and long roles.
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
  on success or `❌ <media-id> <sanitized-error>` on failure. Failure details
  never include the media path or remote URL.
- Manual apply API: `POST /api/media/:id/scrape/apply` accepts `ManualScrapeRequest`, persists metadata through `ScraperService.ApplyManualMatch`, then returns the refreshed `MediaView` from `MediaService.GetMedia`.
- Artwork response: `/api/artwork/:assetID`; originals live under `App.DataDir/artwork/sha256/...`.
- Library deletion: `DELETE /api/libraries/:id` -> `MediaService.DeleteLibrary(ctx, id)`.
- Scrape entrypoints (`POST /api/media/:id/scrape`, `POST /api/libraries/:id/scrape`, manual apply, scan auto-scrape, STRM refresh, and repair-rescrape) enrich metadata only and never invoke `OrganizerService`.
- Regular scrape entrypoints never infer an adult code or call `AdultProvider`. `ScraperService.AnyEnabled` reports regular provider availability and excludes the adult provider.
- Adult network lookup requires an explicit adult operation: manual search whose provider set contains `adult`, manual apply with `source=adult`, or organize with `mediaType=adult`. Manual search with an empty provider or `provider=all` is not an explicit adult operation.

### 3. Contracts

- Scanner resolves exact provider identifiers before writing media but never
  creates local metadata. An exact movie identity binds the Movie; an episodic
  identity binds only when its stored `Series -> Season -> Episode` hierarchy
  already contains that episode.
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
- Only a `Media` whose path ends in `.strm` may
  apply `ffprobe.path_mappings`. Rules require a credential-free HTTP(S)
  prefix with a host and an absolute local prefix. Matching compares scheme,
  host, and complete decoded URL path segments; query and fragment values
  never enter the local path. The longest matching URL path wins, and the
  joined path must remain below its configured local prefix.
- A mapped readable file uses the existing local `Probe` path without remote
  delay. Invalid/unmatched rules and unavailable mapped files preserve the
  original URL, delay, and `ProbeHTTP` behavior. Source validation rereads the
  mapping after ffprobe and before opening the persistence transaction; do not
  query the regular setting repository from inside that transaction.
- A successful full probe atomically upserts the complete document and its typed summary after rechecking the source identity. Projection first clears every typed summary value, then derives all fields from the current document and local target identity so absent values become unknown instead of retaining stale facts. Scanner, scraper, organizer, and playback never persist technical facts on `media`. Failed, partial, or stale probes must not replace the previous valid complete document, and probe failure must never fall back to `ffmpeg -i`.
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
| Web PlaybackInfo targets a visible pending media | Return raw file identity plus probe technical fields; do not add it to normal MediaView lists |
| Duplicate report has no probe summary | Return `size_bytes=0`; do not serialize legacy technical fields or the complete Media model |
| Media has a non-null unknown metadata ID | Database foreign-key rejection |
| Metadata-owned user state has an empty/missing metadata ID | Database NOT NULL/CHECK/foreign-key rejection |
| A new provider ID is unowned | Attach it to the current metadata; replace a stale ID for the same provider/kind |
| Provider identifiers resolve to different metadata rows without explicit merge authority | Reject without changing either metadata graph |
| Explicit provider crosswalk or user-confirmed identity resolves to another metadata | Transactionally merge references and hierarchy, then hard-delete the unreferenced source |
| Provider returns no match | Try read-only local fallback; otherwise set `no_match` |
| Provider request fails | Set `error`; preserve existing canonical data and do not import local fallback |
| A regular scrape path or `provider=all` query resembles an adult code | Do not call `AdultProvider`; continue regular external-ID and provider lookup |
| An explicit adult manual/organize operation has a valid code | Allow `AdultProvider` lookup and persist the selected adult match normally |
| Artwork import fails | Return the error and keep the currently selected managed asset |
| Provider metadata implies a different category/library | Persist metadata and artwork only; preserve the media path and library ID |
| User cannot view NSFW/library | Filter in `MediaView` query before pagination or playback response creation |
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
| Track probe mapping is invalid, unmatched, escapes its local prefix, or maps to an unavailable file | Preserve the original URL and use the existing delayed remote probe |
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
- Bad: calling `ReclassifyMisclassifiedMedia` after a scrape and silently moving or deleting a local media/STRM file.
- Good: a two-version local STRM item schedules both target files, persists each document and typed summary to its probe row, and exposes matching target container/path/name/bitrate.
- Good: a signed remote STRM URL maps by its decoded path to a mounted local
  file; the query signature is ignored and the local probe starts immediately.
- Base: no track probe mapping matches, or the mounted file is temporarily
  unavailable; the existing delayed remote probe remains usable.
- Bad: reuse or reverse `playback.path_mappings`, apply a track mapping to a
  non-STRM media row, or build a local filename from URL query values.
- Good: task-center global backfill repairs missing documents across libraries,
  skips valid documents and soft-deleted media, and keeps
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
- Scanner fingerprint: assert unchanged `.strm` and ordinary files skip without
  touching `updated_at`; NFO-only changes still skip; size/mtime changes update
  with the matching reason; a zero legacy mtime is backfilled once.
- Identity: allow equal external IDs across provider or entity kind; deduplicate equal canonical identities.
- Identity: replace a stale unowned identifier for the same provider/kind, preserve other provider identifiers, and reject an occupied identifier unless merge is explicitly authorized.
- Merge: move multiple media versions, favorites, playlists and history; recursively merge Series children; assert duplicate user state is resolved and source metadata is physically gone.
- Query: add multiple identifiers for one metadata/provider/kind and assert media count, page length, and order remain unchanged; assert the generated query correlates identifier reduction to the current metadata and contains no global identifier `GROUP BY`.
- Visibility: shared `NSFW` must hide list, search, detail, and PlaybackInfo results before pagination/response mapping.
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
  longest-prefix selection, decoded path joining, query exclusion, traversal
  rejection, STRM-only gating, unavailable-file remote fallback, local runner
  selection, and source rejection after a mapping change.
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
- TMDb request reuse: known movie/Series IDs make one detail request and retain
  languages, countries, and genres; search-only matches make one search plus one
  extended-details request and retain the same fields.
- Scrape provider boundary: assert regular enrichment, `provider=all` manual search, and non-adult organize make zero adult-provider requests; retain positive coverage for explicit adult manual search and adult organize.
- Manual apply API: assert the response contains the newly persisted shared title while the media path and library ID remain unchanged.
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
    Joins("JOIN metadata_items AS mi ON mi.id = m.metadata_id AND mi.deleted_at IS NULL").
    Joins(`LEFT JOIN LATERAL (
        SELECT MIN(CASE WHEN mid.provider = 'tmdb' THEN mid.external_id END) AS tmdb_external_id
        FROM metadata_identifiers AS mid
        WHERE mid.metadata_id = mi.id AND mid.entity_kind = mi.kind AND mid.deleted_at IS NULL
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
- Final rank tiers are exact title/original match, ordered full containment in
  one title/original field, then other all-token matches. Exact matches sort by
  year and Metadata ID. The other tiers sort by shared field coverage, match
  position/span, the last valid 0–100 title number descending, year descending,
  and Metadata ID. OpenSearch `_score` only selects its finite candidate set;
  it is not a cross-backend final score.
- Emby `SearchTerm` returns only Movie/Series. A Series/Season `ParentId`, or an
  `IncludeItemTypes` set containing only Season/Episode, returns an empty search
  envelope without changing ordinary no-term hierarchy browsing.
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
| Requested offset is outside the candidate set | Return an empty page with the capped total |
| Search requests only Season/Episode | Return an empty Emby search envelope |

### 5. Good / Base / Bad Cases

- Good: two 1080p/4K Media versions produce one Movie hit and consume one page
  slot; one Series with many Episodes also produces one Series hit.
- Good: `死神2` outranks a less relevant `新死神10`; equal-relevance
  `死神10`, `死神9`, and `死神2` use descending title numbers.
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
- Emby tests assert Movie/Series results, Season/Episode empty search, ParentId
  behavior, multi-term AND, literal `\\`/`%`/`_`, SearchHints, and logical total.

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
// Wrong: overload Episode.Title with hierarchy context.
episode.Title = series.Title

// Correct: keep the Episode title on the entity and project context separately.
episode.Title = localizedEpisodeTitle
view.SeriesTitle = parentSeries.Title
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
