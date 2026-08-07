# Shared Media Metadata Contract

## Scenario: Canonical Metadata and Managed Artwork

### 1. Scope / Trigger

- Apply this contract whenever scanner, scraper, media queries, playback, Emby, search, or artwork storage changes.
- `Media` owns playable-file facts and unresolved scan hints. `MetadataItem` owns shared display metadata. `ArtworkAsset` owns persistent image bytes.
- This is a fresh-install schema. Do not add legacy field dual writes or historical backfills unless a separate migration task requires them.

### 2. Signatures

- Canonical entities: `MetadataItem{Kind, ParentID, SeasonNum, EpisodeNum, Title, ..., NSFW, Source}`.
- External identity: `MetadataIdentifier{MetadataID, Provider, EntityKind, ExternalID}` with global uniqueness on `(provider, entity_kind, external_id)`.
- Season identity: `(parent_series_id, season_num)`; episode identity: `(parent_season_id, episode_num)`; provider season/episode IDs are not required.
- File link: non-null `Media.MetadataID` with a restrictive foreign key to `MetadataItem.ID`.
- Metadata-owned state: `Favorite.MetadataID`, `PlaybackHistory.MetadataID`, and `PlaylistItem.MetadataID` are non-null; `MediaID` only selects a concrete playable version.
- Read model: `MediaViewRepository.FindByID`, `FindByIDs`, `ListByLibrariesFiltered`, and `SearchFilteredPage`.
- Complete track facts: `MediaProbeMetadata{MediaID, ProbeJSON, SchemaVersion, ProbedAt}` uses `media_id` as both primary key and a cascading foreign key to `media(id)`; list queries must not join or preload this table.
- Probe document: `ProbeDocumentSchemaVersion` and `MarshalProbeDocument` / `UnmarshalProbeDocument` own the versioned JSON contract. Only typed format, video, audio, subtitle, chapter, safe tag, disposition, and color/HDR fields are persistable.
- Emby playback response: `PlaybackInfo{MediaSources, PlaySessionId, DateCreated}`; `DateCreated` is the selected concrete `Media.CreatedAt` encoded as a non-null UTC JSON timestamp with seven fractional digits and a trailing `Z`.
- Track backfill API: admin-only `POST /api/libraries/:id/probe` starts a service-lifetime background task and reports `total`, `completed`, `skipped`, and `failed` metrics.
- Manual apply API: `POST /api/media/:id/scrape/apply` accepts `ManualScrapeRequest`, persists metadata through `ScraperService.ApplyManualMatch`, then returns the refreshed `MediaView` from `MediaService.GetMedia`.
- Artwork response: `/api/artwork/:assetID`; originals live under `App.DataDir/artwork/sha256/...`.
- Library deletion: `DELETE /api/libraries/:id` -> `MediaService.DeleteLibrary(ctx, id)`.
- Scrape entrypoints (`POST /api/media/:id/scrape`, `POST /api/libraries/:id/scrape`, manual apply, scan auto-scrape, STRM refresh, and repair-rescrape) enrich metadata only and never invoke `OrganizerService`.
- Regular scrape entrypoints never infer an adult code or call `AdultProvider`. `ScraperService.AnyEnabled` reports regular provider availability and excludes the adult provider.
- Adult network lookup requires an explicit adult operation: manual search whose provider set contains `adult`, manual apply with `source=adult`, or organize with `mediaType=adult`. Manual search with an empty provider or `provider=all` is not an explicit adult operation.

### 3. Contracts

- Scanner resolves or creates minimum metadata before writing media. Metadata hierarchy and media are persisted in one transaction; a failed ingest must not leave an unbound media row.
- Minimum metadata is an internal identity, not proof of provider success. A movie without provider IDs gets an independent `source=local` item; episodic media resolves `Series -> Season -> Episode` before media.
- A reliable provider ID resolves by `(provider, entity_kind, external_id)`. TMDb is optional; Douban-only and provider-less manual metadata are valid.
- Provider match enriches one canonical `MetadataItem`, its identifiers and managed artwork, then links every matching file through `Media.MetadataID`.
- Provider no-match may import existing NFO and sidecar images. Provider error or timeout must set an error state and must not fall back to local metadata.
- Movie and series identifiers include `EntityKind`; equal numeric IDs across kinds or providers must not collide.
- Season rows require a parent Series, `season_num >= 0`, and `episode_num = 0`. Episode rows require a parent Season, `season_num = 0`, and `episode_num > 0`. Movie and Series rows have no parent or episodic position.
- An unowned provider identifier may be attached to the current metadata. A corrected ID from the same provider/kind replaces the stale ID on that metadata in the same transaction.
- If an identifier already belongs to another metadata, automatic graph merge requires an explicit provider crosswalk or user confirmation. Title/year/similarity evidence alone must not authorize merge.
- Graph merge recursively pairs Series children by season and episode number, moves media and metadata-owned state, deduplicates user relations, then hard-deletes the unreferenced source metadata.
- Lists, permissions, pagination, search, playback display text, and Emby display text read `MediaView`. File opening, probing, duration, codecs, path, and STRM URL read the embedded `Media` facts.
- A local `.strm` keeps the sidecar in `Media.Path` and its supported absolute media target in `Media.STRMURL`. Scan, manual reprobe, and asynchronous PlaybackInfo repair probe the target while persisting facts to the original Media row; stale target results must be discarded.
- A successful full probe atomically updates the scalar `Media` projection and upserts its complete probe document after rechecking the source identity. Local and local-STRM probes also persist the probed target's size. Failed, partial, or stale probes must not replace the previous valid complete document.
- Probe JSON must never contain the input filename/path/URL, signed query, request headers, cookies, authorization values, route tokens, attachments, or arbitrary metadata. Unknown schema versions, malformed JSON, duplicate/negative stream indexes, attached pictures, and unsupported stream types are invalid and trigger scalar fallback plus lazy repair.
- Emby `PlaybackInfo` must enumerate every visible sibling `Media` version before scheduling asynchronous track repair. The playback-layer in-flight map deduplicates by `Media.ID`; the `FFprobeService` limiter remains the only actual probe concurrency limit.
- Emby `PlaybackInfo.DateCreated` must be present at the response top level as well as on each `MediaSource`. Compatibility fields must be verified at the exact JSON layer consumed by the client; a same-named field on the item or nested source does not satisfy a top-level contract.
- Emby detail and PlaybackInfo batch-load valid probe documents and map every embedded video/audio/subtitle by its absolute ffprobe stream index. Sidecar subtitles are rediscovered and deterministically indexed after the highest embedded index on every response.
- `GET /api/media/:id` attaches an optional `tracks` array through `MediaService.GetMedia` only. Each `MediaTrack` is a typed whitelist projection of video/audio/subtitle facts with the original absolute `index`; it excludes probe paths, URLs, headers, credentials, arbitrary tags, and unsupported stream types. Missing or invalid probe data omits the array, while list/search responses do not load or expose it.
- Emby paginated browse/list payloads use scalar media fields only: they do not load complete probe documents, scan sidecar subtitles, or schedule lazy track repair.
- Embedded and sidecar subtitles are externally delivered through a controlled token-aware `DeliveryUrl`; delivery revalidates the current stream/index and never accepts a caller-supplied filesystem path or ffmpeg map expression.
- GET query and POST body playback selections preserve omitted, `0`, and `-1`. Values below `-1`, missing explicit audio indexes, unknown subtitle indexes, and media-source IDs outside the visible sibling set are rejected.
- HLS validates the selected absolute audio index before building `-map 0:<index>?`. Its job registry, output directory, playlist, segment, stop, active status, and cleanup all use the same deterministic media/audio `TranscodeKey`; external subtitle selection does not change that key.
- For a local `.strm`, Emby item and source `Container`/`Path` must describe the resolved `Media.STRMURL` target and must never expose the `.strm` sidecar as the playable path. A source `Bitrate` is the average `SizeBytes * 8 / DurationSec` only when both inputs are positive.
- Emby `MediaSource.Name` is a version label derived from the real source filename (the resolved STRM target for local STRM). Remove the extension, title/year, season/episode markers, and preserve the remaining technical release markers; use `默认版本` when no label remains.
- A successful single-media manual scrape response must be read after persistence from `MediaView`; returning the raw `Media` row can expose the previous scan title or omit shared metadata fields. A failed or empty refresh is an internal error, not a successful `null` response.
- `MediaView` uses an inner join to `metadata_items`; persisted unresolved media does not exist and scan hints never replace canonical display identity.
- Emby movie, Series, Season and Episode item identity and user state always use real `MetadataItem.ID`. Concrete `MediaSource` identity and the last or preferred playable version use `Media.ID`; there is no media-ID identity fallback or virtual Series/Season ID.
- Emby `/Items`, `/Items/Counts`, and search hint totals must count logical metadata items after applying the same visibility, type, and library filters used by the payload. Version collapse must happen before user-visible pagination, so multiple playable versions cannot consume a page or inflate `TotalRecordCount`.
- Emby stream and HLS endpoints may receive either a metadata item ID or a concrete media source ID. They must resolve the request to a visible playable `Media.ID` before calling stream/transcode services.
- Emby clients may call `/SearchHints`, `/Search/Hints`, and their user-scoped or lowercase variants; these routes must project shared metadata titles and IDs, not raw media scan fields.
- Provider identifier projection must return at most one joined row per media. Aggregate or otherwise reduce identifiers before joining; never join the raw one-to-many identifier table into paginated media queries.
- Selected artwork is copied into `DataDir`; remote URLs and source paths are provenance only and are never served as the authoritative runtime image.
- Scanner, scraper, metadata edit, and organizer metadata flows only read NFO/poster/fanart/thumb sidecars. They must not create, overwrite, move, or delete them.
- Scan and scrape flows must not move, rename, delete, deduplicate, or reclassify playable media files or change their library/path placement. Only an explicit organize operation may invoke `ReclassifyMisclassifiedMedia` or other filesystem transfer helpers.
- Deleting a library transactionally hard-deletes its `Media`, `LibraryRoot`, and `Library` rows. It preserves shared metadata, metadata-owned user state, identifiers, managed artwork, and all on-disk media files.

### 4. Validation & Error Matrix

| Condition | Required result |
| --- | --- |
| Season has no Series parent, negative season, or episode position | Repository validation error and database CHECK rejection |
| Episode has no Season parent, nonzero season position, or non-positive episode | Repository validation error and database CHECK rejection |
| Movie/Series has a parent or season/episode identity | Repository validation error and database CHECK rejection |
| Media or metadata-owned user state has an empty/missing metadata ID | Database NOT NULL/CHECK/foreign-key rejection |
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
| Any library, root, or media hard-delete fails | Roll back the whole library deletion and return an error |
| Playback repair sees multiple visible versions | Schedule each missing version asynchronously; do not reject siblings merely because the playback reservation map is full |
| Local probe queue is full | Wait for queue capacity until the caller context is canceled; release the per-path reservation on cancellation |
| Local STRM source is exposed through Emby | Resolve the target for source path/container/name; never return the `.strm` text path as a playable source |
| Probe JSON is malformed, outdated, or fails structural validation | Ignore it, serve scalar fallback, and schedule lazy repair without overwriting prior valid data on failure |
| Probe source changes while ffprobe is running | Reject the result transactionally; update neither scalar facts nor complete JSON |
| Explicit audio/subtitle selection is invalid | Return bad request; never silently map a different track |
| Embedded or sidecar subtitle index no longer resolves | Return not found and require a refreshed PlaybackInfo response |
| PlaybackInfo resolves a visible concrete media | Return its non-null `DateCreated` at the response top level; do not rely only on item/source dates |

### 5. Good / Base / Bad Cases

- Good: two paths with the same TMDb movie identity link to one movie metadata row, share Emby favorite/played/resume state, and keep separate media IDs, paths, codecs, sizes, and media sources.
- Good: two files for the same series/season/episode link to one Episode whose parent is a real Season whose parent is the Series; the Season can be favorited independently.
- Good: a Douban-only movie creates/reuses metadata without a TMDb ID; a later explicit TMDb crosswalk either attaches the unowned ID or safely merges into its existing owner.
- Base: a provider-less file has minimum local/manual metadata and a non-null `metadata_id`; MediaView displays that minimum metadata until enrichment.
- Bad: creating a media row first and filling `metadata_id` later, or treating `Media.ID` as an item identity fallback.
- Bad: copying provider title, genres, NSFW, or artwork URL into each `Media` row.
- Bad: applying NFO title or artwork after a successful provider match.
- Good: `/media/STRM-115/Movie.mkv` follows the regular provider chain without a JavDB/JavBus request.
- Good: explicit `provider=adult`, `source=adult`, or `mediaType=adult` can still request adult metadata.
- Bad: treating `provider=all`, a parent directory, or a scan title that resembles a code as consent to contact an adult provider.
- Bad: joining all `MetadataIdentifier` rows directly and then applying `COUNT`, `OFFSET`, or `LIMIT`.
- Good: deleting a local or cloud library physically removes its library/root/media rows while the referenced metadata and user state remain.
- Bad: using GORM's scoped `Delete` for a library or its media and leaving rows in the recycle bin.
- Good: a scan or scrape changes title, identifiers, artwork and scrape status while the playable file path and library ID remain unchanged.
- Bad: calling `ReclassifyMisclassifiedMedia` after a scrape and silently moving or deleting a local media/STRM file.
- Good: a two-version local STRM item schedules both target files, persists each result to its original `Media` row, and exposes matching target container/path/name/average bitrate.
- Bad: limiting sibling scheduling by the playback reservation map or deriving a source label from the `.strm` sidecar/title, which leaves versions unprobed or displays `strm`/title metadata.
- Good: PlaybackInfo for a concrete media returns the same `Media.CreatedAt` at the top level and on that media source; bad: adding the field only to `MediaSource` while the client reads `PlaybackInfo.DateCreated`.

### 6. Tests Required

- Schema: create `Series -> Season 0 -> Episode` and reject invalid parent kinds, duplicate season/episode identities, empty media metadata IDs, unknown foreign keys, and deletion of referenced metadata.
- Identity: allow equal external IDs across provider or entity kind; deduplicate equal canonical identities.
- Identity: replace a stale unowned identifier for the same provider/kind, preserve other provider identifiers, and reject an occupied identifier unless merge is explicitly authorized.
- Merge: move multiple media versions, favorites, playlists and history; recursively merge Series children; assert duplicate user state is resolved and source metadata is physically gone.
- Query: add multiple identifiers for one metadata/provider/kind and assert media count, page length, and order remain unchanged.
- Visibility: shared `NSFW` must hide list, search, detail, and PlaybackInfo results before pagination/response mapping.
- Playback/Emby: assert item display `Name` comes from shared metadata while each `MediaSource.Name` comes from the real source filename with title, year, season/episode markers, and extension removed; source path/container/codecs still come from `Media`.
- Playback/Emby: assert multiple media versions expose one metadata item ID, share user state, and retain distinct media source IDs.
- Playback/Emby: JSON-round-trip PlaybackInfo and assert top-level `DateCreated` is present, non-null, parseable, and equal to the selected concrete media's creation time.
- Playback/Emby: assert `/Videos/{metadata_id}/stream` and HLS requests resolve to a concrete visible media source ID before opening files or transcoding.
- Playback/Emby: assert local STRM scan, manual reprobe, and missing-metadata PlaybackInfo use the real target, persist target size/track facts, deduplicate and bound background probes, and reject stale target results.
- Probe storage: assert safe typed JSON round-trips every video/audio/subtitle absolute index and disposition while excluding input URLs, credentials, arbitrary tags, attachments, and structurally invalid streams; hard media deletion must cascade to the one-to-one probe row.
- Media detail projection: assert `GET /api/media/:id` returns only whitelisted track fields with absolute indexes, omits malformed/missing probe data, and leaves paginated list/search payloads without `tracks` or probe loads.
- Backfill: cover more than one keyset page, valid-record skips, version/corruption repair, failure accounting, request-independent context, and `total = completed + skipped + failed` for completed runs.
- Playback selection: cover GET and POST omitted/zero/negative values, explicit invalid audio/subtitle indexes, default-audio choice, numeric ffmpeg maps, two simultaneous audio selections, stop/restart isolation, and allowlisted segment query propagation.
- Subtitles: add/remove sidecars between PlaybackInfo calls, deliver embedded and sidecar streams by controlled index, and reject stale or wrong-type indexes without exposing backing paths or credentials.
- Playback/Emby: assert all visible sibling versions are scheduled, duplicate media IDs are not probed concurrently, average bitrate is omitted when size or duration is missing, and target-derived source name/container/path never expose the STRM sidecar.
- Scanner queue: assert a full local probe queue waits for capacity and a canceled context releases the reserved path without enqueuing a stale task.
- Playback/Emby: assert `/Items` totals, `/Items/Counts`, and `/SearchHints` count shared metadata once while still exposing every concrete version as a `MediaSource`.
- Playback/Emby: assert Series and Season IDs are real metadata IDs, Episode parent IDs follow the stored hierarchy, and no virtual or media-ID fallback is emitted.
- Scrape state: test provider match, definitive no-match with local fallback, and provider error without fallback.
- Scrape provider boundary: assert regular enrichment, `provider=all` manual search, and non-adult organize make zero adult-provider requests; retain positive coverage for explicit adult manual search and adult organize.
- Manual apply API: assert the response contains the newly persisted shared title while the media path and library ID remain unchanged.
- Artwork: delete/ignore cache and remote source after import; `/api/artwork/:assetID` must still serve the DataDir copy.
- Sidecars: snapshot NFO/poster/fanart/thumb before scan/scrape/organize and assert content and paths are unchanged afterward.
- Media files: snapshot playable paths before every scrape entrypoint and assert file existence, path, library ID and bytes are unchanged afterward.
- Library deletion: query with `Unscoped` and assert library/root/media rows are gone, metadata remains, and a failed child delete rolls back all rows.
- Run SQLite coverage. When a PostgreSQL DSN is available, run AutoMigrate plus MediaView query/constraint integration tests; otherwise record static verification as incomplete.

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
    Joins("LEFT JOIN (<one-row-per-metadata-and-kind identifier projection>) AS ids ON ...").
    Offset(offset).Limit(limit)
```

The inner metadata join enforces the persisted identity contract. The identifier projection must preserve one output row per media before filtering, counting, sorting, and pagination.

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

Playback compatibility fields must be placed at the exact response layer:

```go
// Wrong: a nested source date does not satisfy clients reading the response top level.
return map[string]any{"MediaSources": sources}

// Correct: keep the source fields and expose the selected media date at PlaybackInfo top level.
return map[string]any{"MediaSources": sources, "DateCreated": formatEmbyDateTime(media.CreatedAt)}
```
