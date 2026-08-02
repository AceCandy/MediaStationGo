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
- Manual apply API: `POST /api/media/:id/scrape/apply` accepts `ManualScrapeRequest`, persists metadata through `ScraperService.ApplyManualMatch`, then returns the refreshed `MediaView` from `MediaService.GetMedia`.
- Artwork response: `/api/artwork/:assetID`; originals live under `App.DataDir/artwork/sha256/...`.
- Library deletion: `DELETE /api/libraries/:id` -> `MediaService.DeleteLibrary(ctx, id)`.
- Scrape entrypoints (`POST /api/media/:id/scrape`, `POST /api/libraries/:id/scrape`, manual apply, scan auto-scrape, STRM refresh, and repair-rescrape) enrich metadata only and never invoke `OrganizerService`.

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
| Artwork import fails | Return the error and keep the currently selected managed asset |
| Provider metadata implies a different category/library | Persist metadata and artwork only; preserve the media path and library ID |
| User cannot view NSFW/library | Filter in `MediaView` query before pagination or playback response creation |
| Any library, root, or media hard-delete fails | Roll back the whole library deletion and return an error |

### 5. Good / Base / Bad Cases

- Good: two paths with the same TMDb movie identity link to one movie metadata row, share Emby favorite/played/resume state, and keep separate media IDs, paths, codecs, sizes, and media sources.
- Good: two files for the same series/season/episode link to one Episode whose parent is a real Season whose parent is the Series; the Season can be favorited independently.
- Good: a Douban-only movie creates/reuses metadata without a TMDb ID; a later explicit TMDb crosswalk either attaches the unowned ID or safely merges into its existing owner.
- Base: a provider-less file has minimum local/manual metadata and a non-null `metadata_id`; MediaView displays that minimum metadata until enrichment.
- Bad: creating a media row first and filling `metadata_id` later, or treating `Media.ID` as an item identity fallback.
- Bad: copying provider title, genres, NSFW, or artwork URL into each `Media` row.
- Bad: applying NFO title or artwork after a successful provider match.
- Bad: joining all `MetadataIdentifier` rows directly and then applying `COUNT`, `OFFSET`, or `LIMIT`.
- Good: deleting a local or cloud library physically removes its library/root/media rows while the referenced metadata and user state remain.
- Bad: using GORM's scoped `Delete` for a library or its media and leaving rows in the recycle bin.
- Good: a scan or scrape changes title, identifiers, artwork and scrape status while the playable file path and library ID remain unchanged.
- Bad: calling `ReclassifyMisclassifiedMedia` after a scrape and silently moving or deleting a local media/STRM file.

### 6. Tests Required

- Schema: create `Series -> Season 0 -> Episode` and reject invalid parent kinds, duplicate season/episode identities, empty media metadata IDs, unknown foreign keys, and deletion of referenced metadata.
- Identity: allow equal external IDs across provider or entity kind; deduplicate equal canonical identities.
- Identity: replace a stale unowned identifier for the same provider/kind, preserve other provider identifiers, and reject an occupied identifier unless merge is explicitly authorized.
- Merge: move multiple media versions, favorites, playlists and history; recursively merge Series children; assert duplicate user state is resolved and source metadata is physically gone.
- Query: add multiple identifiers for one metadata/provider/kind and assert media count, page length, and order remain unchanged.
- Visibility: shared `NSFW` must hide list, search, detail, and PlaybackInfo results before pagination/response mapping.
- Playback/Emby: assert source `Name` comes from `MediaView.Title` while source path/container/codecs come from `Media`.
- Playback/Emby: assert multiple media versions expose one metadata item ID, share user state, and retain distinct media source IDs.
- Playback/Emby: assert `/Videos/{metadata_id}/stream` and HLS requests resolve to a concrete visible media source ID before opening files or transcoding.
- Playback/Emby: assert `/Items` totals, `/Items/Counts`, and `/SearchHints` count shared metadata once while still exposing every concrete version as a `MediaSource`.
- Playback/Emby: assert Series and Season IDs are real metadata IDs, Episode parent IDs follow the stored hierarchy, and no virtual or media-ID fallback is emitted.
- Scrape state: test provider match, definitive no-match with local fallback, and provider error without fallback.
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
