# Rich Douban detail dialog

## Goal
Display persisted Douban information in an attractive wide administrator dialog.

## Requirements
- R1: No separate dialog header or close button. Close via backdrop/Escape; follow the app theme.
- R2: Display poster, identity, directors/writers/cast, genres, countries, languages, all release dates, durations, aliases, IMDb ID, rating count/distribution, synopsis, honors and awards when present.
- R3: Reuse discovery MediaCredits circular avatars and local people image storage/variants/auth. Keep references in the Douban center, without creating canonical people/media records.
- R4: Preserve raw API JSON. Store supplementary webpage fields in douban_subjects. Reads perform no detail requests. Manual/data-center scheduled refresh share TTL, gate, proxy and credentials; failed supplementary reads preserve old data and report partial refresh.
- R5: No trailers, stills, external ratings, social comments, editing, bulk backfill or extra theme controls.

## Acceptance
- Available fields render, missing fields are omitted. Wrapped/degraded payloads remain readable.
- Wrong-page/challenge responses cannot replace supplement data. Late supplement/image writes cannot overwrite newer state.
- Avatars persist across provider restart, use authenticated local URLs, support variants/HEAD and failures.
- Responsive light/dark dialogs have no horizontal overflow; backdrop/Escape close and return focus; refresh errors preserve prior presentation.

## Approval
The user reviewed two design iterations and explicitly approved implementation after requesting removal of the top header (2026-10-11).

## Approved follow-up: refresh ownership and navigation
- Move Douban Data Refresh to Common Tasks, retaining scheduler configuration and historical logs/executions.
- Scan due Movie/Series subjects directly from the center, without requiring local media bindings; preserve TTL and retry policy.
- Place the admin-only Data Center link in Viewer Space between Discover and Me.

## Approved follow-up: bounded API-only refresh
- Periodic/task-center refresh contacts only Rexxar Movie/TV detail endpoints, capped at configurable 1–100 subjects (default 100), oldest basic success first.
- Only base freshness/quality/retry determines eligibility; fresh base rows issue no request even when rich data is missing or old.
- Preserve independent rich data/images/freshness and local metadata. Keep shared TTL, serialization, safe persistence and retry.
- Expose accurate changed/unchanged/skipped/failed/remaining/HTTP attempt metrics.
- Add base-only single-subject refresh alongside explicit full refresh, with existing full behavior as the compatibility default. Show separate base and supplement update times.
- No adaptive scheduling, new people tables or automatic celebrity/image/webpage work in this follow-up.
