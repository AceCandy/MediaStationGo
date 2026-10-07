# HuangGuo AI Catalog Isolation

## 1. Scope / Trigger

Read before changing `internal/huangguoai`, `huangguoai_*` persistence, or shared
Web/Emby/scanner/organizer/playback consumers. This catalog is independent of
HongGuo, HuangGuo Video, old CloudFront and article pages.

## 2. Signatures

- Source: `huangguoai`; library type: `huangguoai`; 15 independent model tables
  from `model.HuangGuoAIModels()`. No `metadata_items` surrogate.
- `/api/catalogs/huangguoai/works` is local search/list; `/search` is upstream
  search. Both are paged. `/works/:sourceID/state|favorite|media|episodes|refresh`
  retain numeric source IDs as strings, never UUIDs or display IDs.
- Display IDs: Movie `hga-work-UUID`, Series `hga-group-sourceID`, virtual S01
  `hga-season-workUUID`, Episode `hga-episode-episodeUUID`.
- Path tag: `[huangguoai-sourceID]` in the work directory or filename. New Movie
  downloads use `<work>/<work>.mp4`; Series use `Season 01/S01Exxx.mp4`.
  Movie keeps episode 1 internally. Existing download paths are never rewritten.
  Shared library `/series` returns mixed Movie/Series work cards.
- Settings: `huangguoai.enabled`, `huangguoai.download_root`, source-specific
  scheduler keys. See download config service for current supported keys.

## 3. Contracts

- `ai-huanlian`/`ai-mogai` -> Movie; `ai-duanju`/`ai-manju` -> Series. Counts and
  completion never determine type. Conflicting cross-type membership suspends
  projection; real Movie episodes beyond 1 remain metadata only.
- Works/episodes keep UUIDs on upsert. Favorites use user/sourceID; progress uses
  user/sourceID/episode number. Renaming/rebinding cannot reset these identities.
- Source disable stops new jobs/downloads/bindings; preserves existing files and
  states. `Wait` closes admission, cancels every admitted job and joins them.
  Refresh wakes coalesce without losing a wake during a running manual refresh.
- Discovery requires discover permission plus adult/profile visibility. File
  consumption needs visible bound files; an image can use discovery authorization
  or visible-file authorization. Admin role never bypasses profile adult locks.
- Download re-resolves media per attempt; credentials, keys and signed media URLs
  stay transient. Verify duration and complete decode before no-overwrite publish.
  For HLS, persist the complete media playlist duration returned by `Download`
  and compare the first video track duration against it. Preserve all audio,
  including audio starting before video; longer container duration alone is
  allowed. A difference over 2 seconds from page
  metadata produces a numeric-only warning, not a rejection. Direct MP4 retains
  page-duration verification. Complete current-source transfer does not establish
  that the upstream supplied the complete story; retain that warning explicitly.
  Completed output and scanner ingestion are separate stages.
- HLS resources (segments, maps and keys) retry transient request/read failures,
  short bodies and HTTP 408/429/500/502/503/504 at most three times, with
  cancellation-aware 1/2-second waits. Keep completed resources during retries;
  exclusively create each attempt file and remove only its failed partial file.
  Unwrap `url.Error` before classifying `net.Error`; the wrapper alone is not
  evidence of a transient network failure. Range/protocol, size and disk errors
  fail immediately. Transient HTTP status handling precedes range validation;
  validate `Content-Range` on successful range responses, not error responses.
  Public diagnostics contain only resource sequence numbers,
  fixed categories, byte counts and attempt counts; never raw errors or URLs.
- HLS transfers prepare shared maps and generation-specific keys in playlist
  order, then download at most two segments concurrently per task. Keep local
  filenames and manifest order based on original segment indices, including
  IV/media-sequence and discontinuities. Serialize cumulative progress callbacks
  and count each completed segment once; retain existing map/segment byte
  accounting. A terminal transfer error cancels the sibling and joins both
  workers before returning, so caller-owned cleanup cannot race with writes.
  Preserve the first failure and never merge a partial transfer. Both shared
  map preparation and segment completion enforce the total-media byte limit.
  This is per-task concurrency; source task concurrency remains independent.
- Local HLS remux probes up to 30 MB and analyzes up to 30 seconds before stream
  copy, because some sources introduce video after several seconds of audio.
  Recovery is local and bounded to three fresh, non-overwriting outputs. Only
  the fixed `sample rate not set` diagnostic enables video-copy/all-audio AAC
  recovery; disk, permission and invalid-input errors never enable it. Use
  `-xerror` during audio encoding, then retain the independent complete decode
  with `-err_detect explode` on the resulting MP4. Applying `explode` while
  probing these source audio headers can prevent video parameters being found.
  Audio recovery uses `-copyts -start_at_zero` to keep a common input origin:
  successful mux/decode and equal track durations alone do not prove alignment.
  Require the same audio track count and each audio-to-video start offset within
  0.25 seconds of the input; preserve legitimate leading audio. Only video
  duration mismatch enables `-dts_delta_threshold 1` remux. `copyts` disables
  that correction, so combined recovery omits it and must still pass offset
  checks. Final video duration retains `max(2 seconds, 2%)`; never change the
  playlist duration, drop corrupt packets/audio, or publish by weakening checks.
  Capture bounded FFmpeg stderr with `Cmd.Output`, then return only fixed error
  categories or the numeric exit code; never persist arbitrary stderr text.
  Persist the private `huangguoai_downloads.hls` flag with transfer handoff so
  independent verification/restart retains video-duration validation. The new
  column defaults to false: historical staged files retain strict total-duration
  validation until retried. Direct MP4 and all HongGuo downloads keep their
  total-duration rules and tolerances. Missing/invalid video-track duration must
  fail, never fall back to container duration for HLS.
- New download work directories use the fixed source category labels `AI短剧`,
  `AI漫剧`, `AI换脸`, `AI魔改`, then 64 letter buckets `aa` through `hh`:
  CRC32 IEEE(sourceID) modulo 64, encoded as two base-8 letters `a` through `h`.
  Series layout: `<category>/<bucket>/<title> [huangguoai-ID]/Season 01/S01Exxx.mp4`.
  Movie layout: `<category>/<bucket>/<title> [huangguoai-ID]/<title> [huangguoai-ID].mp4`.
  Missing source categories reject enqueue. Existing placements remain stable;
  moving historical files and download paths is a coordinated operational action,
  never an automatic startup migration. Scanner/organizer identity uses full paths.
- Public Media keeps `metadata_id IS NULL`. Source library scans skip ordinary
  metadata/sidecars. Movie binding accepts no season/episode coordinates or legacy
  S01E001, and always binds the real internal episode 1; other coordinates reject.
  Series still require actual S01 coordinates. Organizers retain the source tag,
  confirm coordinate-free Movies through canonical work kind without classification
  conflict, and preserve legacy S01 paths. They reject moving bound source files
  into ordinary libraries before file transfer. Auto-mark preserves completed rows.
- Work qualification/counting precedes pagination; hydrate only current-page
  files. Scope node work IDs before joins; SQL LIMIT alone does not bound scans.
- A successful `/works/:sourceID/media` page with no visible files returns
  `items: []`, including empty and out-of-range pages. Do not serialize nil
  slices as `null`; assert the HTTP payload for empty pages. Discover details
  show work metadata and download actions only, without local file/confirmed
  episode sections or their media, episode and favorite-state requests.

## 4. Validation & Error Matrix

| Input/state | Required outcome |
| --- | --- |
| Locked profile/global adult off | 404 on source endpoints, even for admin |
| Non-admin download request | 403 |
| Invalid ID/page/category/rank | 400 |
| Missing classification/true episode | Pending metadata, no invented projection |
| Disabled source scan | Keep old bindings; defer new binding |
| Preview/unknown duration/unsupported encryption | Fail safely, no completed file |
| Complete HLS differs from page duration | Warn; verify video track against playlist duration and fully decode all tracks |
| HLS video duration matches but audio extends total duration | Preserve all audio; allow after full decode |
| Missing ENDLIST/failed or incomplete segment/checked duration mismatch/decode failure | Fail safely, no completed file |
| Lost lease/cancel/publish collision | No overwrite or another attempt's deletion |
| Completed auto-mark predecessor | Preserve position, timestamps and events |

## 5. Good / Base / Bad Cases

Good: a one-episode AI Manju still exposes Series/S01/Episode; a Movie's scanned
S01E001 file exposes Movie without a visible season. Base: fileless discoveries
stay in Discover. Bad: use title equality, source count or current filter category
as identity/type evidence; scan files as ordinary metadata to make them visible.

## 6. Tests Required

Run `TestHuangGuoAI*` in repository/service/handler with isolated
`MEDIASTATION_TEST_POSTGRES_DSN`; source package parser/HLS tests separately.
Assert identity, binding, derived library/latest time, projection with zero legacy
metadata, favorites/progress/NextUp, permission boundaries, leases, cancellation,
verification, source disable/re-enable, organizer coordinates and shutdown wakes.
`TestHuangGuoAIWorkPagePlanAndExactCounts` checks 2,000 works/4,000 episodes, actual
EXPLAIN rows/loops for public page payloads and exact totals, plus global/Latest/Web
behavior. Do not claim it covers every filter or generic prepared plan.
Run Web lint/build and `node web/scripts/check-huangguoai.mjs` with local Vite on
4179. This uses synthetic responses only. Close browser/server/test DB afterward.
Live acceptance evidence is in `docs/huangguoai-design.md`; never commit temporary
media, raw HTML, keys or signed URLs. Real posters and player-device QA are separate.

## 7. Wrong vs Correct

Wrong: infer four real episodes from `episode_count=4`, clear S01 coordinates when
organizing a Movie, or place a source UUID into a source-ID API path.
Correct: persist reported counts separately, bind confirmed episode coordinates,
preserve the numeric source key and existing internal UUID, and use explicit source
projection branches through Web/Emby and shared playback state.

Discover `downloaded` means at least one completed download record, not all
episodes complete, current file existence or ingestion. Hydrate this flag in one
batch scoped to the current list/search rows; details use the same helper.
Download work summaries include `kind: movie|series|""` from canonical works,
joined after work pagination. Missing work metadata keeps the download visible
with empty kind; UI uses neutral task labels. Movies show Movie/部/正片; Series
show Series/集/E-number. Retry/cancel messages use generic download-task wording.

Download work summaries expose each processing phase count in addition to the
compatible aggregate `active`. Filter and page works before aggregating their
whole episode queues; never truncate a work's counts by the selected status.
`TestHuangGuoAIDownloadKindPathsAndLegacyRetry` covers all four category paths
and stable historical paths on re-enqueue/retry.
`TestHuangGuoAIMovieBindingWithoutEpisodeCoordinates` covers coordinate-free
Movie binding, legacy first files, invalid Movie coordinates and strict Series
binding. The transfer/verify/publish test binds its downloaded Movie through
scanner construction. `TestHuangGuoAIOrganizeMovieWithoutCoordinates` rejects
unconfirmed/series/conflicting coordinate-free files and retains Movie identity.
`check-huangguoai.mjs` covers mixed download labels and Movie details.

`TestHuangGuoAIListDownloadedBadge` and
`TestHuangGuoAIDownloadWorkStatusCounts` verify these projections in PostgreSQL.


## Download execution summaries

- Both Movie (one internal episode) and Series keep one task execution per source
  work. The ID is a deterministic UUID namespaced with
  `huangguoai_download:<sourceID>`; SourcePath stores `huangguoai://<sourceID>`.
  HongGuo identities and queues remain separate; no schema migration is needed.
- Download rows remain authoritative for leases, retries, verification and
  publication. Summaries count the complete work queue using its source-ID index;
  pending phases remain running, and completion requires every row completed.
  Serialize refresh with a per-system/work transaction advisory lock before
  reading a fresh queue snapshot and upserting the stable task ID.
- Refresh only after committed enqueue/action operations and worker entry/exit,
  never on byte heartbeats. Summary failures must not alter business outcomes.
  Startup reconciles running/interrupted summaries once and joins on shutdown,
  including a terminal queue commit followed by a crash before summary refresh.
- Each episode/stage writes a log-only handle, without execution rows or active/
  recent tracker entries. Names use source ID and episode number; source titles,
  title-bearing paths and raw upstream errors never enter summaries or logs.
  Only sanitized `Resolve`, `Download` and media-verification errors receive the
  private `huangGuoAIDownloadError` marker. Persist the same safe diagnostic in the
  download row and task log; unmarked database/other errors remain generic.
  URL-only redaction is insufficient for arbitrary errors containing titles,
  credentials or filesystem paths.
- `TestHuangGuoAIDownloadWorkTaskLifecycle` covers Movie/Series, supplement,
  mixed states, retry/cancel, recovery, concurrency and cross-system identity.
  `TestHuangGuoAIDownloadSummaryFailureKeepsPublicationAndLogs` verifies publication
  despite missing summary storage and safe diagnostics including upstream errors.
  The transfer/verify/publish integration test asserts both stages share one task.
  `TestHuangGuoAIDownloadHLSCompletenessAndDuration` verifies playlist duration
  survives persistence/reclaim, metadata mismatch warns and publishes, incomplete
  playlists/segments fail, and incorrect video duration or decode failure never
  publishes. `TestHuangGuoAIDownloadUnexpectedErrorsRemainPrivate` injects an
  unmarked persistence error to verify that neither row diagnostics nor task logs
  expose its private text. Never discard `Download`'s duration and later verify an
  HLS output against page metadata. Leading audio is preserved and allowed only
  for HLS, the flag survives independent claim, and direct MP4/HongGuo retain
  their stricter container-duration behavior.

`TestFetchResourceRetry`, `TestFetchResourceCancellationAndExistingFile`,
`TestFetchResourceHTTPRetryClassification` and
`TestDownloadHLSRetryPreservesCompletedSegments` verify bounded recovery,
failed-file cleanup, cancellation during read/backoff, no overwrite, unchanged
range/headers, key/map recovery and progress without double counting.

`TestDownloadHLSDelayedVideoParameters` uses a synthetic late-starting video
track to reproduce the insufficient-probe failure, asserts both output tracks
and full decode, and checks safe stderr classification through `Download`.
`TestHLSMergeErrorKeepsDiagnosticsPrivate` covers known and unknown tool errors
without retaining titles, paths, URLs or credentials.
`TestHLSMergeRecoversTimestampJump` keeps all frames, fixes a five-second jump,
preserves authored pauses and rejects a false playlist duration.
`TestHLSMergeAudioRecoveryPreservesEveryTrack` checks real AAC encoding, both
audio tracks, combined recovery, leading audio, collision, storage and cancellation.
`TestHLSMergeRejectsAudioTimingLoss` rejects lost tracks, invalid timing and
equal-duration outputs with displaced audio; transport-only mocks supply a
synthetic ffprobe result rather than treating placeholder bytes as real media.
`TestHuangGuoAIHLSRequiresValidVideoDuration` rejects missing, invalid and
nonfinite video durations even when the container duration matches. The opt-in
`TestHuangGuoAIDownloadHLSLive` checks real transfer, reclaim, full decode and
publication using an isolated PostgreSQL schema and test-cleaned media.

## Cross-attempt HLS segment resume

### 1. Scope / Trigger

HuangGuo download transport failures and explicit retry. Complete-file verification
and publication retain their existing independent leases and checks.

### 2. Signatures

`Client.DownloadResuming(ctx, media, dir, previousDir, progress, checkpoint)`;
`HLSResumeError` marks a failed transfer with complete cached segments.
`segment-NNNNNN.ts|m4s.resume` contains only `Identity`, `Size`, `SHA256`.
Use existing `staging_path`; no schema or public API changes.

### 3. Contracts

Use a private directory for each lease. Copy previous complete regular files with
exclusive writes and verify size/digest; never share writable files or hard links.
Identity hashes the resolved playlist (URLs/ranges/order/sequence/IV), Referer,
and freshly downloaded generation-specific keys and maps. Signed URL changes
conservatively invalidate cache. No URL, signature or key plaintext in records.
Before switching `staging_path`, finish copying, sync records/files and stage plus
its parent, then run the fenced checkpoint callback; only afterward retire old
stage. Copy/cancel/checkpoint failures retain the previous database path.
Each completed segment gets a synced record before progress is reported. Reused
bytes count once toward progress and the total-media limit. A failed transfer
retains only segments/records; keys/maps/manifests are removed. Successful transfer
or merge failure exits resume mode; independent decode/duration failures cannot
reuse suspect data. Existing `Download` remains non-resuming for other callers.

### 4. Validation & Error Matrix

- Missing/corrupt record, nonregular/truncated/corrupt file -> download again.
- Changed playlist/key/map -> invalidate affected cache conservatively.
- HLS changes to direct MP4 -> discard obsolete HLS cache; direct transfer has no
  segment resume, including when the new direct response is incomplete.
- Snapshot storage failure/cancel/checkpoint failure -> retain old checkpoint.
- Partial transfer with complete segments -> `HLSResumeError`; no publication.
- Media-size limit or merge/verification failure -> discard current cache.

### 5. Good / Base / Bad Cases

Good: first two segments survive failed third, new lease requests only missing
segments. Base: no records in historical failed stage -> full download. Bad:
skip by filename alone, strip signed URL query parameters, or advance database
path before the new snapshot is durable.

### 6. Tests Required

`TestDownloadHLSResumeAcrossAttempts` checks unchanged reuse, sequence/key/map
changes, same-size corruption, truncation, missing records, symlinks and accounting.
`TestDownloadHLSResumeCancellationAndMergeFailure` checks joined blocked sibling,
partial cleanup and non-resumable merge failure. `TestHLSResumeSnapshotCheckpoint`
checks snapshot-before-checkpoint, cancellation, collision and safe diagnostics.
`TestHuangGuoAIDownloadResumeRetryAndPublish` uses isolated PostgreSQL and synthetic
HLS for failure/retry/new lease, intermediate Resolve failure, reuse, complete
handoff, strict independent verification/publication and final cleanup.

### 7. Wrong vs Correct

Wrong: update the task path, delete previous stage, then begin snapshot copying.
Correct: copy and persist the snapshot first; commit the fenced task path next.

## Scheduled new-work download supplement

### 1. Scope / Trigger

Task Center owns `huangguoai_download_supplement`; Download Space owns per-work
download progress. Supplement acquires new works, not episode catch-up.

### 2. Signatures

`POST /api/tasks/definitions/huangguoai_download_supplement/run` accepts `{count}`.
The existing schedule endpoint accepts `{enabled,interval_seconds,count}`.
Settings use `huangguoai.download_supplement.enabled`, `.interval_seconds`, `.count`.

### 3. Contracts

Defaults: disabled, 86400 seconds, 10 works. Counts are integers in 1–100;
manual counts never change the saved schedule. Persist all schedule values in
one transaction; omitted count preserves the current value. HongGuo uses its
own independent keys and retains its existing candidate ordering.

Select canonical works with a valid source ID/category-kind pair, no projection
error, and 1–10000 confirmed episodes. Movies require only confirmed episode 1.
Any download placement or episode history excludes a work regardless of status.
Order by discovery `created_at` descending, falling back to canonical work
`created_at`, then work creation time and ID. Raw `source_created_at` is not a
verified release timestamp. The work-row lock, episode-history recheck and
first-placement conflict guard prevent concurrent duplicate acquisition.
Ordinary manual enqueue still adds newly confirmed episodes to existing works.

Return actual candidate/work/episode/skipped/failed metrics. Per-work failures
preserve other committed works and fail the round. Do not hydrate, retry old
failures or refill short rounds. Scheduler Stop cancels and joins manual and
periodic runs. Supplement logs contain safe aggregate counts, never work titles.

### 4. Validation & Error Matrix

Non-admin -> 401/403. Invalid/fractional count or run body over 4 KiB -> 400.
Disabled source/missing root -> recorded background failure; no candidates ->
successful empty round. Overlapping runs -> rejection, not a second admission.

### 5. Good / Base / Bad Cases

Good: add two new series and retain failed/completed predecessors. Base: fewer
candidates than requested. Bad: treat a failed predecessor as never downloaded.

### 6. Tests Required

`TestHuangGuoAIDownloadSupplement*` covers filtering, first-discovery ordering,
legacy history, repeated/concurrent enqueue and ordinary manual catch-up.
`TestHuangGuoAISupplement*` checks defaults, config restoration, source isolation,
manual/periodic triggers, partial failures and joined shutdown. Run with isolated
PostgreSQL and race detection; the shared `TestHongGuoSupplementTaskHTTP` and
`web/scripts/check-hongguo-supplement-task.mjs` exercise both source task keys.

### 7. Wrong vs Correct

Wrong: cast raw source-date text or use task logs to select new downloads.
Correct: use local first-discovery time and authoritative download history,
then reuse the existing download transaction with the only-new guard.
