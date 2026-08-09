# Season and Episode Catalog Research

## Current Project

- `metadata_items` already models Movie, Series, Season, and Episode in one
  table. `ParentID` forms `Series -> Season -> Episode`; partial unique indexes
  enforce one Season number per Series and one Episode number per Season.
- Current discover hydration persists only a Movie or Series root and uses an
  in-memory pending map. `CatalogHydratedAt` currently skips a completed root;
  no durable job or child traversal exists.
- Current TMDb HTTP errors include the complete request URL, whose query string
  contains the API key. Durable jobs must store only sanitized endpoint/status
  errors.
- Current Season persistence creates a generated title and hierarchy slot but
  does not fetch provider Season details or artwork.
- Current Episode detail enrichment requires an existing Media row. It fetches
  an Episode still by default, but scanner post-processing can explicitly skip
  it and catalog-only Episodes are never created.
- `MetadataArtwork` permits one selected asset for each
  `(metadata_id, artwork_type)` and `ArtworkAsset` deduplicates bytes by SHA-256.
  This exactly supports the approved primary-only image scope.
- `MediaView` currently falls back from Season/Episode artwork to parent and
  Series assets. This conflicts with the product decision to leave missing own
  images empty.
- Physical Emby membership requires a visible `Media` relation. Catalog-only
  metadata remains stored but is not a member of a physical library.

## TMDb Official API

Sources:

- `https://developer.themoviedb.org/openapi/tmdb-api.json`
- `https://developer.themoviedb.org/reference/tv-series-details`
- `https://developer.themoviedb.org/reference/tv-season-details`
- `https://developer.themoviedb.org/reference/tv-episode-details`
- `https://developer.themoviedb.org/docs/rate-limiting`

Findings:

- Series, Season, and Episode each have a details endpoint and support
  `append_to_response` for namespace subresources.
- Series details returns the Season inventory, including Season TMDb ID,
  number, name, overview, poster path, and Episode count.
- Season details returns the complete Episode inventory with Episode TMDb ID,
  number, name, overview, air date, runtime, rating, still path, crew, and guest
  stars.
- Season and Episode expose their own external IDs, credits, translations,
  videos, and image endpoints. The approved image scope does not require image
  gallery calls.
- The legacy 40-requests-per-10-seconds rule is disabled, but TMDb documents a
  variable upper limit around 40 requests per second and requires clients to
  respect HTTP 429.
- Series, Season, and Episode have separate changes endpoints suitable for a
  later incremental-refresh task.

## Jellyfin and Emby Reference

Source inspected through Gread: `jellyfin/jellyfin` HEAD. Current proprietary
Emby source is not public, so no claim is made about its latest internals.

Findings:

- Jellyfin models Series and Season as folders and Episode as video, all with
  independent IDs and explicit Series/Season relationships.
- Provider IDs participate in stable identity and lookup context.
- `Series.RefreshAllMetadata` refreshes descendants before final Series refresh.
- Generic metadata refresh tracks metadata and image success separately and
  only advances `DateLastRefreshed` when both succeed.
- Jellyfin supports inherited Season/Episode images. This project intentionally
  differs because the user requires independent entity artwork with no parent
  fallback.

## Adopted Decisions

- Keep the shared metadata table and stable IDs.
- Persist TMDb IDs for every Series, Season, and Episode.
- Persist root data first, but advance completion bottom-up.
- Store typed fields plus one raw provider JSON snapshot per entity/provider.
- Use one durable sequential worker with separate own-metadata, own-artwork, and
  full-scope checkpoints plus 429 backoff.
- Prioritize root jobs and yield after one Season so a long Series cannot block
  other discovered Series roots; bound root priority so descendants cannot
  starve under continuous discovery.
- Preserve root-only Movie hydration and reject invalid TMDb IDs before durable
  job upsert.
- Aggregate the fixed artwork types into one own-artwork checkpoint; people
  profile image import remains best-effort under the existing shared contract.
- Store original bytes for one selected image per supported entity/type.
- Do not inherit metadata, credits, or artwork.
- Defer gallery images, scheduled changes refresh, and provider-removal
  reconciliation.
