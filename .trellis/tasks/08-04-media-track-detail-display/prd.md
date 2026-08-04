# Improve media track detail display

## Goal

Expose the complete media track facts already stored in media_probe_metadata in the places users inspect media details, while keeping paginated library/list responses lightweight.

## Background

- The corrected sample already contains the real MKV facts: Matroska, 29.56 GB, 3840x2160 HEVC Main 10 HDR, and three titled audio streams.
- Emby detail and PlaybackInfo load probe documents but omit several useful derived fields.
- The Web detail page calls /api/media/:id and currently exposes only scalar media facts.
- Paginated list responses must remain lightweight and must not load complete probe documents.

## Requirements

- Improve Emby MediaStreams mapping from the existing ProbeDocument source of truth.
- Add a compact single-media track view to /api/media/:id for video, audio, and embedded subtitle display.
- Reuse existing probe loading and mapping; do not add storage or schema.
- Keep paginated library/list responses unchanged.
- Keep sidecar subtitle discovery live and non-persistent.

## Acceptance Criteria

- [x] Emby streams expose HDR/language display, profile/level/time-base when available, flags, channel facts, and absolute stream indexes.
- [x] /api/media/:id returns compact track details only for single-media detail.
- [x] Web detail displays video, audio, and embedded subtitle tracks when present.
- [x] List responses do not load probe documents or add track fields.
- [x] Playback selection and subtitle delivery remain unchanged.
- [x] Focused backend and frontend checks pass.

## Out Of Scope

- Changing ffprobe collection arguments.
- Estimating missing per-stream audio bitrates.
- Persisting sidecar subtitle scans.
- Byte-for-byte Emby parity for every optional field.
- New configuration or schema migrations.

## Open Questions

None.
