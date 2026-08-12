# Emby Client Acceptance

Status: accepted. The user reported a pass on 2026-08-11 for Yamby, an official
Emby client, and SenPlayer, then explicitly waived the detailed evidence
requirement on 2026-08-12.

## Required Environment Record

For each client, record the exact stable version/build, OS/device, server
revision, timestamp, and the shared synthetic catalog used for the run. Keep
only query-stripped method, normalized path, status, and privacy-reviewed
screenshots. Do not store tokens, credentials, headers, signed URLs, or query
values.

## Result Matrix

| Step | Yamby | Official Emby client | SenPlayer |
| --- | --- | --- | --- |
| Login and server/user/library discovery | User-confirmed pass | User-confirmed pass | User-confirmed pass |
| Browse Movie and Series -> Season -> Episode | User-confirmed pass | User-confirmed pass | User-confirmed pass |
| Display metadata, people, artwork, versions, and tracks | User-confirmed pass | User-confirmed pass | User-confirmed pass |
| PlaybackInfo source/audio/subtitle selection | User-confirmed pass | User-confirmed pass | User-confirmed pass |
| Original playback, HEAD, Range, seek, HTTP/STRM redirect | User-confirmed pass | User-confirmed pass | User-confirmed pass |
| Pause, resume, stop, played state, and progress/session state | User-confirmed pass | User-confirmed pass | User-confirmed pass |
| Unsupported source offers no HLS or conversion fallback | User-confirmed pass | User-confirmed pass | User-confirmed pass |

The user explicitly accepted this evidence level for task completion on
2026-08-12. Add a sanitized protocol fixture only when a real client reproduces
a shared-contract incompatibility.
