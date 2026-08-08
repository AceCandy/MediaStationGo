# Design: Persist TMDb Discover Catalog

## Boundaries

`discoverFeedHandler -> ScraperService catalog queue -> TMDb full match ->
MetadataRepository + ArtworkStore + PeopleImageStore -> metadata tables and
DataDir artwork/people`

The discover handler remains responsible for collecting external results. The
scraper owns provider hydration because it already owns canonical metadata,
artwork, and credit persistence. No `Media` row is created for catalog-only
items.

## Data Model

Add nullable `MetadataItem.CatalogHydratedAt`. A non-null value means the
catalog hydration completed after metadata, artwork, and credits were written;
it does not mean a playable media version exists. AutoMigrate owns the column.

## Queue and Lifecycle

- `ScraperService` owns a service-lifetime pending map keyed by
  `tmdb:<entity-kind>:<id>` and a coalescing wake channel.
- `/discover/feed` enqueues TMDb results after assembling its response.
- A single worker drains pending items sequentially, so duplicate section rows
  do not fan out duplicate provider requests or credit replacement races.
- The worker uses the container context and is joined during `Container.Close`.

## Hydration Flow

1. Resolve the canonical metadata by TMDb identifier; skip an item whose
   `CatalogHydratedAt` is already set.
2. Fetch `GetMovieMatch` or `GetTVMatch`, which returns full display fields and
   the loaded TMDb credit snapshot.
3. Reuse `persistProviderMetadata` with an empty `model.Media` value; this
   writes canonical metadata, managed artwork, and people/credits.
4. Mark the resulting metadata row hydrated only after all three persistence
   stages succeed.

Each repository operation remains transactional as it is today. External
   requests happen outside repository transactions. A partial failure leaves
   the completion timestamp empty so later observations can retry.

## Compatibility

Existing image proxy caching remains available for ordinary discover display.
Hydrated artwork and people are authoritative local files served through the
existing `/api/artwork` and Emby people image paths.
