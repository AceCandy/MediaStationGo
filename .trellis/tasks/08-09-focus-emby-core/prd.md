# Focus Product on Emby Core

## Goal

Make MediaStationGo a focused Emby-compatible media server for local and
protocol-neutral media sources. A real Emby-protocol client must be able to
authenticate, browse complete canonical metadata, inspect playable media
information, play a selected source and track, and report playback state.

Retire provider-specific cloud-drive management so cloud browsing, mounting,
synchronization, and transfer concerns no longer expand or constrain the core
media and Emby API architecture.

Retire PT/BT tracker configuration and torrent-resource search so tracker
adapters, credentials, anti-bot infrastructure, and tracker-facing product
surfaces no longer sit outside the focused Emby media-server boundary.

## Background

- The completed `08-08-persist-tmdb-discover-catalog` task already builds the
  canonical Movie and `Series -> Season -> Episode` metadata, credits, artwork,
  and provider identifiers needed by Emby responses. Its implementation remains
  independent and must not be folded into this task's cloud-removal diff.
- The repository already exposes Emby-compatible authentication, discovery,
  library and item browsing, people and image routes, media sources and
  streams, playback routes, sessions, and playback-state endpoints.
- Provider-specific cloud playback currently intersects the Emby stream path.
  Generic HTTP/STRM redirect playback therefore needs to be separated from
  cloud provider configuration rather than deleted with it.

## Confirmed Product Decisions

- Remove cloud-drive product capabilities using scope A selected by the user.
- Use a destructive retirement migration rather than retaining inert cloud
  compatibility data. The user confirms that no cloud provider is currently
  configured and no cloud content needs preservation.
- Preserve protocol-neutral HTTP/STRM/302 media-source playback for Emby
  clients.
- Preserve generic HTTP redirect resolution and a one-hour resolved-target
  cache as playback infrastructure rather than as a cloud-provider capability.
  An operator setting named `playback.redirect_resolve_prefixes`, containing
  one HTTP/HTTPS URL prefix per line, selects which playback URLs require
  resolution after path mapping has produced the final URL.
- Local libraries, scanning, metadata scraping, artwork, media probing,
  playback, users, sessions, and progress remain core capabilities.
- Reuse the existing canonical metadata, media, artwork, people, media-probe,
  and playback-state models. Do not introduce a parallel Emby-only media model.
- Treat Yamby, official Emby clients, and SenPlayer as the release-blocking
  acceptance clients for the first complete release.
- Limit the first release to video libraries: Movie and episodic Series content,
  with TV, Anime, and Variety represented through the canonical
  `Series -> Season -> Episode` hierarchy.
- Remove all server-side transcoding and HLS generation. Retain source-byte
  direct playback and protocol-neutral HTTP/STRM redirects only.
- Retain FFprobe-based media inspection and persisted media-stream metadata.
  The server must not start an FFmpeg process for transcoding or probe fallback.
- Keep those clients on one shared Emby protocol path. Add client-specific
  aliases or payload behavior only when a captured real-client request proves
  that the shared contract cannot satisfy it.
- Prefer deletion and direct reuse over compatibility wrappers or new generic
  abstractions that have no second implementation.
- Remove PT/BT site management, connection testing, resource/user-data lookup,
  and cross-site torrent search rather than hiding those capabilities behind
  navigation or feature flags.
- Permanently drop the `sites` table and all stored PT credentials and
  configuration. Rollback does not restore this data and requires a database
  backup when recovery is needed.

## Requirements

### Retire Cloud-Drive Capabilities

- Remove Web UI, API routes, services, scheduler jobs, settings, and navigation
  dedicated to cloud provider configuration, browsing, mounting, importing,
  synchronization, and local-to-cloud transfer.
- Remove provider-specific support for Alist/OpenList, WebDAV, S3-backed
  storage, Cloud115, and CloudDrive2 where it exists solely for cloud
  management or direct-link resolution.
- Remove the `cloud_sync` and `cloud_upload` scheduled jobs and their operator
  settings without changing unrelated local maintenance jobs.
- Prevent creation of new `cloud://` libraries, roots, or provider-backed media
  through public or admin APIs.
- Remove the provider configuration persistence model and table, cloud-only
  settings, and other provider-only schema after deleting any unexpected
  provider configuration rows.
- Delete any unexpected `cloud://` media rows, library roots, and libraries in
  dependency order. Do not delete shared canonical metadata merely because a
  removed cloud media row referenced it.
- The destructive migration must match provider-specific rows explicitly; it
  must never delete a local library, local media row, or plain HTTP/STRM media
  source.
- Remove cloud-specific labels and claims from the maintained Web UI and
  primary project documentation.
- Keep provider retirement independent from canonical TMDb metadata hydration.

### Retire PT/BT Site and Torrent-Search Capabilities

- Remove the maintained Web pages, navigation, API clients, HTTP routes,
  handlers, services, repositories, models, adapters, helpers, tests, and
  documentation dedicated to PT/BT tracker configuration and torrent search.
- Remove support for NexusPHP, Gazelle, UNIT3D, M-Team, YemaPT, Discuz, and
  custom tracker RSS adapters, including connection tests, tracker resource and
  user-data endpoints, browser emulation, tracker authentication, and
  tracker-specific rate limiting.
- Remove the `can_manage_sites` permission from backend and Web contracts.
- Remove FlareSolverr configuration and helpers because the current runtime has
  no non-tracker consumer.
- Remove `Site` from automatic schema creation and add an idempotent destructive
  migration that drops the `sites` table and the `can_manage_sites` permission
  column after runtime consumers have been removed.
- Preserve local-media search, TMDb/Bangumi/TheTVDB/Douban/AI search and
  discovery, manual metadata matching, and Emby SearchHints behavior.
- Preserve Mgo device/account-cleanup behavior and protocol-neutral local,
  HTTP/HTTPS, and STRM playback; they do not depend on tracker sites.

### Preserve Generic Media-Source Playback

- Preserve playback of local files.
- Preserve plain HTTP/HTTPS and STRM targets as protocol-neutral media sources.
- Preserve HTTP redirect playback, temporary playback authorization, HEAD and
  Range behavior required by external Emby clients.
- Stream local source bytes without remuxing, re-encoding, scaling, bitrate
  adaptation, audio conversion, or subtitle burn-in.
- Remove HLS playlists and segments, transcoder services and jobs, transcode
  cache management, FFmpeg encoder settings, and Web controls that imply the
  server can convert media.
- Keep FFprobe configuration, bounded probe execution, complete video/audio/
  subtitle/chapter documents, and media-probe backfill.
- Generic HTTP/STRM playback must not require a cloud provider account,
  provider type, cloud browser, cloud mount, or cloud scheduler.
- Apply configurable redirect-resolution matching to the URL after
  `playback.path_mappings` has been applied; apply the same matching contract
  to persisted HTTP/HTTPS STRM URLs.
- Trim each configured line, ignore empty or non-HTTP/HTTPS entries, and match
  valid entries as case-sensitive literal URL prefixes. For example, the prefix
  `http://media-gateway.example/d` matches either an STRM URL beginning with
  that value or a URL beginning with that value after
  `playback.path_mappings` rewrites a local path.
- For a matching URL, disable automatic redirect following, read only the first
  3xx response `Location`, resolve a relative `Location` against the requested
  URL, and accept only an absolute HTTP/HTTPS target. Do not request later
  redirect hops server-side.
- Cache a successful resolved target for one hour from resolution time. The
  cache identity is the actual HTTP/HTTPS URL after path mapping plus the exact
  player `User-Agent`; the process-local cache is cleared by service restart.
  Return the cached or newly resolved target in the player's HTTP 302
  `Location` header.
- For a non-matching URL, preserve the existing behavior and return the source
  URL to the player with HTTP 302 without pre-resolution.
- If resolution fails, times out, or returns no valid HTTP/HTTPS redirect
  target, including a non-3xx response, do not cache the failure; return the
  original source URL in an HTTP 302 `Location` header so the player can attempt
  it directly.
- Redirect-resolution logs must not expose query values, signed URLs, tokens,
  or credentials.
- Do not preserve cloud-provider-specific headers, API clients, direct-link
  resolution, or proxy behavior unless a confirmed generic playback contract
  requires the same behavior without provider knowledge.

### Complete the Emby Client Contract

- Cover only the Emby video domain in this phase: Movie, Series, Season, and
  Episode. Music, Audio, Album, Artist, Photo, Book, Live TV, Channel, Program,
  and recording workflows are not release-blocking and need no new endpoints.
- Authenticate an account and return stable server, user, and library identity.
- Return physical libraries and visible Movie, Series, Season, and Episode
  hierarchies backed by canonical metadata IDs.
- Return entity-owned titles, overviews, dates, ratings, genres, countries,
  languages, provider IDs, people, credits, and selected artwork without
  inventing ancestor fallback for Season or Episode metadata.
- Return media versions and `MediaSources` with accurate container, size,
  duration, bitrate, resolution, codecs, and direct-play capabilities.
- Return probed video, audio, and subtitle `MediaStreams`, including stable
  stream indexes, language, title, default/forced flags, and external subtitle
  delivery where applicable.
- Keep embedded subtitle streams selectable inside the unchanged original
  media, but do not extract or convert them server-side with FFmpeg. Continue
  controlled delivery of supported external sidecar subtitles without FFmpeg.
- Support source and track selection through PlaybackInfo and preserve the
  selected media ID through the stream request.
- Advertise direct playback only: `SupportsTranscoding` must be false and no
  transcoding URL may be returned. `SupportsDirectStream` may be true only when
  it means serving unchanged source bytes rather than remuxing.
- Do not advertise conversion indirectly through user policy, server
  configuration, device profiles, or non-empty transcoding profiles. Protocol
  fields retained for Emby JSON compatibility must remain false, empty, or
  omitted as appropriate.
- Support direct playback with correct HEAD, Range, content headers, seeking,
  and error behavior. If a client cannot decode the source container or codec,
  fail clearly without silently routing to HLS or FFmpeg.
- Record playback start, progress, stop, resume state, completion state, and
  client/device identity through the Emby session endpoints.
- Validate the contract through real-client request flows in addition to
  focused handler and service tests.
- Validate the same login, discovery, browsing, metadata, PlaybackInfo,
  playback, seeking, track selection, and progress-reporting workflow with
  Yamby, an official Emby client, and SenPlayer.
- Use the current stable build available when acceptance is executed and record
  the exact client version, platform, server revision, test fixture, timestamp,
  per-step result, and sanitized evidence. Every workflow step must pass in
  every release-blocking client.

### Compatibility and Safety

- Preserve local media, metadata, artwork, favorites, playlists, users,
  playback state, and stable Emby item identity during cloud and PT retirement.
- No rollback compatibility path is required for removed cloud provider or PT
  tracker data. Rollback is code/schema rollback only and does not promise
  restoration of deleted cloud or tracker configuration rows.
- Do not silently reinterpret an existing provider-specific cloud reference as
  a generic HTTP/STRM target.
- Do not log credentials, provider tokens, signed URLs, playback tokens, or
  request query strings during removal or compatibility testing.
- Keep the removal independently reviewable and reversible from the Emby
  compatibility work.

## Out of Scope

- Adding new cloud providers or retaining cloud features behind hidden flags.
- Adding new tracker adapters or retaining PT/BT management or search behind
  hidden flags or compatibility routes.
- Synchronizing, transferring, uploading, or managing remote cloud files.
- Building a second metadata database or an Emby-specific persistence layer.
- Claiming compatibility with every Emby ecosystem client without an explicit
  acceptance-client matrix.
- Making Infuse, VidHub, Fileball, or other third-party clients release-blocking
  in this phase. Existing compatibility should not be intentionally broken,
  but failures limited to those clients do not block the first release.
- Removing or expanding existing non-video Emby endpoints solely because
  Music, Photo, Book, Live TV, Channel, Program, and DVR are outside the
  release-blocking video matrix. Their current behavior is frozen unless a
  shared compile-time dependency must be removed.
- Adding metadata refresh behavior already owned by the completed TMDb catalog
  task.
- Server-side video/audio transcoding, remuxing, adaptive bitrate ladders,
  resolution scaling, codec conversion, subtitle burn-in, HLS playlists, and
  HLS segments.
- Music libraries and audio-specific Artist/Album metadata or playback.
- Photo, Book, Live TV, Channel, Program, tuner, guide, recording, and DVR
  domains.
- Following an upstream redirect chain beyond its first `Location`, persisting
  redirect targets across service restarts, distributing the cache, or making
  the one-hour TTL configurable.
- Removing or changing ordinary media-library search, metadata-provider search,
  AI search, Emby search, Mgo device maintenance, or HTTP/HTTPS/STRM playback as
  part of PT/BT retirement.

## Acceptance Criteria

- [x] The maintained Web UI contains no cloud provider configuration, browser,
      mount, sync, upload, or transfer workflow.
- [x] Public and admin route inventories expose no provider-specific cloud
      management, sync, upload, mount, or playback endpoint.
- [x] The maintained Web UI and route inventory expose no PT/BT site add,
      update, delete, connection-test, resource, user-data, or torrent-search
      workflow, including `/sites`, `/sites/*`, and `/search/sites`.
- [x] Tracker models, repositories, services, adapters, helpers, FlareSolverr
      configuration, `can_manage_sites`, and tracker-specific documentation are
      absent, while ordinary media/TMDb/AI/Emby search and HTTP/STRM playback
      remain available.
- [x] The idempotent retirement migration drops `sites` and
      `user_permissions.can_manage_sites`; fresh schema creation does not
      recreate either, and unrelated user permissions and data survive.
- [x] The scheduler contains no `cloud_sync` or `cloud_upload` job and retains
      local scan, organize, and recycle cleanup behavior; `transcode_cleanup`
      and HLS cache cleanup no longer exist.
- [x] A local media library can be scanned and exposed through the Emby library,
      item, hierarchy, image, people, and PlaybackInfo endpoints.
- [x] A plain HTTP/HTTPS or STRM media target remains playable without any cloud
      provider configuration or provider-specific service.
- [x] A configured matching URL is evaluated after path mapping, resolved to
      the first 3xx `Location` without following it, cached for one hour per
      post-mapping URL and exact player `User-Agent`, and returned in the
      player's HTTP 302 `Location`; a non-matching URL is returned unchanged,
      with no cloud-provider configuration required.
- [x] A redirect-resolution timeout, request failure, missing redirect, or
      invalid target is not cached and falls back to returning the original URL
      in HTTP 302 instead of failing the playback request.
- [x] No transcoder service, HLS route, transcode scheduler job, transcode cache,
      or encoder setting remains. Boot, probe, PlaybackInfo, original playback,
      and subtitle requests never start an FFmpeg process; FFprobe media
      inspection and persisted track metadata still work. A distribution
      package may contain an unused FFmpeg binary only when it supplies the
      retained FFprobe executable.
- [x] Emby payloads expose canonical metadata and complete probed media-source
      and stream information for the selected physical media version.
- [x] PlaybackInfo advertises no transcoding support or URL, and unsupported
      source codecs fail without invoking server-side conversion. User policy,
      server configuration, and device profiles do not advertise transcoding,
      remuxing, media conversion, or HLS support.
- [ ] An accepted real client can log in, browse a Movie and a complete Series
      hierarchy, display metadata and artwork, start playback, select tracks,
      seek, resume, and report completion.
- [ ] The complete acceptance workflow passes independently in Yamby, an
      official Emby client, and SenPlayer; request/response fixtures cover any
      client-specific protocol difference found during real-client validation,
      and the recorded evidence identifies the exact client/server environment.
- [x] Local media and user state survive the change; cloud data handling follows
      the approved destructive retirement policy, and no provider-specific
      configuration table, setting, library root, or media row remains.
- [x] Focused backend and Web tests, route-inventory checks, and `git diff
      --check` pass. Full compilation or full-suite execution remains subject
      to explicit user approval under the project Java/build policy when
      applicable.
