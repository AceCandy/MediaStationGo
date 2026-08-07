# Reuse canonical metadata before provider lookup

## Goal

Reuse authoritative canonical metadata identified by an exact provider ID before
performing another online lookup, and defer creation of local metadata until
provider lookup has definitively failed.

## Background

- A path such as `Snow White (1938) [tmdbid=408]` is parsed into the exact
  identifier `(tmdb, movie, 408)`.
- `MediaRepository.Upsert` currently requires a metadata row before inserting the
  media row and creates a scan-generated `Source=local` record when none is set.
- Updating through `UpsertCanonical` can overwrite an existing provider-backed
  canonical record with sparse scan-generated `Source=local` data.
- Scraping currently performs provider lookup without first reusing an existing
  authoritative canonical record.

## Requirements

- Resolve cache candidates only by the exact tuple `(provider, entity kind,
  external ID)`; do not introduce title/year fuzzy matching.
- When that tuple already resolves to canonical metadata, bind the scanned media
  to the existing metadata without changing the canonical record or performing a
  provider request.
- When no canonical metadata exists, persist the scanned media with no metadata
  binding and keep its external-ID/path hints for the post-scan scraper.
- Media without a metadata binding remains absent from `MediaView` and other
  metadata-backed display reads until enrichment completes.
- A provider match creates and binds provider canonical metadata.
- A definitive provider no-match creates and binds eligible local metadata.
- Provider request errors must remain scrape errors and must not be converted to
  successful local fallback.
- Preserve recognition of directory names containing `[tmdbid=<id>]`, including
  non-ASCII titles.

## Acceptance Criteria

- [x] Scanning a media path whose exact identifier already belongs to canonical
  metadata binds the media to that metadata ID.
- [x] The scan does not overwrite the reused canonical title, details, or source.
- [x] Reused authoritative metadata does not trigger an automatic provider lookup.
- [x] A media path with no canonical metadata is stored without a metadata ID and
  remains pending for provider enrichment.
- [x] Unresolved media remains absent from metadata-backed display reads.
- [x] A successful provider match fills the media metadata ID after scanning.
- [x] A definitive provider no-match fills the media metadata ID with local
  metadata after scanning.
- [x] Existing no-match and provider-error fallback semantics remain covered and
  unchanged.
- [x] `白雪公主和七个小矮人 (1938) [tmdbid=408]` resolves TMDb ID 408.

## Out of Scope

- Metadata freshness or TTL policies.
- Fuzzy metadata reuse by title or year.
- Changes to provider priority after an authoritative cache miss.

## Notes

- The user confirmed that `media.metadata_id` may be `NULL` between scan and
  enrichment. The existing foreign-key relationship remains in effect for
  non-null IDs.
