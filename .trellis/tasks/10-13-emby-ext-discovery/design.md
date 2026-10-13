# Design

## Routes
Authenticated `/emby-ext/v1`: GET capabilities, GET rankings, GET rankings/:id/items?page=N, GET calendar?weekday=1..7, POST library/matches (1..100 items; key, source, external_id, media_type, optional season+episode). Reuse Emby token, active-user and target-user checks and Discover permission. Public data projections only.

## Rankings
Reuse existing section definitions and provider/center calls, preserve source identity/order and native page size. No cross-source fallback disguised as the requested ranking. Response identifies requested ranking, page, page_size, items and nullable total; continuation is explicitly source pagination, not a snapshot-stability promise. Existing source cache/refresh behavior is retained and documented.

## Calendar
A small Bangumi-owned snapshot table stores the complete normalized public weekly calendar and success time. The provider shares one in-flight refresh. Cold requests wait for the first fetch; fresh snapshots read locally; snapshots older than six hours return immediately and trigger one service-owned refresh; failures retain the persisted payload and back off. Service shutdown prevents new work, cancels and joins refresh. Validate upstream day IDs and item IDs. Preserve weekly schedule timezone Asia/Tokyo; unknown exact broadcast time and season/episode remain null. Existing Discover rail projects its first 24 entries from the same calendar.

## Matching
A bounded repository query matches provider/kind/external ID. For series, visible files attached to series/season/episode establish availability; season+episode narrows to exact episode. Apply existing media filter including allowed/hidden libraries before projecting IDs, deduplicate versions. Unmatched/hidden/fileless all return an empty match list. Never return a stream URL.

## API catalog
Keep existing Emby entries intact; extension catalog separate, renderer selects protocol before category/search. Extension paths are literal, no root aliases.

## Migration
Add only calendar model to current schema registry. No historical/source imports. Existing source stores and routes stay usable.
