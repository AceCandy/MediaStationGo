# Improve media track detail display Design

## Research: StrmAssistant

StrmAssistant does not execute a standalone ffprobe process. Its scheduled task calls the Emby internal provider refresh, then reads BaseItem media sources and persists MediaStreams with the item repository.

Useful ideas for this project:

- isolate media probing from normal metadata fetchers, NFO savers, and image refreshes;
- persist stream facts and scalar media facts together, with source-change validation;
- handle sidecar subtitle updates as a separate, repeatable operation;
- sanitize runtime media-source identity and path fields before persistence.

Not directly reusable:

- the Emby internal provider and repository interfaces are process-local and version-coupled;
- the plugin JSON wrapper is not this project's ProbeDocument contract;
- the plugin does not provide a public REST endpoint that returns richer probe data.

## Research: Emby ToolKit

Emby ToolKit directly runs ffprobe for local files and resolved 115 download URLs, then converts raw format, streams, chapters, dispositions, tags, color metadata, and side data into an Emby-compatible MediaSourceInfo document. A separate Emby plugin persists that document through an authenticated item-scoped endpoint.

Useful mapping behavior to adopt:

- derive 10-bit and 12-bit depth from pixel format when ffprobe omits explicit bit depth;
- derive HDR10, HDR10+, Dolby Vision, and resolution labels from color and side-data facts;
- normalize language, codec, channel layout, profile, subtitle text/binary status, and display titles;
- preserve absolute embedded stream indexes and move conflicting live sidecar indexes after them;
- keep raw probe facts separate from formatted Emby output.

Behavior to defer or reject for this task:

- remote 115 URL resolution, ISO probing, SHA1 shared caches, and bridge-plugin writeback are outside this display task;
- native 115 is a strong future candidate for shared probe caching because its listing exposes a stable whole-file SHA1, while generic HTTP, WebDAV, and STRM sources must not share probe data without an equally trustworthy content identity;
- smart default-track rewriting is a playback preference, not a probe fact, so persisted dispositions remain unchanged;
- subtitle language sampling and larger remote probe windows add bandwidth and latency and need a separate failing sample;
- raw ffprobe JSON must not replace this project's typed, credential-safe ProbeDocument whitelist.

## Data Flow

ProbeDocument -> shared stream projection -> Emby MediaStreams and Web detail tracks.

## Backend Contract

- ProbeDocument remains the persisted source of truth.
- Absolute ffprobe stream indexes remain unchanged.
- Compact track DTOs are attached only in MediaService.GetMedia.
- Raw probe JSON, paths, URLs, headers, credentials, and arbitrary tags are never exposed.

## Emby Mapping

- Extend the existing stream mapper.
- Derive TimeBase, Level, BitDepth, HDR fields, DisplayLanguage, DisplayTitle, audio profile, flags, Protocol, and attachment/text flags when supported by the document.
- Emit bitrate only when present; do not fabricate missing audio bitrate.

## Web Detail

- Extend the Media type with an optional compact track list.
- Render a read-only video/audio/embedded-subtitle section.
- Reuse the existing detail fetch; do not add another request.

## Compatibility And Rollback

- Playback, HLS selection, and live sidecar discovery remain unchanged.
- List/page responses remain unchanged.
- Reverting the additive DTO/UI mapping leaves persisted probe data valid.
