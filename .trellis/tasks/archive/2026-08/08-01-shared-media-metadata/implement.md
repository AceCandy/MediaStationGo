# Implementation Plan

## Phase 1: Enforce the Domain Model

- [ ] Add `season` to MetadataItem and enforce `series -> season -> episode` parent and uniqueness constraints.
- [ ] Make `media.metadata_id` non-null with a foreign key that rejects deletion of referenced metadata.
- [ ] Make favorites, playback history and playlist items metadata-owned; retain media ID only where a concrete playback version is required.
- [ ] Remove compatibility fallbacks that treat media ID as metadata identity.
- [ ] Update fresh-install SQLite and PostgreSQL schemas only; do not add legacy backfill.

Validation:

```bash
go test ./internal/model ./internal/database ./internal/repository -run 'Metadata|Season|Favorite|History|Playlist|Constraint'
```

Rollback point: schema and model tests must pass before changing ingest callers.

## Phase 2: Create Metadata Before Media

- [ ] Add one transactional repository/service operation that resolves or creates minimum metadata, then creates media with its metadata ID.
- [ ] Route local scan, cloud scan, organizer and manual creation through that operation.
- [ ] Resolve reliable `(provider, entity_kind, external_id)` identities before creating metadata; support Douban without TMDb.
- [ ] Normalize and validate provider tokens, entity kinds and provider-specific external ID formats at repository boundaries.
- [ ] Create independent `source=local` metadata when no provider ID exists; never merge by title/year automatically.
- [ ] Create `source=manual` metadata before binding manually created media; allow zero provider identifiers.
- [ ] Build episodic identities in order: Series, Season, Episode, then media.
- [ ] Delete tests and branches that expect a persisted media row with `metadata_id = NULL`.

Validation:

```bash
go test ./internal/service ./internal/repository -run 'Scanner|Cloud|Organizer|Manual|Metadata|Season|Episode'
```

Rollback point: failure injection must prove neither orphan media nor partial hierarchy rows remain after transaction rollback.

## Phase 3: Enrich and Merge Canonical Metadata

- [ ] Attach a newly discovered unowned provider identifier to the current metadata without changing its internal ID.
- [ ] Add a metadata merge transaction for identifiers that already belong to another metadata.
- [ ] Automatically merge only when a provider explicitly supplies the cross-provider mapping.
- [ ] Carry explicit-versus-inferred identity evidence from provider adapters into candidate selection and merge authorization.
- [ ] Require user confirmation for title/year/similarity candidates before any rebind or merge.
- [ ] Rebind media and metadata-owned user relations, resolve unique-key duplicates, and hard-delete the source metadata only after it has no references.
- [ ] Recursively merge Series children by season number and episode number without breaking parent links.
- [ ] Define deterministic metadata/artwork field precedence so the existing canonical provider record wins unless the confirmed match supplies newer authoritative fields.
- [ ] Test repeated and contradictory cross-provider mappings for idempotence and full rollback.

Validation:

```bash
go test ./internal/repository ./internal/service -run 'Identifier|Canonical|Merge|Douban|TMDb|Bangumi|TheTVDB|Season|Episode|Favorite|Playlist'
```

Rollback point: conflict and injected-error tests must leave both original metadata graphs unchanged.

## Phase 4: Unified Reads, Emby and Search

- [ ] Make MediaView use an inner metadata join and remove unresolved-media display fallbacks.
- [ ] Update list/detail/recent/search/AI/history/favorite/playlist paths to consume metadata identity consistently.
- [ ] Replace virtual Emby Series and Season IDs with real MetadataItem IDs.
- [ ] Make Emby movie, series, season and episode Item IDs metadata IDs while keeping MediaSource IDs as concrete media IDs.
- [ ] Update version grouping, SQLite FTS and OpenSearch reindexing for real metadata hierarchy.
- [ ] Keep the current flat frontend response shape, adding only required metadata identity fields.

Validation:

```bash
go test ./internal/repository ./internal/service ./internal/handler -run 'MediaView|Search|Series|Season|Version|Visibility|Emby|Playback'
npm --prefix web run build
npm --prefix web run lint
```

## Phase 5: Persistent Artwork and Read-Only Sidecars

- [ ] Keep authoritative artwork under `App.DataDir/artwork` and expose only internal artwork URLs.
- [ ] Ensure provider success ignores non-ID NFO fields; definitive no-match may import local metadata and artwork.
- [ ] Ensure provider operational errors do not trigger local fallback.
- [ ] Remove automatic NFO writes, export endpoints and organizer mutations of metadata sidecars.
- [ ] Add a read-only media-directory test covering provider and local fallback flows.

Validation:

```bash
rg -n 'WriteMediaNFO|ExportLibrary|ExportOne|transferSidecarNFO|moveSidecarNFO|removeMediaAndNFO' internal web
go test ./internal/service ./internal/handler -run 'Scrape|LocalMetadata|Artwork|NFO|Sidecar|ReadOnly'
```

## Final Quality Gate

- [ ] Run focused tests after every phase.
- [ ] Run `go test ./...` after focused checks pass because the change crosses shared models and consumers.
- [ ] Run frontend build/lint if frontend types changed.
- [ ] Run `git diff --check` and inspect `git diff --stat`.
- [ ] Independently review schema constraints, atomic ingest, merge rollback, hierarchy integrity, query pagination, NSFW filtering, artwork persistence and sidecar write removal.
- [ ] Confirm no media-ID metadata fallback, virtual Emby Series/Season ID, or persisted null `metadata_id` remains.
- [ ] Confirm no service started during testing; if one is started, stop it before completion.

## Known Risks

- PostgreSQL currently has no built-in integration fixture; run it only when a test DSN is available and otherwise report the gap.
- Metadata merge crosses identifiers, hierarchy, media and user relations; it requires transaction rollback and duplicate-relation tests.
- Third-party cross-provider mappings can be wrong; automatic merge is restricted to explicit provider mappings, while fuzzy candidates remain manual.
- Existing local media organizer may intentionally move media files; only metadata/artwork sidecar mutation is removed.
