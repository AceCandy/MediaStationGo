# 完整媒体轨道探测与播放选择实施计划

## 1. Schema And Typed Contract

- [x] Add `MediaProbeMetadata` with `media_id` primary/foreign key, TEXT JSON, schema version, and probe timestamp.
- [x] Register the model in `AllModels`; add SQLite schema/foreign-key cascade, migration-copy, bootstrap-only detection, and PostgreSQL AutoMigrate/round-trip or static DSN-unavailable verification.
- [x] Define the versioned safe probe document and JSON round-trip/normalization helpers.
- [x] Add representative parser fixtures covering HDR video, three audio tracks, embedded subtitles, chapters, dispositions, missing fields, and forbidden filename/URL data.

Verify:

```bash
go test ./internal/model ./internal/database ./internal/service -run 'Probe|MediaProbe' -count=1
```

## 2. Shared Probe Persistence

- [x] Expand ffprobe JSON parsing to all declared streams/chapters while preserving scalar summary callers and ffmpeg fallback behavior.
- [x] Add repository methods for single/batch load and upsert.
- [x] Implement the shared probe-and-persist path with stable source snapshot, short transaction, stale-result rejection, scalar projection update, and cache invalidation.
- [x] Route local scanner, cloud scanner, manual reprobe, and PlaybackInfo repair through the shared persistence path without changing their scheduling semantics.

Verify:

```bash
go test ./internal/service -run 'FFprobe|LocalProbe|CloudProbe|TrackProbe|Reprobe' -count=1
```

Rollback point: the old scalar-only probe paths still compile before consumers switch to full documents.

## 3. Lazy Repair And Admin Backfill

- [x] Make detail/PlaybackInfo schedule missing/old records for every visible version, including plain local media, without blocking the response.
- [x] Add the admin-only per-library backfill endpoint and background task metrics.
- [x] Add the existing-style admin library menu command and API client call; use the global task panel for progress.
- [x] Test no startup auto-backfill, valid-record skipping, concurrency limiting, failure accounting, and initial scalar fallback.

Verify:

```bash
go test ./internal/handler ./internal/service -run 'ProbeBackfill|TrackProbe|PlaybackInfo' -count=1
npm --prefix web run build
```

## 4. Emby Projection And Subtitle Delivery

- [x] Batch-load full probe documents for sibling media sources and map all embedded streams with original indexes and known fields: language, title/display title, default/forced/external flags, profile, HDR/color facts, pixel format/bit depth, frame rate, bitrate, channels, and sample rate.
- [x] Merge deterministically indexed live sidecar subtitles without persisting paths.
- [x] Add token-aware uppercase/lowercase subtitle delivery routes.
- [x] Serve embedded subtitles by validated numeric index and sidecars by rediscovery through the existing safe conversion path.
- [x] Attach controlled route authentication to Delivery URLs and test that stored JSON contains no route token, source signed URL, header, cookie, Authorization, or backing path; API payloads must not expose backing paths or source credentials.

Verify:

```bash
go test ./internal/handler ./internal/service -run 'MediaStreams|Subtitle|PlaybackInfo' -count=1
```

## 5. Playback Selection And HLS Jobs

- [x] Parse GET/POST playback selection without losing zero or `-1`; retain the old service wrapper for internal callers.
- [x] Propagate selected audio/subtitle indexes from PlaybackInfo response URLs into Emby HLS handlers and the internal `/api/hls` handlers.
- [x] Validate absolute stream indexes and choose default audio when omitted.
- [x] Pass validated audio index into ffmpeg argument construction.
- [x] Introduce the deterministic media/audio transcode key and use it consistently for job registry, directories, playlists, segments, touch/wait/stop, active-status, hub publish payloads, and cleanup.
- [x] Restrict segment query propagation to required authentication/selection fields.
- [x] Add concurrent different-audio tests proving isolated args, jobs, directories, and playlists.

Verify:

```bash
go test ./internal/handler ./internal/service -run 'PlaybackInfo|HLS|Transcod|FFmpegArgs' -count=1
```

## 6. Integration Review

- [ ] Run the real sample through ffprobe and compare persisted/mapped output: one HEVC Main 10 HDR 3840x2160 25fps video plus three audio streams. **未验证：当前工作区未找到该真实样本文件。**
- [x] Verify local file, local STRM, cloud media, item detail and PlaybackInfo lazy repair, multi-version item, embedded subtitle, live sidecar addition/removal, Direct Play, internal HLS routes, and two simultaneous HLS audio selections.
- [x] Independently review schema/data flow/security boundaries and confirm unrelated dirty cache changes remain intact.
- [x] Update `.trellis/spec/backend/shared-media-metadata.md` with the final executable schema/API/playback contracts.

Quality gate:

```bash
gofmt -w <changed-go-files>
go test ./... -count=1
go vet ./...
npm --prefix web run build
git diff --check
```

PostgreSQL schema/round-trip uses `MEDIASTATION_TEST_POSTGRES_DSN` when supplied; this run performed static schema verification because no DSN was configured.

## Rollback

- Revert consumer wiring first; scalar metadata remains available.
- Revert additive model/repository code last. Leave the unused table in place rather than dropping user data automatically.
