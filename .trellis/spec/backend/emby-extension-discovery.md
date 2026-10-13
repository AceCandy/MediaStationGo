# Emby Extension Discovery

## 1. Scope / Trigger

MSG exposes source-center discovery to native players at `/emby-ext/v1`.
Change this contract alongside any extension route, calendar storage, match
projection or player API catalog change. LinPlayer integration is separate.
No personal Trakt/Bangumi authorization, additional ranking providers, stream
URL cache or administrative source diagnostics belong to this surface.

## 2. Signatures

- `GET /emby-ext/v1/capabilities`
- `GET /emby-ext/v1/rankings`
- `GET /emby-ext/v1/rankings/:id/items?page=1`
- `GET /emby-ext/v1/calendar?weekday=1`
- `POST /emby-ext/v1/library/matches`
- `GET|HEAD /emby-ext/v1/images/tmdb/:kind/:tmdbID/:type`

`BangumiCalendarSnapshot` is registered in `model.AllModels` and stores one
`key=weekly`, JSONB normalized payload and `fetched_at`. UPSERT only accepts a
newer fetch start time, preventing a late older response replacing newer data.
This is source-owned data, unrelated to local media lifecycle.

## 3. Contracts

All routes use Emby token/session authentication, current active/revoked user
checks and target-user protection. Capabilities is available without discovery permission; rankings/calendar/library_matching reflect the current permission. Content routes additionally require `can_view_discover`. No root/case aliases.

Capabilities includes `danmaku: {enabled, format: "bilibili_xml"}`; enabled reflects availability of the raw service, independently of discovery permission or upstream collection settings. Clients may automatically fetch authenticated same-origin `/api/danmu/:id/raw` and overlay other sources. Current coverage is HongGuo visible leaf media; empty body/XML is valid, and responses remain no-store.
JSON uses lower snake case, independently from Emby payload casing.

Rankings reuse `discoverSectionCatalog` (excluding calendar) and the existing
TMDb/Douban source-center calls. Do not invoke Discover's cross-provider
fallback. Preserve source order and native page sizes (20/24). Page is 1–500;
`total` and `has_more` are null because the existing projection lacks accurate
pagination metadata. Pagination is live source pagination, not a stable
snapshot. Empty page or page cap ends traversal. Keep existing center cache
policies; do not claim universal stale-while-revalidate for rankings.

TMDb relative image URLs point to the authenticated extension image alias,
which reuses `TMDbDataCenter.ServeImage`. Third-party absolute URLs remain
source image URLs: clients must never attach MSG tokens to those origins.

Calendar returns the complete weekly rules with `source=bangumi`,
`mode=weekly`, timezone `Asia/Tokyo`, weekday 1–7 and explicit null
`broadcast_time`, `season`, `episode` when unavailable. Do not invent dates,
episode numbers or exact airtimes. Optional weekday filters the full snapshot.
`updated_at` is the successful snapshot's fetch start time; `stale` means at
least six hours old. Fresh reads use PostgreSQL; stale reads return the last
successful snapshot immediately and trigger one process-local shared refresh.
Cold reads wait, but caller cancellation does not cancel shared work. Refresh
uses the service context and a 30-second deadline; failures retain data and
back off one minute. Refresh is access-triggered, not a new cron task. Shutdown
cancels service work and joins it. The existing Discover rail projects distinct
first 24 subjects from the same snapshot. No global cross-process refresh lease
is provided; concurrent instances may fetch independently.

Matching accepts 1–100 entries and at most 64 KiB JSON, including trailing
bytes. Each entry has a unique 1–128-byte `key`, `source` (tmdb/douban/bangumi),
positive numeric string `external_id`, and `media_type` (movie/tv/series/anime).
Optional season 0–10000 and episode 1–100000 must occur together for a series.
Normalize tv/anime to series for exact identifier lookup; never guess titles.
Only ordinary metadata with external identifiers is supported, not independent
NFO/HongGuo/HuangGuo catalogs. Apply current request/default profile, PIN,
allowed and hidden libraries, enabled/live library and actual file existence.
An explicitly restricted empty library list must produce zero matches, never
an unrestricted query. Return input-order `items[{key,matches}]`, with matched
`key`, Emby `item_id`, `item_type` (Movie/Series/Episode). Deduplicate versions.
Hidden, fileless and unmatched records all produce `matches:[]`; never expose
file paths or playback URLs.

The administrator page selects Emby/Emby extension protocol before category
and search. Switching resets category, retains query. Static extension entries
live in `embyExtApiCatalog.ts`, sharing the existing detail/copy renderer.

## 4. Validation & Error Matrix

- 400: invalid page/weekday/identity, duplicate keys, empty/oversized batch/body.
- 401/403: missing/revoked token, inactive/expired account or absent permission.
- 404: unknown ranking or missing image.
- 502: ranking upstream error or no usable calendar; sanitized public error.
- 503: disabled/unconfigured source, unavailable image service.
- 500: local match query failure; never convert DB failures to empty success.
- Stale calendar refresh failure: 200 old snapshot with `stale=true`.

## 5. Good / Base / Bad Cases

Good: cold calendar persists all seven weekdays and a restart serves it locally.
Base: an exact known work without visible files yields an empty match array.
Bad: use the 24-item Discover rail as the full calendar or substitute a Douban
ranking when the requested TMDb ranking fails.

## 6. Tests Required

- `TestExtensionCalendarPersistentRefresh`: all weekdays and >24 entries,
  persistent restart, coalesced cold/stale requests, failed refresh/backoff,
  old-write protection and unchanged Discover rail limit.
- `TestExtensionCalendarValidationAndCancellation`: invalid upstream identities,
  caller cancellation, service cancellation and no work after close.
- `TestExtensionMatchesExactIdentityAndVisibleFiles`: same numeric ID with
  different kinds/providers, exact special episode, absent/fileless/hidden
  matches, version deduplication, disabled libraries and invalid input.
- `TestExtensionRoutesAuthCacheAndMatching`: actual registered routes, token
  carriers, revocation/role/permission changes, PIN and empty allowed profiles,
  source cache reuse/order/errors, image GET/HEAD and bounded JSON.
- Run affected Discover/Emby regressions with a dedicated temporary PostgreSQL
  database and race detection. No production DSN.
- `web/scripts/check-nextup.mjs`: both protocols, categories/search/details/copy,
  three viewports, light/dark accessibility and non-admin denial; lint/build.

## 7. Wrong vs Correct

Wrong: refresh from each device, return a guessed next episode, then store a
playback URL with the calendar. Correct: MSG owns source snapshots and exact
visible library IDs; clients render them and ask Emby for current playback.
