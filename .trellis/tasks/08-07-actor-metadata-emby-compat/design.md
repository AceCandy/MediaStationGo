# Technical Design

## 1. Domain Model

```text
TMDb credits / local NFO actors
        -> Person + PersonIdentifier
        -> MetadataCredit -> MetadataItem
        -> Emby item People / Persons / Person detail
        -> existing ImageProxy for profile URL
```

`Person` is the shared people identity. `MetadataCredit` is the per-work relationship and owns type, role and ordering. Initial credit types are `Actor`, `GuestStar`, `Director` and `Writer`.

### Person

- UUID base fields and soft delete.
- `Name`, `OriginalName`, `NormalizedName`, `Overview`, `ProfileURL`, `Source`.
- `OriginalName` is provider/NFO source text. `Name` is the display value and may be AI-localized; source refresh only resets `Name` when `OriginalName` changed.
- Active local-person uniqueness uses `(source, normalized_name)` only for `source=local`.
- A TMDb person is resolved through `PersonIdentifier`, not by name.

### PersonIdentifier

- `PersonID`, `Provider`, `ExternalID`.
- Active global uniqueness: `(provider, external_id)`.
- Initial supported provider is `tmdb`; normalization mirrors `MetadataIdentifier` where applicable.

### MetadataCredit

- `MetadataID`, `PersonID`, `Type`, `Role`, `OriginalRole`, `SortOrder`.
- `OriginalRole` is source text. `Role` is the display value and may be AI-localized for Actor/GuestStar; source refresh only resets it when the original changed.
- Role source/display values use text columns because providers may return one credit containing many slash-separated roles.
- Active uniqueness includes metadata, person, type and role so one actor may hold multiple roles.
- Queries order by `sort_order`, then stable ID.
- Repository replacement runs in one transaction: resolve/upsert people, soft-delete stale credits, restore or create current credits.

Models are registered in `model.AllModels()`. Related test migration helpers must explicitly include all three tables.

## 2. Scrape Data Contract

Add `Credits []PersonCredit` plus a loaded-type set to `Match` and `LocalMetadata`. `PersonCredit` is an internal provider-neutral DTO containing provider, external ID, original/display name, type, original/display role, order and profile URL. A missing type in the loaded set means the source did not supply that contract and existing credits of that type remain unchanged; a loaded type with no rows is an authoritative empty snapshot.

TMDb movie and TV requests append `credits` to the existing `alternative_titles,translations` request. Cast maps to Actor. Crew jobs Director and Writer/Screenplay/Story/Teleplay map to Director/Writer. Episode detail requests parse guest stars and the same crew subset. The profile URL uses the configured TMDb image CDN and a portrait-appropriate size.

Local NFO conversion maps `<actor>`, `<director>`, `<writer>` and `<credits>` into credits; empty names are ignored. Existing adult genre/tag behavior is left unchanged unless a test proves a name is duplicated solely because of the new structured mapping.

When TMDb succeeds, local NFO may fill only credit types whose TMDb snapshot is empty. It never unions into a non-empty TMDb type because completeness and identity cannot be inferred safely from names.

## 3. Persistence Boundaries

Provider persistence writes movie/TV credits on the canonical movie or Series metadata returned by `UpsertCanonicalWithMerge`. Episode detail credits are written on the Episode metadata. Episode rows never receive copied Series rows.

Local persistence writes movie actors to the movie metadata and episodic actors to the concrete Episode metadata. This avoids the last episode NFO replacing the whole Series cast.

Credit replacement is atomic within its own repository transaction and any failure is returned as a scrape failure. Existing metadata/artwork transaction boundaries are not redesigned by this feature; all writes are idempotent so a retry converges after a partial outer scrape.

`mergeMetadataGraph` migrates source credits to target before hard deletion. Duplicate target relationships are retained once with deterministic minimum order; non-duplicates are rebound to target.

## 4. Emby Read Contract

`EmbyService` adds person/credit queries through the repository container:

- Item payload resolves credits for the item's metadata.
- Episode resolution combines by type: Episode rows replace matching Series types and inherit every missing Series type.
- Series payload resolves Series credits directly.
- Season payload does not synthesize separate cast in the first version.
- `People` entries expose credit role/type and person image tag.

Person list supports paging, name search and ID filtering. `Items` branches to the person query only when `IncludeItemTypes` exclusively requests `Person`; mixed media/person requests remain unsupported rather than silently changing existing media pagination.

Person detail is resolved before media lookup because person UUIDs and metadata UUIDs share the same route shape. The response uses `Type=Person`, `ImageTags.Primary=person.ID` when a profile URL exists, and provider IDs where available.

`EmbyService.ImageURL` falls back to person `ProfileURL` when the item ID is a Person. The existing unauthenticated item image handler, SSRF checks, proxy caching and placeholder response remain unchanged.

## 5. Backfill

No migration fabricates actors from legacy `genres` or names. The existing manual library scrape endpoint sets `IncludeMatched=true`; after deployment, an administrator can explicitly re-scrape a library to populate actors. This preserves rate control and current task progress reporting.

## 6. AI Localization

Use the existing `AIService` and a persisted `metadata.people_ai_translate` setting exposed as a SettingsPage toggle. Translation runs after the authoritative credit snapshot is persisted:

1. Persist the authoritative people and credit snapshot, then signal a service-lifetime background worker; scraping never waits for AI.
2. On startup, periodically, and after a signal, query people/credits whose original text is non-Chinese and whose display value still equals the original.
3. Resolve a context-aware cache before calling AI. Person-name cache keys use the stable Person identity; role cache keys use the Metadata identity. Both also include kind, original text, target language and prompt version.
4. Person entries include up to three associated movie/Series titles. Role entries include the current title, original title, year and media kind.
5. Deduplicate exact cache keys, then split misses at the first of 100 entries or the input-character limit. Submit each batch through Responses API without tools.
6. Parse and validate exact keys and non-empty Chinese values. Persist successful cache rows, then update display fields only when the target ID, original text and untranslated display value still match the request snapshot.
7. Any request, validation or conditional-update miss keeps the current display text and does not fail scraping. A later worker pass may retry unresolved rows.

`TranslationCache` is a reusable result dictionary, not a task queue. Database source/display fields remain the durable pending-state source, so a restart can rediscover unfinished work without persisting an in-memory queue.
Its source and translated values use text columns so long role values remain cacheable after credit persistence.

## 7. Compatibility and Rollback

- Existing media JSON and web API shapes are unchanged.
- Missing actor tables contain no required legacy rows, so AutoMigrate is additive.
- Empty or failed remote cast data must not corrupt existing media fields.
- Source rollback removes the new read/write paths. Additive person tables may remain unused; no destructive rollback migration is required.

## 8. Key Trade-offs

- Profile images use the existing proxy/cache instead of a new persistent person artwork subsystem. Add persistence only when remote avatar durability is a demonstrated requirement.
- NFO-only people use local normalized-name identity because NFO supplies no stable ID; they are intentionally not auto-merged with TMDb identities.
- Actor subscription remains a separate feature and is not coupled to People metadata.
- Director/Writer are included because Emby treats them as first-class People and they reuse the same relation model; Producer/Composer remain deferred.
- Douban people fallback is excluded because the current provider has no people contract and copying third-party mobile API credentials is not acceptable.

## 9. AI Runtime Configuration and Web Search

Extend the existing `APIConfig` row with `Model` and `WebSearchEnabled`; AutoMigrate adds the columns without a separate migration. `APIConfigService` owns the public view, patch, persistence, and resolved runtime projection. A non-empty database model overrides `cfg.AI.Model`; an empty value retains the configuration-file fallback.

The active admin route remains `/admin?tab=api`. Only its `APIConfigsPanel` OpenAI editor receives a model input and web-search toggle; the unused legacy page is not duplicated.

`AIService.Chat` sends the transcript to `<baseURL>/responses` with `tools: [{"type":"web_search"}]` only when web search is enabled, then extracts text from message output content. `TranslatePeople` also uses `<baseURL>/responses`, but without tools so its JSON translation remains offline and deterministic; smart search and recommendations continue using `<baseURL>/chat/completions`. Unsupported compatible providers return their upstream error so the UI never claims an offline answer was web-backed.
