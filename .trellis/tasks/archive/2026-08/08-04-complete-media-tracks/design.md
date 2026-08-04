# 完整媒体轨道探测与播放选择设计

## 1. Architecture

```text
local / local STRM / cloud URL
            |
            v
FFprobeService -> typed ProbeResult (safe allowlist)
            |
            v
transaction: media scalar projection + media_probe_metadata JSON
            |
            +-> MediaView/list: existing scalar fields only
            |
            +-> Item/PlaybackInfo: full MediaStreams + live sidecar subtitles
                                      |
                                      +-> Direct Play: original container
                                      +-> HLS: validated audio index + isolated job
                                      +-> Subtitle DeliveryUrl: embedded extraction/live sidecar
```

The complete probe document has one decoder/normalizer owner in the ffprobe service. Scanner, manual reprobe, lazy repair, Emby mapping, and transcoding consume the typed result; none parse stored JSON fields ad hoc.

## 2. Database Contract

Add one model with an explicit table name:

```text
media_probe_metadata
  media_id       varchar(36) primary key references media(id) on delete cascade
  probe_json     text not null
  schema_version integer not null
  probed_at      timestamp not null
```

- `media_id` is both primary key and foreign key; there is no surrogate ID and no reverse `probe_info_id` on `media`.
- Register the model in `model.AllModels()` so AutoMigrate and the existing SQLite-to-PostgreSQL model copier discover it.
- Do not preload or join this table into `Media`/`MediaView` list queries. Detail and PlaybackInfo batch-load by concrete media IDs.
- `probe_json` is portable TEXT for SQLite/PostgreSQL. The application owns validation and decoding; database JSON queries/indexes are out of scope.
- Existing `media.duration_sec`, `width`, `height`, `video_codec`, `audio_codec`, and `container` remain indexed/list-friendly compatibility projections.

## 3. Probe Document

Extend the typed probe result with a versioned document containing:

- Format: names, duration/start time, size, aggregate bitrate, probe score, safe tags.
- Streams in original ffprobe order: absolute index, type, codec/name/profile/level, duration/time base/bitrate, video dimensions/frame rates/aspect/pixel format/bit depth/color/HDR side data, audio channels/layout/sample rate, subtitle codec, safe tags, and disposition flags.
- Chapters: ID, time base, start/end, and safe title tags.

Only declared fields are unmarshaled and persisted. Do not retain raw ffprobe JSON. Exclude input filename/URL, request headers, cookies, authorization, signed query strings, attachments, and arbitrary unbounded metadata. Safe tags use a small allowlist such as language, title, handler name, encoder, and creation time.

`ProbeResult` continues exposing the current scalar summary so existing callers remain compatible. JSON parsing derives the summary from the typed stream list. The ffmpeg text fallback may refresh scalar projections but must not replace a valid complete document with partial data.

## 4. Atomic Persistence

Create one shared service/repository path used by scanner, manual reprobe, and lazy repair:

1. Resolve and snapshot the stable source identity: local path, current local STRM target, or current cloud reference.
2. Probe outside the database transaction.
3. Begin a short transaction and reload the media row.
4. Reject the result if the stable source identity changed.
5. Update scalar projection fields and upsert the complete probe row.
6. Commit, then invalidate media/Emby response caches.

A failure or partial fallback leaves the previous complete row untouched. A stale asynchronous result is discarded without mutating either storage location.

## 5. Lazy Repair And Backfill

- A record is valid when it exists and `schema_version` equals the current document version.
- Item detail and PlaybackInfo schedule asynchronous repair when a visible source record is missing/old. The initial response falls back to current scalar-generated streams and never waits for ffprobe.
- Extend lazy repair to plain local media as well as local STRM/cloud sources, while retaining media-ID in-flight deduplication and the shared FFprobe limiter.
- Do not run automatic startup/full-library backfill.
- Add an admin-only per-library background action. It processes missing/old records, reuses the shared resolver and limiter, and reports total/completed/skipped/failed metrics through `TaskTrackerService`.
- Add the action to the existing admin library menu; the global tasks view remains the progress surface.

## 6. Emby MediaStreams

- Batch-load probe documents for every concrete version before producing `MediaSources`.
- Map embedded streams with the original absolute ffprobe index. Do not synthesize fixed 0/1 indexes.
- Populate only known values: codec/type/language/title/display title/default/forced/external, dimensions, bitrate, channels, sample rate, aspect ratio, frame rate, profile, pixel format, bit depth, and video range/color facts.
- If no valid complete document exists, retain the current scalar fallback until asynchronous repair completes.
- Discover sidecar subtitles at response time, sort deterministically, assign indexes after the highest embedded index, and mark them external. Do not persist their paths in probe JSON.

## 7. Subtitle Delivery

Expose token-aware upper/lowercase Emby subtitle routes keyed by media source ID and validated stream index.

- Embedded subtitle: verify the index/type against the stored probe document, resolve the current source, and invoke ffmpeg with a numeric map generated from the validated index. Convert to WebVTT for delivery.
- Sidecar subtitle: rediscover current tracks, reconstruct the same deterministic external index mapping, and serve through the existing safe subtitle conversion path.
- Never accept an arbitrary filesystem path or ffmpeg map expression from the Emby request.
- `DeliveryMethod=External` and `DeliveryUrl` are returned for both embedded and sidecar streams. Direct Play and HLS clients overlay the subtitle; video is not burned and no HLS subtitle rendition is generated.
- Delivery URLs may carry only controlled route authentication/selection data needed to serve the subtitle. Stored probe JSON must never contain route tokens, source signed URLs, headers, cookies, authorization values, or backing filesystem paths.
- If a sidecar changes between PlaybackInfo and delivery, return not found and require a fresh PlaybackInfo response.

## 8. Playback Selection And HLS Isolation

- Parse POST JSON body and GET query into one internal selection type with pointer indexes so absent, zero, and `-1` are distinguishable.
- Preserve the existing three-argument PlaybackInfo method as a default wrapper; add an options-aware method for handlers.
- Omitted/negative audio selection chooses the default disposition, then first audio. An explicitly missing nonnegative audio index is a bad request rather than silently playing the wrong language.
- `SubtitleStreamIndex=-1` disables subtitles. Subtitle selection does not affect video transcoding because delivery is external.
- Validate the selected audio against the stored stream list before constructing ffmpeg args. Build `-map 0:<absolute-index>?` from the validated integer only.
- Replace the HLS job/output key from `mediaID` to a deterministic key including `mediaID` and selected audio index. Emby playlist/segment routes and the internal `/api/hls` playlist, segment, stop, active-status, and cleanup operations use the same key.
- Propagate only allowlisted authentication and selection query fields to segment URLs; do not copy arbitrary raw query strings.

## 9. Compatibility And Rollback

- Existing rows require no destructive migration. Missing probe rows use the scalar fallback.
- Existing direct-play, STRM target resolution, cloud playback, version names, source paths/containers, and average bitrate remain unchanged.
- Rolling code back leaves an unused additive table; existing scalar fields continue serving old clients. No automatic table drop is required.
- PostgreSQL and SQLite schema/round-trip coverage are required; PostgreSQL integration may use an optional test DSN, with static migration verification recorded when unavailable.

## 10. Error Behavior

| Condition | Result |
| --- | --- |
| ffprobe unavailable/fails | Preserve prior full JSON and scalar fields; task/lazy attempt reports failure |
| Stored JSON invalid/unknown version | Ignore it, use scalar fallback, schedule repair |
| STRM/cloud source changed during probe | Discard result without writes |
| Explicit audio index is absent/wrong type | Reject HLS request as bad input |
| Subtitle index no longer resolves | Return not found; client refreshes PlaybackInfo |
| External subtitle conversion fails | Return delivery error without changing probe data |
