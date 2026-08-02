# Codebase Research

## Current Storage

- `internal/model/library_media.go:24-101`: Media and Series duplicate file, metadata, provider ID and artwork responsibilities.
- `internal/model/model.go:33-64`: `AllModels()` is the source for AutoMigrate and SQLite-to-PostgreSQL copying.
- `internal/database/schema_migration.go:9-80`: startup migration and performance indexes assume metadata fields live on media.
- `internal/database/schema_media_search.go:5-90`: SQLite FTS triggers index media title/original name/path/genres.

## Scrape and Sidecars

- `internal/service/scraper.go:22-110`: local NFO is read/applied before provider lookup and merged into successful matches.
- `internal/service/scanner_local_ingest.go:138-169`: scanner applies local metadata while building Media.
- `internal/service/scraper_tmdb_details.go:159-175`: successful scrape writes NFO back to the media directory.
- `internal/service/nfo.go:75-177` and `internal/handler/nfo.go:12-31`: explicit NFO export paths.
- `internal/service/organizer_sidecar.go:8-29` and `organizer_reclassify_files.go:29-50`: organizer mutates NFO sidecars.

## Artwork

- `internal/service/image_proxy.go:50`: remote images are cached under `CacheDir/images`.
- `internal/service/image_proxy_remote.go:174-218`: existing fetch, content validation and atomic cache-write patterns are reusable.
- `internal/config/normalize.go:14-45`: DataDir defaults to `./data`; CacheDir defaults below DataDir and remains disposable.

## Read Consumers

- `internal/repository/media_repository.go:68-117`: find/list query raw Media and sort/filter on media metadata columns.
- `internal/repository/media_search_repository.go:15-145`: OpenSearch, SQLite FTS and LIKE all assume media-owned metadata.
- `internal/service/media_listing.go:18-108`, `media_search.go:15-77`, `media_series.go:25-99`: primary service read paths consume raw Media.
- `internal/service/emby_items_detail.go:183-265`, `emby_artwork.go:36-74`, `emby_playback.go:16-231`: Emby display and playback mix metadata/artwork/file facts from Media.
- `web/src/types/media.ts:1-45`: frontend Media type mirrors the flattened backend model.

## Decisions from Research

- MediaView must be joined before pagination, filtering and sorting.
- Provider no-match and provider operational errors require distinct outcomes.
- SQLite FTS should index metadata once; OpenSearch should retain one media document for permission filtering.
- Persistent originals belong in DataDir; CacheDir is only for reconstructable derivatives.
- Fresh database creation is the only supported rollout path for this pre-release change.

## Current Partial-Implementation Gaps

- `scanner_local_write_batch.go` and `scanner_cloud_ingest.go` still persist pending media before canonical metadata exists; tests explicitly accept `MetadataID == nil`.
- `metadata.go` currently supports only movie, series and episode. Episode points directly to Series and Season is not persisted.
- `emby_series_ids.go` still synthesizes virtual Series/Season IDs; these must be replaced by real metadata identities.
- `metadata_identifiers` already normalizes provider identities by `(provider, entity_kind, external_id)` and supports multiple provider IDs per metadata.
- `MetadataRepository.UpsertCanonical` attaches an unowned identifier to an existing resolved metadata, but returns a conflict when supplied identifiers already point to different metadata; it does not yet perform controlled merge.
- The partially implemented history, favorite and playlist paths still include media-ID fallback behavior that conflicts with the final non-null metadata invariant.

## Confirmed Identity Rules

- Media persistence requires metadata persistence in the same transaction.
- Local media without provider IDs starts as an independent metadata and may merge only after explicit user confirmation.
- A provider-explicit cross-provider ID mapping may merge automatically; title/year/similarity inference always requires confirmation.
- Season is a real MetadataItem so it can own favorites and other metadata-scoped state.
