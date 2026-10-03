# HongGuo On-Demand Danmu

## 1. Scope / Trigger

Applies to HongGuo App parameters, the Emby raw danmu endpoint, incremental
comment persistence, and service shutdown. This is separate from catalog
hydration, download, favorites, watched state and playback progress.

## 2. Signatures

- `PUT /api/admin/api-configs/hongguo`: `{enabled?, hongguo_app?: {cookie?,
  token?, user_agent?, device_id?, iid?, query?: Record<string,string>}}`.
- `DELETE /api/admin/api-configs/hongguo` clears all App parameters, not comments.
- `GET [/emby]/api/danmu/:id/raw`: existing token authentication; HongGuo emits
  `application/xml; charset=utf-8` with `Cache-Control: no-store`.
- `hongguo_danmus` primary key: `(source_id, episode_number, comment_id)`.
- `Client.Danmus(ctx, sourceID, episode, videoID, app)` returns successful
  windows even alongside an error. `HongGuoDanmuService.Get` requires a target
  previously authorized by `EmbyService.HongGuoDanmuTarget`.

## 3. Contracts

- Store source work ID plus episode number, never album or upstream video ID.
  Register only in `AllModels`, outside `HongGuoModels` catalog cleanup.
  No foreign keys to old tables, startup backfill, periodic jobs or prefetch.
- Historical IDs win; live unknown IDs append; sort by milliseconds and ID.
  Insert batches of 500 with conflict-do-nothing. Never delete absent comments,
  deduplicate by text, replace an episode, or modify existing comment contents.
- App parameters use the encrypted `api_configs.api_key` text column; reject
  plaintext encryption fallback and corrupt ciphertext. Public `hongguo_app`
  contains only configured booleans; never apply partial key masking to JSON.
  Omitted/empty individual inputs preserve old values; a supplied `query` object
  replaces that group. Empty configuration is anonymous; disabled skips upstream.
- Resolve parameters once per episode request. Use only allowlisted query keys
  and fixed HTTPS App hosts; do not follow redirects with credentials. Mapping
  fallback reads only the requested work detail and does not update catalog rows.
- The dedicated signer leaves `download_app.go` unchanged. Comment POST uses an
  empty stub; duration POST hashes its body. SM3 uses pinned MIT `gmsm`;
  Simon/Ladon attribution is in `internal/hongguo/danmu_sign.LICENSE`.
- Fetch at most 128 windows, 20,000 returned records, 2 MiB per response and
  25 seconds total, with 180 ms between windows. A repeated cursor or non-advancing
  time ends the fetch with partial data. Source counts do not prove completeness.
- Validate each time window before accepting its rows; offsets must be in
  `[start, min(next,duration))`. Deduplicate accepted IDs across windows. Preserve
  compatibility when `group_id` or `status` is omitted; provided fields must
  match the requested video and public status `1`, including numeric/string `1`.
- At most three shared fetch/save operations hold slots. Concurrent requests
  share only in-flight work keyed by source, episode, video and a SHA-256 digest
  of the resolved App parameter snapshot. Configuration changes/disable are
  observed before joining; never use plaintext credentials in flight keys/logs.
  Saturation returns history; there is no result cache or refresh cooldown.
  Canceling one waiter does not affect others; the last waiter cancels upstream
  and removes the flight so later callers cannot join canceled work. Responses
  own their byte slices. Saving has a separate five-second context; shutdown
  stops admission under the same mutex as WaitGroup.Add and joins requests,
  shared fetches and incremental saves. Completed flights are removed before
  publishing their result; response delivery does not wait for persistence.
- Permission checks precede comment reads/network. Only visible episodes,
  movie works and bound files are targets; groups/seasons/people return 404.
  Ordinary/NFO requests retain empty 200 text responses. XML uses seconds with
  millisecond precision, string IDs, nine p fields, escaped text and no user IDs.

## 4. Validation & Error Matrix

- Missing/hidden target or container -> 404, no comment read/network.
- Network/business/JSON error or empty result -> preserve historical comments.
- Invalid/oversized App input -> safe 400 without submitted values.
- Missing App configuration -> anonymous; invalid ciphertext -> historical only.
- Database read failure -> 500; asynchronous write failure -> response unchanged.
- NUL in source text -> strip before PostgreSQL insertion; XML strips invalid
  control characters. No credentials, response bodies or signed URLs in logs.

## 5. Good / Base / Bad Cases

Good: history 100 + live old 95/new 20 returns and persists 120. Base: disabled
upstream returns sorted history. Bad: using an album ID as ownership, treating a
smaller result as deletion, or fetching every episode from PlaybackInfo.

## 6. Tests Required

- `TestDanmuSigning`, `TestDanmuWindowsAndSafeFailures`,
  `TestDanmuResolvesOnlyRequestedEpisode`: vectors, identity snapshot, partial
  failures, bounded parsing and single-episode mapping fallback.
- `TestDanmuFiltersSourceWindows`: public/missing/malformed fields, wrong video,
  window boundaries, cross-window duplicate IDs and invalid-window partial data.
- `TestHongGuoDanmuCoalescesRequests`,
  `TestHongGuoDanmuSharedFetchCancellation`,
  `TestHongGuoDanmuFlightConfigurationIsolation`,
  `TestHongGuoDanmuCanceledFlightDoesNotRemoveReplacement`: same-key sharing under full
  slots, caller cancellation, independent response bytes, source/episode/video
  isolation, parameter updates/disable, persistence and shutdown under `-race`.
- `TestHongGuoAPIConfigIsolation`, `TestHongGuoDanmuMergeXML`,
  `TestHongGuoDanmuAsyncPersistenceAndClose`, `TestHongGuoDanmuTargetVisibility`,
  `TestHongGuoDanmuSaveFailureAndFallback`: secrets, merge, permission, persistence
  and close under real PostgreSQL and race detection.
- `TestEmbyDanmuRawRoutes`, `TestHongGuoDanmuMigrationIsolation`: both prefixes,
  ordinary response, authentication, repeated migration and cleanup isolation.
- `node web/scripts/check-hongguo-danmu.mjs`, frontend lint/build and existing
  `check-nextup.mjs`: forms, old provider payloads, catalog and responsive access.
- Fixtures requiring full schema must call `database.AutoMigrate`: the two API
  models share one table, and raw GORM AllModels does not add every API column.
- `MEDIASTATION_HONGGUO_DANMU_LIVE=1` explicitly opts into anonymous source tests.
  Logged-in completeness and YAMBy rendering require separate device validation.

## 7. Wrong vs Correct

Wrong: `DELETE WHERE source_id=?; INSERT liveRows`, or send `api_key` JSON through
the generic masked-key UI. Correct: `ON CONFLICT DO NOTHING` and dedicated
`hongguo_app` input/configured-state projection. Keep all old provider semantics.
