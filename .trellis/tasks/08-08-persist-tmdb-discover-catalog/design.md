# Design: Complete TMDb Series Catalog Hydration

## Design Summary

Retain the existing shared metadata hierarchy. Replace the in-memory pending
map as the source of truth with a small durable job table, checkpoint each
metadata entity's own metadata, own artwork, and descendant scope separately,
and store one raw TMDb JSON snapshot per entity/provider. The worker remains
single-threaded and reuses the existing canonical metadata, credit,
people-image, and artwork persistence paths.

This keeps the implementation narrow while satisfying the two properties the
current root-only flow cannot provide: complete child metadata and recovery
after a restart or partial failure.

## Reference Decisions

- TMDb Series details provides the Season inventory. Season details provides
  the complete Episode inventory for one Season. Episode details is still
  requested once per Episode because the product requires the Episode's own
  extended payload and identifiers, not only the embedded Season summary.
- TMDb details endpoints support `append_to_response`; use it for stable
  metadata subresources while excluding image galleries and user-specific
  resources.
- Jellyfin models Series, Season, and Episode as independent entities with
  stable IDs and only advances its refresh timestamp after metadata and image
  work succeeds. This design adopts those properties.
- Jellyfin allows inherited Season/Episode images. The product explicitly
  rejects that behavior, so MediaView and Emby projections use only the
  entity's own selection.
- Current proprietary Emby source is not publicly verifiable. Compatibility is
  based on this project's existing Emby contract and real metadata IDs.

## Data Model

### Shared Metadata Items

Keep all entity kinds in `metadata_items`:

```text
Series (parent_id NULL)
  -> Season (parent_id = Series.ID, season_num >= 0)
       -> Episode (parent_id = Season.ID, episode_num > 0)
```

Do not create separate Season/Episode tables. The existing foreign keys,
favorites, playback history, playlists, artwork, credits, Media links, merge
logic, and Emby IDs all depend on a shared stable metadata ID.

Add `RuntimeSec` to `MetadataItem`. Runtime is metadata-owned for catalog-only
Episodes and cannot live only on a playable `Media` row.

Add two own-entity checkpoints and keep `CatalogHydratedAt` for full scope:

| Field | Meaning |
| --- | --- |
| `CatalogMetadataHydratedAt` | Typed fields, IDs, raw snapshot, and credits completed |
| `CatalogArtworkHydratedAt` | Required own primary image stored, or provider explicitly supplied no image path |
| `CatalogHydratedAt` | Own checkpoints plus all expected descendants completed |

For a Movie or Episode, full-scope completion follows immediately after both
own checkpoints. For a Series or Season, it is delayed until descendants are
complete. These checkpoints avoid repeating successful provider and image work
when only one Episode in a large tree failed.

`CatalogArtworkHydratedAt` is one aggregate checkpoint, not one timestamp per
image type. The entity persistence pass evaluates a fixed mapping:

| Entity | Required artwork scope |
| --- | --- |
| Movie / Series | Poster and backdrop |
| Season | Poster |
| Episode | Still |

For every mapped type, a supplied provider path must resolve to a stored local
selection; an explicitly empty path is a successful empty result. Set the
aggregate timestamp only after all mapped types satisfy one of those outcomes.
On partial failure, retry the pass; content hashing and selection upsert make
already successful types idempotent.

### Provider Identifiers

Every TMDb-hydrated Series, Season, and Episode receives its own
`MetadataIdentifier{Provider: "tmdb", EntityKind, ExternalID}`. Additional
stable external IDs returned by TMDb are stored under their actual provider and
the same entity kind.

Resolution order is provider identity first, then the expected hierarchy slot.
If an exact TMDb response connects an identifier-owned entity with an existing
parent/number entity, use the existing explicit merge path; never merge by
title or number alone across unrelated Series.

### Provider Snapshots

Add `MetadataProviderSnapshot`:

| Field | Contract |
| --- | --- |
| `MetadataID` | Foreign key to the canonical entity |
| `Provider` | `tmdb` for this flow |
| `Payload` | Complete response body as PostgreSQL JSONB |
| `FetchedAt` | UTC provider fetch time |

Use a unique key on `(metadata_id, provider)`. Upsert the snapshot after JSON
validation. Store response bodies only; never persist request URLs, query
parameters, headers, or credentials.

### Durable Hydration Jobs

Add `CatalogHydrationJob` with a unique key on
`(provider, entity_kind, external_id)`:

| Field | Contract |
| --- | --- |
| `MetadataID` | Nullable until the root is resolved |
| `Status` | `pending`, `running`, `retry`, or `completed` |
| `Stage` | `root` or `seasons` |
| `Attempts` | Incremented when a job is claimed |
| `NextAttemptAt` | Retry eligibility time |
| `LastError` | Bounded diagnostic text, never credentials |
| `StartedAt` / `CompletedAt` | Lifecycle timestamps |

The job is scheduling state; `MetadataItem.CatalogHydratedAt` is entity
completion state. This separation lets the worker recover a job whose process
stopped between child checkpoints.

Movie uses only the `root` stage and completes after its own checkpoints.
Series moves from `root` to `seasons` after its details and expected Season
shells are persisted.

### Artwork

The existing `ArtworkAsset` and unique
`MetadataArtwork(metadata_id, artwork_type)` selection already implement the
chosen scope. No gallery table is needed.

Use original TMDb image URLs for catalog persistence:

- Series: `poster`, `backdrop`
- Season: `poster`
- Episode: `still`

An absent provider image is a successful empty result and does not copy or
reference a parent image. A failed required image download preserves an
existing image owned by the same entity and keeps the artwork checkpoint
incomplete for retry.

Credit persistence reuses `PeopleImageStore`: profile bytes are downloaded and
deduplicated before writing a new person image key. Its established failure
contract remains best-effort, preserving an existing key and allowing
authoritative metadata to complete. Entity artwork, unlike profile artwork,
does gate `CatalogArtworkHydratedAt`.

## Provider Contracts

Introduce provider DTOs that retain both typed fields and the validated raw
response. Map provider DTOs into the existing provider-neutral metadata and
credit inputs; do not duplicate repository persistence in TMDb-specific code.

Request shape:

1. Series details once, with stable metadata subresources such as alternative
   titles, content ratings, external IDs, keywords, translations, credits, and
   videos appended. The response supplies the authoritative Season inventory.
2. Season details once per Season, with external IDs, aggregate credits,
   translations, and videos appended. The response supplies the authoritative
   Episode inventory and each Episode's core fields.
3. Episode details once per Episode, with external IDs, credits, translations,
   and videos appended. Image galleries are not requested. Series uses only
   its `poster_path`/`backdrop_path`, Season its `poster_path`, and Episode its
   `still_path`.

The request count for a Series is intentionally `1 + season_count +
episode_count`, plus image/profile downloads. This is the cost of preserving
each Episode's own extended payload. A single worker bounds load. TMDb HTTP 429
uses `Retry-After` when valid, otherwise bounded exponential backoff. Context
cancellation must interrupt both waits and requests.

Provider errors use a sanitized endpoint label, entity ID, and status code.
They must not embed the request URL because the current TMDb URL carries the
API key in its query string. The same sanitized error is used for logs and the
durable job's `LastError`.

## Hydration Flow

```text
/discover/feed
    -> upsert durable root-stage job (small database write)
    -> signal worker
    -> return discover response

worker claims job
    -> fetch and persist root details, IDs, credits, snapshot, own artwork
    -> create/update expected Season shells from the Series response
    -> move job to seasons stage

worker claims one seasons-stage turn
    -> select one incomplete expected Season
         -> fetch/persist Season shell and provider data
         -> upsert Episode shells from the Season inventory
         -> for each incomplete Episode
              -> fetch/persist Episode details, IDs, credits, snapshot, still
              -> mark Episode complete
         -> verify expected Episodes complete
         -> mark Season complete
    -> if another Season is incomplete, requeue the job
    -> otherwise mark Series and job complete
```

Root-stage jobs are claimed before seasons-stage jobs, so every newly observed
Series becomes canonical before one long tree consumes the worker. A
seasons-stage claim advances at most one Season before yielding to the queue.
Completion still advances bottom-up so an ancestor never claims a complete tree
while a descendant is missing.

External requests and image file writes occur outside database transactions.
Each entity's database upserts remain idempotent, and its completion timestamp
is written last. A crash can leave harmless downloaded or deduplicated assets,
but cannot leave a false completion checkpoint.

## Queue and Lifecycle

- `QueueCatalogHydration` upserts jobs and sends a coalescing wake signal.
- Validate provider, supported root entity kind, and positive external ID
  before the upsert. Invalid discover rows neither create jobs nor wake work.
- The database job row, not the wake channel, is the source of truth.
- This service supports one process and one catalog worker. At startup, reset
  every persisted `running` row to `retry`; no timeout or distributed lease is
  needed. Multi-instance claiming is out of scope.
- Claim eligible `pending`/`retry` rows in deterministic order. Root-stage work
  has bounded burst priority; after a fixed internal root burst, one eligible
  seasons-stage turn must run before another root burst. The bound is an
  implementation constant, not runtime configuration.
- The single worker processes one job at a time. Do not add configurable
  concurrency in this task.
- A seasons-stage turn processes at most one Season, then requeues itself so
  other Series make progress.
- A failure records a bounded error, computes `NextAttemptAt`, and continues to
  other eligible jobs. Rediscovery can wake an existing retry job but cannot
  duplicate it.
- The worker waits on either the coalesced signal or the nearest persisted
  `NextAttemptAt`, so retries occur without requiring another discover request.
- Shutdown cancels requests/backoff waits and joins the worker.

For compatibility with the existing root-only implementation, the absence of
a completed durable Series job and the new own-entity checkpoints take
precedence over an old non-null root `CatalogHydratedAt`; the first new
full-tree job must still run.

## Projection and Emby Compatibility

Remove parent and Series artwork fallback from `MediaView`. Project only the
current metadata entity's poster and still/backdrop selections. Emby image tags
and image resolution must use those same own-entity projections.

Likewise, Series, Season, and Episode credit payloads use only rows owned by
that metadata entity. Parent IDs and parent names remain available for
navigation and context; they are relationships, not fallback metadata.

Catalog-only entities remain absent from physical Emby library membership.
Once a real media file links to a prehydrated Episode, existing Emby queries
continue to expose the real Series, Season, and Episode metadata IDs and the
normal hierarchy.

## Compatibility and Migration

- Register additive models/columns through the existing AutoMigrate list.
- Do not create legacy dual writes or fake Media backfills.
- Existing root metadata, identifiers, media links, user state, and artwork are
  preserved.
- Existing root-only completion timestamps do not count as a completed full
  tree without a completed job row.
- Update the shared metadata spec after implementation: TMDb-hydrated
  Season/Episode provider IDs become required, metadata-level runtime and raw
  snapshots become authoritative, and inherited Season/Episode artwork/credits
  are removed.

## Operational Risks and Controls

- Large Series create many provider and image requests. The durable sequential
  worker favors correctness and recoverability over speed.
- Provider data can change during a long run. The Season response captured for
  that attempt defines its expected Episode set; a later explicit refresh can
  reconcile provider removals.
- Raw JSON increases database size, but one compressed JSONB row per entity is
  bounded compared with downloading image galleries and prevents unmapped data
  loss.
- This task does not delete provider-removed children. Scheduled changes-feed
  refresh and tombstone reconciliation require separate product rules around
  locally linked media.

## Rollback

- Disabling the discover enqueue call stops new work without affecting browse
  responses or existing metadata.
- New tables and additive columns can remain unused if the worker is reverted.
- Reverting the no-inheritance projection restores prior display behavior
  without changing stored assets.
- No rollback step deletes metadata, artwork files, or user state.
