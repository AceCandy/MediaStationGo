# Technical Design

## 1. Domain Boundaries

```text
provider / local fallback
        -> MetadataItem + MetadataIdentifier
        -> MetadataArtwork -> ArtworkAsset (DataDir)
        -> Media.MetadataID
        -> MediaView
        -> HTTP / frontend / player / Emby / search
```

`Media` is the playable file entity. `MetadataItem` is the shared display and user-state identity. `ArtworkAsset` is the persistent image entity. NFO and sidecar images are read-only fallback inputs and never outputs.

## 2. Models

### MetadataItem

- UUID base fields
- `Kind`: `movie`, `series`, `season`, `episode`
- `ParentID`: null for movie/series, Series ID for season, Season ID for episode
- `SeasonNum`: canonical season position on season rows
- `EpisodeNum`: canonical episode position on episode rows
- `Title`, `OriginalName`, `EpisodeTitle`, `Overview`
- `Rating`, `Year`, `ReleaseDate`
- `Languages`, `Countries`, `Genres`, `NSFW`
- `Source`: canonical provider name, `local`, or `manual`; `local` includes metadata imported from local NFO.
- Unique season identity: `(parent_series_id, season_num)`
- Unique episode identity: `(parent_season_id, episode_num)`

### MetadataIdentifier

- `MetadataID`, `Provider`, `EntityKind`, `ExternalID`
- Global unique identity: `(provider, entity_kind, external_id)`
- Movie and series identifiers are distinct even when the numeric value matches.
- Season and episode rows do not require provider identifier rows.
- Repository boundaries trim all components, lowercase canonical provider/entity tokens, and reject non-canonical provider names. Numeric provider IDs are stored as canonical base-10 strings; opaque IDs preserve provider-defined case semantics after trimming.

### ArtworkAsset

- `SHA256`, `StorageKey`, `MimeType`, `Width`, `Height`, `SizeBytes`
- `SHA256` and `StorageKey` are globally unique.
- Original files live at `DataDir/artwork/sha256/<2>/<2>/<hash>.<ext>`.

### MetadataArtwork

- `MetadataID`, `AssetID`, `ArtworkType`, `SourceProvider`, `SourceURL`
- One selected artwork per `(metadata_id, artwork_type)`.
- Supported initial types: `poster`, `backdrop`, `still`.

### Media

- Keep library/root/path/relative path, file size, duration, dimensions, codecs, container, STRM, file hash/ID and duplicate fields.
- Keep parsed season/episode and detected title as pre-persistence scan inputs; they are not canonical display metadata.
- Add non-null `MetadataID`, an index, and a foreign key that prevents deleting referenced metadata.
- Keep per-file scrape status; metadata source is stored on MetadataItem.
- Remove persisted shared title/artwork/provider/taxonomy/NSFW fields from the new-install model.

The persisted `Series` metadata table is removed. Series, Season and Episode are all real MetadataItem rows; cards and lists are query projections over those rows.

## 3. Artwork Storage

Create one `ArtworkStore` service using the standard library and existing ImageProxy fetch/validation helpers:

1. Read local bytes or fetch remote bytes.
2. Validate `image/*`, reject the transparent placeholder, decode dimensions with `image.DecodeConfig`.
3. Hash image bytes with SHA-256 and derive extension from validated MIME.
4. Write a temporary file under the target directory and atomically rename it.
5. Upsert ArtworkAsset by hash and update MetadataArtwork in a transaction.

Remote URLs and local source paths remain provenance only. API and Emby image handlers resolve asset IDs to managed local files. Resized variants may remain in CacheDir and are never authoritative.

## 4. Ingest and Scrape State Machine

```text
discovered file
  -> resolve/create minimum metadata identity
  -> create media with non-null metadata_id in the same transaction
  -> pending
  -> provider matched  -> persist provider metadata and managed artwork -> matched
  -> provider no-match -> read local NFO/artwork
       -> local found  -> persist local metadata and managed artwork -> matched
       -> none         -> retain minimum local/manual metadata -> no_match
  -> provider error    -> error, no local fallback
```

The ingest transaction creates metadata before media. A reliable external identifier first resolves MetadataIdentifier; an unknown identifier creates a minimum metadata row with that identifier. Without identifiers, each local movie starts with an independent local metadata identity. Episodic media resolves or creates Series, then Season, then Episode before media is written.

Provider lookup APIs must distinguish `no_match` from operational errors. The orchestrator is the single owner of fallback selection. Scanner only supplies file facts and identity hints; it never persists a media row without metadata.

NFO provider IDs may seed provider lookup. When provider succeeds, all other NFO fields are ignored. For local-only TV content, a deterministic local series key derived from the canonical show/NFO path groups episodes; an explicit local unique ID in NFO takes precedence when supported.

## 5. Identifier Enrichment and Merge

`metadata_identifiers` is the normalized external identity table. A MetadataItem may own any number of TMDb, Douban, Bangumi or TheTVDB identifiers; provider IDs are never dedicated columns on MetadataItem. Internal constructors use canonical provider tokens (`tmdb`, `douban`, `bangumi`, `thetvdb`) rather than aliases.

A provider adapter must mark every returned identifier as explicit provider data or inferred candidate data. Only an identifier explicitly associated with the same provider entity may authorize automatic cross-provider merge. Search results, title/year matching and locally inferred aliases are always inferred candidates.

When a provider result adds an identifier:

1. If the identifier is unowned, attach it to the current metadata.
2. If it already belongs to the current metadata, update metadata fields only.
3. If it belongs to another metadata and the mapping came explicitly from a provider, merge into the existing identifier owner in one transaction.
4. If the candidate came only from title/year/similarity, persist no identity change until user confirmation.

Merge order is identifiers and child identities, media references, metadata-scoped favorites/playlists/history, duplicate relation cleanup, artwork/source resolution, then hard deletion of the unreferenced source metadata. Series merges recursively pair Seasons by season number and Episodes by episode number. Database constraints must prevent source deletion while references remain. Repeating the same explicit mapping is idempotent; contradictory explicit mappings fail without changing either metadata graph.

## 6. Query Contract

Introduce a flat `MediaView` read model containing current public JSON fields plus `metadata_id`. A dedicated query repository performs the required joins before filtering, sorting and pagination:

```text
media
JOIN metadata_items
LEFT JOIN metadata_artworks (poster/backdrop/still)
LEFT JOIN artwork_assets
LEFT JOIN libraries/display libraries
```

There is no unresolved-media fallback because persisted media always has metadata. Minimum local/manual metadata supplies the title until enrichment completes.

All user-facing services consume MediaView. Raw Media remains available only to scanner/probe/write/delete/stream internals. Playback uses file facts from MediaView; display text and images come from joined metadata.

## 7. Search and Grouping

- SQLite FTS becomes metadata-centered and indexes title, original name, overview and genres once per metadata row. Media path and scan-title hints use a LIKE branch.
- OpenSearch keeps one document per media ID for library and permission filtering, but document fields are produced from MediaView. Metadata updates reindex every linked media.
- Version grouping uses `metadata_id`; episode hierarchy follows persisted Series -> Season -> Episode parents.
- Series/season responses project real metadata rows and never synthesize virtual IDs.

## 8. API and Consumers

- Preserve current flat media JSON names where possible; `poster_url` and `backdrop_url` become internal artwork URLs.
- Metadata edit endpoints update MetadataItem and invalidate/reindex all linked MediaViews.
- Emby history, favorites and playlists use metadata IDs as stable item identities; the last or preferred playable version remains a media ID. There is no media-ID identity fallback.
- History, favorites, playlists, recent media, AI search and unified search load MediaView through the stored metadata identity and preferred media version.
- Emby movie, series, season and episode item IDs always use real metadata IDs; MediaSource IDs, PlaybackInfo sources and stream opening continue to use media IDs and file facts.
- Frontend keeps its current `Media` response shape initially and adds `metadata_id`; no unrelated UI redesign.

## 9. Sidecar Read-Only Contract

Remove automatic NFO writing, explicit NFO export routes/services, and organizer operations that move/delete NFO sidecars. No code path may write poster/fanart/thumb files under a library root.

Local fallback imports bytes into DataDir and never retains the source path as the served image. Tests use a non-writable media directory and verify no sidecar mutation.

## 10. Database and Rollback

Models are registered in dependency order in `AllModels()`. Add SQLite/PostgreSQL-compatible indexes and constraints without legacy backfill. Fresh databases are the supported rollout path.

Rollback is source-level: revert the code and recreate the development database. Managed artwork files are additive and may remain orphaned safely; garbage collection is deferred because it is not required for the initial feature.

## 11. Trade-offs

- Flat MediaView avoids a simultaneous public API redesign while still removing database duplication.
- One artwork per type is sufficient for current UI and avoids speculative candidate management.
- No artwork garbage collector in the first implementation; content hashing bounds duplicate storage and orphan cleanup can be added when measurable.
- No per-media metadata overrides; the confirmed source rule is provider or local fallback for the whole metadata record.
- Exact provider crosswalks may auto-merge; fuzzy matches require confirmation because a temporary duplicate is safer than a wrong irreversible identity merge.
