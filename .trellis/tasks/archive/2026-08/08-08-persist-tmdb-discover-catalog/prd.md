# Persist TMDb Discover Catalog Metadata

## Goal

Build a durable local metadata catalog from TMDb discover results without
delaying discover responses. A discovered TV Series must become a complete
canonical `Series -> Season -> Episode` tree whose entities, provider data,
credits, and selected artwork remain reusable when local media is scanned
later.

## Confirmed Product Decisions

- Discovering a Series requests full catalog hydration even when no matching
  `Media` row exists.
- Persist the Series first, then hydrate all Seasons and Episodes in the
  background, including specials represented as season zero.
- Series, Season, and Episode are independent canonical metadata entities.
- Season and Episode metadata, credits, and images must come from that entity;
  missing values stay empty instead of inheriting from an ancestor.
- Artwork scope is one provider-selected primary image per supported type, not
  every gallery candidate: Series poster/backdrop, Season poster, Episode still.
  Store the original provider image bytes locally.
- Preserve provider detail responses so fields not yet projected into typed
  columns are not lost.

## Requirements

### Discovery and Scheduling

- Queue only TMDb Movie or Series discover entries with a positive TMDb ID;
  invalid IDs are ignored before any job upsert.
- `/discover/feed` must not wait for TMDb detail, credit, or image requests.
- Queueing must be durable and idempotent by `(provider, entity kind, external
  ID)` so service restart does not lose work and duplicate discover sections do
  not create duplicate jobs.
- Root hydration jobs must take bounded priority over descendant work so one
  long Series cannot prevent other discovered roots from being persisted and a
  continuous root stream cannot starve existing descendant jobs. After roots,
  process at most one Season scope per Series turn for fairness.
- Use one bounded background worker. A failed or interrupted entity must remain
  retryable with backoff, and HTTP 429 handling must honor `Retry-After` when
  present.
- Persisted job errors and logs must identify the entity/endpoint and HTTP
  status without including TMDb request URLs, query strings, API keys, or
  headers.
- Stop and join the worker with the service lifecycle.

### Canonical Metadata

- Keep Movie, Series, Season, and Episode in the shared `metadata_items` table
  with the existing parent hierarchy; do not introduce separate Season or
  Episode entity tables.
- Preserve the existing Movie catalog path as a root-only job using the same
  own-metadata and own-artwork completion rules; only Series has descendant
  stages.
- Fetch and persist the Series root before processing its descendants.
- Hydrate every Season returned by TMDb and every Episode in each Season,
  regardless of local media presence.
- Store a TMDb `MetadataIdentifier` on every Series, Season, and Episode. Store
  additional stable external identifiers returned by TMDb when available.
- Persist typed display fields used by the application, including each
  Episode's name, overview, air date, rating, and metadata-level runtime.
- Persist provider-owned credits for Series, Season, and Episode through the
  shared people/credit path, including local profile images. Do not fill a
  missing entity credit type from its parent. Profile image import remains
  best-effort: failure preserves an existing local key and does not block the
  authoritative entity metadata checkpoint.
- Persist the complete raw provider detail payload for each hydrated entity in
  PostgreSQL JSONB. Payloads must not contain request URLs, API keys, headers,
  or other credentials.
- Keep catalog metadata permanently and never create a fake `Media` row or an
  `is_in_library` flag. Later scanner resolution must bind to the existing
  canonical hierarchy and provider identifier contracts.

### Artwork

- Persist authoritative image bytes below `DataDir/artwork/sha256/...` and
  person profile bytes below `DataDir/people/sha256/...`.
- Persist only the entity's selected primary provider artwork: Series poster
  and backdrop, Season poster, and Episode still.
- Download the provider original image rather than a display-size derivative.
- Existing content-hash asset deduplication and one-selection-per-artwork-type
  behavior remain authoritative.
- Season and Episode image projections and Emby responses must not fall back to
  parent or Series artwork. Missing own artwork produces an empty image field or
  the existing image-route placeholder.

### Completion and Recovery

- Track the entity's own provider-metadata completion and own-artwork completion
  separately from full descendant completion. An explicit provider response
  with no image path satisfies own-artwork completion; a failed required image
  download does not.
- The single own-artwork checkpoint aggregates every required type for that
  entity: Movie/Series poster and backdrop, Season poster, Episode still. It is
  written only when each supplied path is stored and every absent path has been
  observed as explicitly empty.
- Mark an Episode fully hydrated only after its own metadata, identifiers,
  provider snapshot, credits, and own-artwork scope have completed.
- Mark a Season fully hydrated only after its own metadata/artwork scope and
  every expected Episode have completed.
- Mark a Series and its durable job fully hydrated only after its own
  metadata/artwork scope and every expected Season have completed.
- Completed entity checkpoints must make retries resume from incomplete work
  without duplicating metadata, identifiers, credits, or artwork selections.
- A previously root-only Series completion timestamp must not suppress the new
  full-tree job when no completed durable job exists.

### Compatibility

- Catalog-only metadata remains outside physical Emby libraries until at least
  one visible `Media` row links to it.
- Emby retains real `MetadataItem.ID` values and the current
  `Series -> Season -> Episode` navigation hierarchy.
- Existing scanned media, favorites, playback state, playlists, identifiers,
  and managed artwork must survive idempotent upserts and any exact-identity
  merge required to attach provider Season/Episode IDs.

## Out of Scope

- Downloading or indexing every alternate TMDb image candidate.
- User-specific TMDb account state, user ratings, and regional watch-provider
  availability.
- Exposing catalog-only entities as members of a physical Emby library.
- Periodic TMDb changes-feed refresh, removal reconciliation, or scheduled
  refresh of already completed trees; the data model must leave these possible
  as a later task.
- Parallel multi-worker scraping, multi-instance distributed job claiming, or
  a new operator-facing task UI.

## Acceptance Criteria

- [ ] `/discover/feed` returns without waiting for any catalog detail, credit,
      profile-image, or artwork network request.
- [ ] Repeated Series rows upsert one durable job, and pending/running work is
      recoverable after service restart.
- [ ] Invalid/zero TMDb IDs create no job; valid Movie entries retain the
      existing root-only hydration behavior.
- [ ] All newly queued Series roots are processed ahead of long descendant
      scopes within a bounded burst, while continuous root arrivals cannot
      starve Season progress.
- [ ] The worker persists the Series root before descendants and creates no
      `Media` row.
- [ ] A successful Series job creates or reuses every TMDb Season and Episode,
      including season zero, with real parent IDs and TMDb identifiers.
- [ ] Each hydrated entity stores its typed fields, raw provider JSON, own
      credits, separate own-metadata/own-artwork checkpoints, and selected
      primary original artwork when TMDb supplies one.
- [ ] Credit profile images use the existing local people-image store when
      available; a failed refresh preserves the previous local key and does not
      create a false entity-artwork failure.
- [ ] Season/Episode metadata, credits, poster/still projections, and Emby image
      responses never inherit an ancestor value; missing own data stays empty.
- [ ] A partial failure leaves the affected entity, its ancestors, and the job
      incomplete; retry resumes idempotently and does not redo completed
      descendants.
- [ ] The Series root and durable job become complete only after the expected
      Season/Episode tree is complete.
- [ ] A later scan of matching media binds to the prehydrated canonical Episode
      without replacing the stored provider metadata.
- [ ] TMDb 429 responses use bounded retry/backoff and do not spin or block
      service shutdown.
- [ ] Provider failures and durable job errors contain no API key, credential,
      request query string, or signed URL.
- [ ] Focused model, repository, provider, worker, MediaView, and Emby tests pass;
      PostgreSQL integration tests run when `MEDIASTATION_TEST_POSTGRES_DSN` is
      available, and `git diff --check` passes.
