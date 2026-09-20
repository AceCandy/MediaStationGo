# Independent local NFO catalog

## Boundaries
Use `catalog_source=nfo` with NULL `media.metadata_id`. Store Movie/Series/Season/Episode identities in `nfo_items`, file-owned NFO snapshots and bindings in `nfo_media_bindings`, and independent user state/events. No surrogate shared metadata rows, provider lookups or remote artwork requests. Keep public MediaView DTOs and existing file/probe/stream handling.

## Identity and ingestion
Internal UUIDs persist by library root and local key, not external identifiers or title. Movies group only with folder-prefix plus explicit version delimiter; other movies use file paths. Series use show directories, seasons explicit season numbers, episodes explicit episode numbers with version-compatible names. Per-file snapshots retain independent NFO data. Missing/invalid NFO preserves the last successful snapshot. Re-read NFO for NFO scans and use content fingerprints to avoid stale sidecars. Transactions cover file/binding/item writes. Source-only NFO items stay out of ordinary scraping/metadata SQL.

## Read and user boundaries
NFO views join only NFO tables and shared files/probes, applying library/profile/NSFW filters before pagination. Web and Emby expose local logical identities and file versions. Store state/events independently, with stable NFO item IDs; preserve state when files are removed. Shared artwork bytes may be reused only as immutable assets, while ownership remains NFO-specific. Local sidecars are read-only.

## Migration
The user confirmed there are no historical NFO libraries or data. Register independent models in startup AutoMigrate and verify fresh/repeated schema creation in isolated PostgreSQL. Do not implement historical data or user-state migration; ordinary and HongGuo data must remain untouched.

## Tasks
Use a separate `nfo` task-center system for NFO scan/import/retry as needed. Reuse task tracking/history/cancellation; no online scrape jobs for NFO. Ordinary/hongguo behavior remains unchanged.

## Validation
PostgreSQL integration tests prove transactional isolation, migration, repeated scans, version boundaries, NFO failure preservation, zero provider requests, user state, Web/Emby visibility and playback. Run normal catalog/hongguo regressions and Web lint/build. Review all changed boundaries separately from implementation.
