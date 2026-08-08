# Design: metadata-scoped Emby libraries

## Architecture

The query pipeline is split at the existing domain boundary:

```text
Emby container request + user
            |
            v
resolve library scope
  - global visible scope
  - physical library scope
            |
            v
MetadataItem count/order/page
            |
            v
batch-load visible MediaView versions
            |
            v
Emby item payload / MediaSources
```

`MetadataItem` owns work identity, display fields, hierarchy, and logical
pagination. `Media` owns physical membership, file facts, versions, and source
selection. No persisted membership state is copied onto metadata.

## Scope Boundary

Add the smallest query input needed to describe the two scopes used now:

- global: any Media visible to the user;
- physical: visible Media restricted to one resolved library ID.

The scope is passed through metadata query/count operations and through the
subsequent MediaView batch load. It is a value/query boundary, not a new
interface hierarchy or a virtual-library framework.

Future virtual-library implementations can add a metadata predicate or an
ordered metadata-ID set at this boundary. They must not change Emby item IDs or
the rule that playback sources come from user-visible Media.

## Physical Metadata Query

Movie and Episode membership is expressed as a metadata query constrained by an
`EXISTS` relation to valid Media:

```sql
SELECT ...
FROM metadata_items AS mi
WHERE mi.deleted_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM media AS m
    WHERE m.metadata_id = mi.id
      AND m.deleted_at IS NULL
      AND m.library_id IN (...visible scope...)
  )
```

The actual repository query must use GORM constructs compatible with SQLite and
PostgreSQL. Visibility filters, metadata type filters, search predicates, and
NSFW policy are applied before both count and pagination. The page order ends
with `mi.id` as a stable tie-breaker.

For media-derived sort values such as `DateCreated`, use an aggregate over only
the Media rows inside the same scope. Do not fetch an arbitrary physical row and
sort after pagination.

## Series Hierarchy

Series and Season membership is derived through their Episode descendants:

```text
Series metadata
  <- Season.parent_id
  <- Episode.parent_id
  <- visible Media.metadata_id
```

Series and Season queries group/select the corresponding real metadata rows at
the database layer, then count and paginate those rows. They do not prefetch all
Episode Media or synthesize virtual IDs. Episode child queries use the same
physical scope and preserve existing parent filters.

Mixed movie libraries retain the current classification rule. The two logical
branches produce metadata identities, are combined and sorted deterministically,
and are paginated only after logical deduplication.

## Payload and Playback Loading

After selecting metadata IDs, batch-load `MediaView` rows with the same user's
allowed/hidden-library and NSFW filter. Group the views by metadata ID and reuse
existing payload/version helpers where their contracts still fit.

Change `mediaVersionSiblings` (or its shared caller contract) so user visibility
is mandatory. The following paths must all use the filtered sibling set:

- list payload construction;
- item detail;
- Resume and Latest projections;
- PlaybackInfo.

When a requested metadata item has no visible playable Media, it must not leak a
hidden sibling as fallback. Existing concrete MediaSource IDs, preferred/last
Media selection, track loading, and stream selection remain based on `Media.ID`.

## Identity and Parent Contracts

- `MetadataItem.ID` remains the Emby Item ID everywhere.
- `Media.ID` remains the MediaSource and concrete playback ID.
- User favorite and playback state remain shared across libraries by metadata
  identity.
- A physical library request projects that requested container as `ParentId`;
  membership does not create a permanent owner on metadata.
- Global projections deduplicate by metadata ID and do not choose one physical
  library as the canonical owner.

## Repository and Service Placement

Prefer extending the existing metadata repository/query layer and existing Emby
service helpers. Add no general-purpose membership table, no provider registry,
and no interface with a single implementation. Keep count and page query
construction shared enough that filter drift is structurally difficult, while
avoiding an unrelated repository refactor.

## Compatibility and Migration

- No schema migration or historical backfill is required.
- Persisted catalog metadata and existing Media links remain valid.
- Public Emby response shapes and IDs remain unchanged; only duplicate counts,
  pagination correctness, completeness, and visibility are corrected.
- PostgreSQL and SQLite must execute equivalent query semantics.

## Rollback

The change is code-only. Reverting the metadata-scope repository/service changes
and their tests restores the previous query behavior; no data rollback is
needed. Existing people/artwork localization changes are outside this rollback
boundary.

## Risks

- Hierarchical Series queries can accidentally diverge from Episode visibility;
  focused hierarchy and permission tests must pin this down.
- Count/page filter drift can return mismatched totals; both must be built from
  the same scope/filter input and tested together.
- Media-derived sorting can become nondeterministic across versions; scoped
  aggregation plus metadata-ID tie-breaking is required.
- Shared metadata across allowed and hidden libraries makes fallback selection
  security-sensitive; sibling source loading must never bypass user filters.
