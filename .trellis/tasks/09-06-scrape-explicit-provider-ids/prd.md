# Explicit provider IDs for automatic scraping

## Requirements

- Automatic scraping and pre-organize recognition must not search providers by title/year or automatically select search candidates.
- Query a provider only with its explicit ID from existing scan hints or sidecars. A failed lookup must not trigger name-based fallback.
- Preserve exact-ID canonical reuse, explicit manual search/apply, local NFO behavior, and explicit adult-code operations.
- Preserve media files and raw media records; unresolved scraping reports no_match and provider errors report error.

## Acceptance

- No IDs: zero provider requests, no automatic provider metadata creation.
- A supplied provider ID is queried directly; other providers without IDs receive no requests, including after lookup failure.
- Multiple IDs may fall through only to providers with their own ID.
- Automatic organize performs no name search. Existing NFO and explicit path IDs still work.
- Focused tests, compilation and independent diff review pass; database test limitations are reported.

## Approved scope

User approved the proposed implementation in this conversation. No schema change, historical rematching, or deletion of manual search features.
