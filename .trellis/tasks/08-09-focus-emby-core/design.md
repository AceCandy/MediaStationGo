# Focus Product on Emby Core - Technical Design

## 1. Design Objective

Reduce MediaStationGo to four runtime responsibilities:

1. Maintain canonical video metadata and physical media versions.
2. Scan local sources, scrape entity-owned metadata/artwork, inspect media with
   FFprobe, and persist complete track facts.
3. Expose the shared Emby video contract for Movie, Series, Season, and Episode.
4. Serve unchanged local or protocol-neutral HTTP/HTTPS/STRM source bytes and
   maintain users, sessions, favorites, playlists, and playback state.

Cloud-provider management, PT/BT tracker management and search, and server-side
conversion are deleted capabilities, not disabled modes. The design
deliberately avoids replacement provider interfaces, hidden flags,
compatibility shims, and a new Emby-only data model.

## 2. Runtime Boundaries

### 2.1 Canonical Metadata and Media

`MetadataItem` remains the logical Emby item identity. `Media` remains a
concrete playable version, and `MediaProbeMetadata` remains the complete typed
track document. Shared metadata, artwork, people, user state, playlists, and
playback history must not become owned by a removable storage provider.

The existing `MediaView` and Emby projection services remain the read boundary.
Movie, Series, Season, and Episode payloads continue to use real canonical IDs;
`MediaSource.Id` continues to select one visible physical version.

### 2.2 Protocol-Neutral Source Resolution

The only supported source classes are:

- a local playable file;
- a local `.strm` sidecar whose target is an absolute supported local or
  HTTP/HTTPS URL;
- a plain HTTP/HTTPS media source.

Source resolution must not depend on `StorageConfig`, a provider type,
`cloud://`, or provider credentials. The small generic HTTP redirect behavior
currently coupled to cloud storage is moved into the existing playback/stream
boundary; no general storage-provider abstraction replaces it.

Local and reachable remote sources retain GET, HEAD, Range, content headers,
seeking, temporary authorization, and token-preserving 302 behavior. The server
does not remux or transform the response body. Provider-specific header
injection, signed-link resolution, cloud visibility merging, and cloud proxy
routes are removed.

The protocol-neutral redirect contract is exact:

- the target comes only from persisted `Media.STRMURL` or the existing
  admin-owned `playback.path_mappings`, never from a caller-supplied target;
- `playback.redirect_resolve_prefixes` stores one literal HTTP/HTTPS URL prefix
  per line; whitespace is trimmed, empty and non-HTTP/HTTPS entries are ignored,
  and case-sensitive matching is applied to the final URL after path mapping or
  directly to a persisted STRM URL;
- an external candidate must be an absolute HTTP/HTTPS URL. It is returned
  unchanged, including its existing query, when no prefix matches or redirect
  resolution fails; a successful match returns the resolved redirect target.
  Neither form receives an appended MediaStation token or `media_id`;
- a same-origin internal `/api/stream/:id` redirect may receive only the
  existing short-lived `external_play` JWT scoped to that media ID;
- no upstream Authorization, Cookie, provider credential, or arbitrary stored
  header is forwarded, and query values are never logged;
- local file responses implement Range directly; after an external 302 the
  client issues its own HEAD/Range request to the target.

For a matching URL, the playback boundary first looks up an in-memory cache by
the actual post-mapping HTTP/HTTPS URL and the exact player `User-Agent`. A miss
issues a bounded GET to that URL with `Range: bytes=0-0` and the player's
`User-Agent`, disables automatic redirect following, and accepts the first 3xx
`Location` only when it resolves to an absolute HTTP/HTTPS URL. Relative
`Location` values resolve against the requested URL. The resolver does not
request the target or traverse later redirect hops.

A successful target is cached until one hour after resolution. Expired entries
are resolved again; failures, timeouts, non-3xx responses, and missing or
invalid targets are not cached and fall back to the original URL. The cache is
process-local, is cleared by restart, and is consulted only after the current
prefix setting still matches, so removing a prefix immediately bypasses an old
entry. Persistent, distributed, provider-aware, and configurable-TTL caches are
outside this boundary.

The player receives HTTP 302 with the selected target and `Cache-Control:
no-store`; the one-hour cache is server-internal and does not ask the client to
retain a signed URL. Logs may contain only sanitized source/target hashes,
scheme, host, path, query-key names, and cache-hit state, never query values or
full signed targets. This bounded helper is not a provider client and cannot
accept a provider type, ref, credential, or header map.

### 2.3 Probe Boundary

FFprobe remains the single executable media-inspection boundary. It retains its
path setting, bounded concurrency, timeout/cancellation, typed document
validation, persisted probe metadata, and backfill workflow.

The FFprobe service must never fall back to `ffmpeg -i`. A missing executable,
timeout, invalid output, or unsupported remote source returns a diagnosable
probe error and preserves any previous valid probe document.

The container image may continue obtaining `ffprobe` from a distribution
package that also contains an `ffmpeg` binary when no separate supported
package exists. Package/binary presence is allowed; application discovery,
installation, configuration, and invocation are not. A fake `ffmpeg` sentinel
in automated tests and a process check during container smoke tests verify that
boot, probe, PlaybackInfo, original playback, and subtitle requests do not
start it.

### 2.4 Emby Video Contract

The shared route set remains available with the existing `/emby` and compatible
case/prefix variants where already supported. The contract includes:

- system identity, authentication, users, views, sessions, capabilities, and
  logout;
- Items, Latest, Resume, Counts, SearchHints, detail, people, and entity-owned
  images;
- Series, Season, and Episode hierarchy and ordering;
- PlaybackInfo GET/POST, media-version selection, audio/subtitle selection,
  controlled subtitle delivery, and token propagation;
- original video stream GET/HEAD, Range seeking, and protocol-neutral 302;
- Playing, Progress, and Stopped session updates and shared user state.

`MediaSources` retain container, size, duration, bitrate, resolution, codecs,
stream indexes, language, title, default/forced flags, and delivery URLs.
`DirectStreamUrl`, `Protocol=Http`, `SupportsDirectPlay`, and unchanged-byte
`SupportsDirectStream` remain available.

Every response path advertises direct playback only:

- `SupportsTranscoding=false`;
- `TranscodingUrl` is absent;
- server/user conversion and remux policy fields are false or omitted;
- device-profile transcoding profiles are absent or empty;
- HLS master, playlist, segment, status, stop, and cleanup routes do not exist.

Embedded subtitle tracks remain non-external `MediaStreams` with their original
absolute indexes and are selected inside the direct-played source. They do not
receive an extraction `DeliveryUrl`. Supported local external SRT/ASS/SSA/VTT
sidecars keep stable synthetic indexes and controlled token-aware delivery;
their existing in-process text conversion remains and starts no FFmpeg process.

No codec negotiation or replacement player engine is added. A client that
cannot decode the original source receives no conversion fallback and must
surface its direct-play failure. A client-specific alias or payload branch is
added only from a captured failing request/response fixture.

### 2.5 Route Inventory Contract

Route-table tests enumerate every registered method/path and require these
provider/conversion patterns to be absent, including `/emby`, root, uppercase,
and lowercase variants:

- `/admin/storage/status`, `/admin/storage/:type`, and its `test`, `logout`, and
  `upload-local` commands;
- `/admin/cloud/scan-all`, `scan/cancel`, `scan/status`, and every
  `/admin/cloud/:type/*` list/mkdir/rename/import/mount/QR command;
- `/sites`, every `/sites/*` CRUD/test/resource/user-data/search command, and
  the `/search/sites` alias;
- `/hls/:id/*`, `/cloud/play/:type`, `/img/cloud/:type`, and
  `/playback/transcode/:job_id/status`;
- `/Videos/:id/master.m3u8`, `/Videos/:id/main.m3u8`, `/Videos/:id/:seg`, and
  their lowercase equivalents.

A disabled, busy, or compatibility response still fails this contract: these
routes must not be registered. The generic scheduler trigger route remains,
but `cloud_sync`, `cloud_upload`, and `transcode_cleanup` are absent job names.

The retained route table explicitly covers `/stream/:id` GET/HEAD, Emby
PlaybackInfo GET/POST, `/Videos/:id/{stream,original}` plus container variants
GET/HEAD, controlled subtitle delivery, images GET/HEAD, and playback progress.

## 3. Retirement Design

### 3.1 Admission and Runtime Removal

Before schema retirement, public and admin library/root/media entry points must
reject new `cloud://` values. Service boot must no longer start cloud health
checks, cloud scans, cloud synchronization, uploads, or conversion cleanup.

Remove provider routes, services, clients, repositories, settings, scheduler
jobs, Web pages, navigation, task tables, and tests that exist only for:

- Alist/OpenList, WebDAV, S3-backed storage, Cloud115, or CloudDrive2;
- cloud browsing, mount, import, sync, upload, transfer, or direct-link lookup;
- PT/BT site configuration, tracker authentication, connection tests,
  resource/user-data lookup, torrent search, tracker adapters, tracker rate
  limiting, or FlareSolverr browser emulation;
- HLS, transcoder jobs/cache, FFmpeg installation/status/security, or encoder
  configuration.

Generic local scanning, metadata scraping, organizer jobs, recycle cleanup,
FFprobe backfill, and protocol-neutral STRM generation/playback remain.

Existing non-video Emby routes are neither expanded nor deliberately removed.
They are outside the release-blocking matrix and may change only where removal
of a shared cloud/transcoder dependency is necessary to keep the build coherent.

### 3.2 Data Migration

The startup migration is idempotent and explicit for the project's supported
PostgreSQL runtime. It runs through the existing migration-only connection with
prepared statements disabled and simple protocol enabled. The owning schema is:

| Table | Retirement predicate/action |
| --- | --- |
| `storage_configs` | Drop the entire provider configuration table after data cleanup. |
| `sites` | Run `DROP TABLE IF EXISTS sites CASCADE`, permanently deleting all stored tracker credentials, after removing `Site` from AutoMigrate. |
| `user_permissions` | Drop only the retired `can_manage_sites` column; preserve every unrelated permission. |
| `library_roots` | Cloud root when `LOWER(TRIM(path)) LIKE 'cloud://%'`; hard-delete by ID. |
| `media` | Cloud media when `LOWER(TRIM(path)) LIKE 'cloud://%'`, its `library_root_id` is a cloud root, or its ID is referenced by an explicitly provider-protocol `strm_records` row; include soft-deleted rows. |
| `media_probe_metadata` | Explicitly delete rows whose `media_id` is in the cloud-media ID set before media deletion; do not rely solely on FK enforcement. |
| `strm_records` | Delete rows linked to deleted media or whose normalized `protocol` is `alist`, `alists`, `openlist`, `openlists`, `webdav`, `davs`, or `s3`; preserve `http` and `https`. |
| `libraries` | Delete only a candidate library with no remaining non-cloud roots and no remaining media. For a mixed library, preserve it and reset its compatibility `path` to the first root ordered by `sort_order`, `created_at`, then `id`. |
| `settings` | Delete `cloud.%`, `app.cloud_%`, provider upload/sync keys, `transcode.%`, `transcoder.%`, `ffmpeg.path`, and `app.ffmpeg_path`; preserve `strm.enabled`, `playback.path_mappings`, `playback.redirect_resolve_prefixes`, FFprobe settings, and unrelated scheduler settings. |

The migration sequence is:

1. Run inside one transaction and detect absent legacy tables/columns as a
   successful no-op.
2. Select cloud root/media/candidate-library IDs using the predicates above.
3. Delete media probe rows and provider-only STRM records, then hard-delete the
   selected media and roots.
4. Delete only now-empty cloud libraries and repair the compatibility `path` of
   mixed libraries from the deterministically ordered surviving root. If a
   candidate retains media but has no non-cloud root, fail and roll back rather
   than inventing a path.
5. Delete the explicit setting keys/prefixes, drop `storage_configs`, drop
   `user_permissions.can_manage_sites`, and drop `sites` with idempotent DDL.
6. Remove `StorageConfig` and `Site` from AutoMigrate in the same release as the
   destructive migration so neither table is recreated on later boots.

The migration never matches an HTTP/HTTPS `strm_records.protocol`, a plain
HTTP/HTTPS `Media.STRMURL`, an ordinary local `.strm`, metadata source/name, or
provider display text. It never deletes `metadata_items`, identifiers, artwork,
people/credits, favorites, playlists, or playback histories. Their non-FK
preferred `media_id` values may reference a removed version, but logical state
continues to belong to the preserved `metadata_id`. Failure rolls back the
transaction and prevents a partially retired schema from booting.

Rollback is code/schema rollback only. Deleted provider configuration,
`cloud://` file rows, PT credentials, and tracker configuration are
intentionally not reconstructed; recovery requires a pre-migration backup.

## 4. Cross-Layer Data Flow

```text
local / HTTP / STRM source
  -> scanner and canonical Media link
  -> FFprobe typed document + scalar projection
  -> Emby item / PlaybackInfo projection
  -> direct stream, Range response, or token-preserving 302
  -> client playback events
  -> shared playback state and session projection
```

The same media ID and selected absolute stream indexes must survive each
boundary. List responses continue using scalar facts; item detail and
PlaybackInfo may load complete probe documents. No removed provider object or
transcoder job appears in this flow.

## 5. Web and Operator Surface

The maintained Web application keeps local libraries, metadata, FFprobe-backed
media details, direct playback, users, sessions, schedules unrelated to cloud
or conversion, and task visibility for remaining background work.

It removes cloud storage pages and navigation, provider forms, cloud browser,
mount/import/transfer controls, PT site management and search pages, tracker
forms and API clients, HLS player mode and button, `hls.js`, active transcode
tables, FFmpeg encoder settings, and cloud/transcode API clients.
STRM controls remain only where their output is protocol-neutral and playable
without a provider account. General playback settings retain path mappings and
add the multiline `playback.redirect_resolve_prefixes` field without any
provider label or provider-specific default.

## 6. Compatibility Validation

Focused automated checks first lock the common contract at the exact JSON
layer consumed by clients. Black-box validation then runs the same matrix for
Yamby, an official Emby client, and SenPlayer:

1. Login and server/user/library discovery.
2. Browse one Movie and one complete Series -> Season -> Episode hierarchy.
3. Display owned metadata, people, artwork, source versions, and track facts.
4. Request PlaybackInfo and select a media version, audio track, and subtitle.
5. Start original playback, exercise HEAD/Range/seek and HTTP/STRM 302.
6. Pause, resume, stop, mark played, and verify session/progress state.
7. Attempt an unsupported source and verify that no HLS/FFmpeg path is offered
   or invoked.

Captured sanitized fixtures are added only when a real client exposes a shared
contract defect. Tokens, signed URLs, headers, query strings, and credentials
must be removed from artifacts and logs.

Acceptance uses the current stable build available for each client at execution
time. The result records exact client version/build, OS/device, server revision,
timestamp, and the same synthetic test catalog: one local Movie, one complete
Series hierarchy, one plain HTTP/STRM source, multiple audio/subtitle tracks,
one external sidecar subtitle, and one intentionally unsupported source.

Each client gets a per-step pass/fail table plus a query-stripped server trace
of request method, normalized path, and status. A version-screen capture may be
kept only after privacy review. All seven steps must pass; skipped, untested, or
conversion-fallback results block release. Evidence is recorded in the task's
`research/client-acceptance.md`, with sanitized protocol fixtures added only for
reproduced incompatibilities.

## 7. Sequencing and Rollback Points

The work stays in one task because cloud removal, conversion removal, and Emby
direct playback overlap the same service wiring, route registration, playback
projection, Web player, and deployment configuration. Splitting those edits
would create intermediate builds with contradictory capabilities.

Implementation uses reviewable checkpoints:

1. Contract tests and destructive migrations.
2. Backend cloud/PT retirement with generic search and source playback
   preserved.
3. Transcoder/HLS/FFmpeg runtime retirement with FFprobe preserved.
4. Emby direct-only projection and route contract.
5. Web, deployment, and documentation cleanup.
6. Automated and three-client acceptance verification.

Each checkpoint must compile and pass its focused tests before the next one.
Before the migration is released, rollback is ordinary code rollback. After it
runs, rollback cannot restore deleted provider data and therefore requires a
database backup if restoration is desired.

## 8. Known Risks

- Provider logic and generic HTTP/STRM behavior are currently coupled; deleting
  files wholesale can remove required Range, HEAD, redirect, or token behavior.
- A cached redirect target can expire upstream before the fixed one-hour TTL;
  the server cannot observe a later client-to-target failure, so expiry waits
  for cache timeout or process restart. The fallback applies only when the
  server-side resolution request itself fails.
- Existing shared-media specs and tests still require HLS in places. This task's
  approved direct-only contract supersedes those clauses, and Phase 3 must
  update the shared spec after implementation proves the new behavior.
- Real clients may depend on additional session, subtitle, or image fields not
  evident from repository tests. The acceptance matrix, not speculative client
  branching, determines those additions.
- PostgreSQL destructive DDL and DML must remain atomic through the dedicated
  migration connection. An isolated schema test must cover migration followed
  by the first prepared-statement runtime query.
- Dropping `sites` permanently deletes stored tracker credentials and
  configuration. The migration must be idempotent, preserve unrelated
  permission columns, and be covered with absent-table and repeated-run cases.

During implementation, this PRD/design supersedes only the HLS/transcoder and
FFmpeg-invocation clauses in the existing shared-media spec. Every canonical
metadata, visibility, probing, subtitle-safety, artwork, and playback-state
clause remains binding. The shared spec is updated after the replacement
contract passes, not used to reintroduce the retired behavior.
