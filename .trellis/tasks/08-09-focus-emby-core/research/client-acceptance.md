# Emby Client Acceptance

Status: user-reported pass on 2026-08-11 for Yamby, an official Emby client,
and SenPlayer. Exact client versions, platforms, server revision, synthetic
fixture, timestamp, and sanitized method/path/status traces were not supplied,
so the evidence-complete release gate remains open.

## Required Environment Record

For each client, record the exact stable version/build, OS/device, server
revision, timestamp, and the shared synthetic catalog used for the run. Keep
only query-stripped method, normalized path, status, and privacy-reviewed
screenshots. Do not store tokens, credentials, headers, signed URLs, or query
values.

## Result Matrix

| Step | Yamby | Official Emby client | SenPlayer |
| --- | --- | --- | --- |
| Login and server/user/library discovery | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |
| Browse Movie and Series -> Season -> Episode | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |
| Display metadata, people, artwork, versions, and tracks | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |
| PlaybackInfo source/audio/subtitle selection | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |
| Original playback, HEAD, Range, seek, HTTP/STRM redirect | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |
| Pause, resume, stop, played state, and progress/session state | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |
| Unsupported source offers no HLS or conversion fallback | Reported pass; evidence pending | Reported pass; evidence pending | Reported pass; evidence pending |

All cells must pass before release. A skipped or conversion-fallback result is
a failure, not a partial pass. Add a sanitized protocol fixture only when a
real client reproduces a shared-contract incompatibility.
