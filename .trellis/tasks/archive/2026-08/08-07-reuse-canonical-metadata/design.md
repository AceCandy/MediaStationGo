# Design

## Boundary Change

`Media.MetadataID` becomes nullable while a scan result is unresolved. The
foreign key still protects every non-null link. `MediaView` keeps its inner join,
so unresolved media is intentionally hidden from metadata-backed reads.

## Write Flow

1. Scanner builds a `Media` row with provider/path hints.
2. `MediaRepository.Upsert` preserves an existing path binding or resolves exact
   external identifiers through `MetadataRepository.FindByIdentifier`.
3. If an identifier resolves, the media binds to that canonical metadata without
   updating it and becomes `matched`.
4. If no identifier resolves, the media is inserted with `metadata_id = NULL` and
   remains `pending`.
5. The scraper groups unresolved rows independently, performs the existing
   provider chain, and writes `metadata_id` only after provider or eligible local
   persistence succeeds.

## Identity And Conflict Rules

- Resolution uses only `(provider, entity_kind, external_id)`.
- Multiple identifiers resolving to different canonical rows remain an error.
- Existing canonical rows are never updated by the scanner.
- Provider persistence retains the current controlled merge behavior.

## Compatibility

- Fresh databases create a nullable `media.metadata_id` column.
- Existing schema migration must remove the non-null/check constraint while
  preserving the foreign key and active metadata index.
- Raw media reads and scrape candidate queries accept an empty Go string as the
  representation of database NULL.
- `MediaView` remains unchanged and excludes unresolved rows by design.

## Failure Semantics

- Provider match: persist canonical metadata, bind media, mark `matched`.
- Definitive no-match: persist eligible local metadata and bind it; otherwise keep
  the row unbound and mark `no_match`.
- Provider/local read error: keep the row unbound and mark `error`.

## Rollback

The code change can be reverted without data conversion. Rows already enriched
remain valid; unresolved NULL rows would need a later scan/enrichment before code
that restores the non-null invariant could accept them.
