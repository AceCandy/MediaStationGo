# Focus Product on Emby Core - Implementation Plan

## Success Criteria

The task is complete only when provider-specific cloud, PT/BT tracker, torrent
search, and server-side conversion surfaces are absent, protocol-neutral direct
playback and ordinary metadata/media search still work, FFprobe inspection
still works, the Emby video contract exposes complete canonical metadata and
media facts, and the same release-blocking workflow passes in Yamby, an
official Emby client, and SenPlayer.

## Ordered Checklist

### 1. Lock the Retirement Contracts

- [x] Add route-inventory tests that fail while provider cloud, PT/BT site,
      tracker search, HLS, transcode status, or transcode cleanup endpoints from
      design section 2.5 remain; disabled/busy handlers still count as present.
- [x] Add PlaybackInfo/policy/profile tests requiring direct URLs and complete
      media facts while requiring `SupportsTranscoding=false`, no
      `TranscodingUrl`, false conversion/remux policy, and no transcoding
      profiles.
- [x] Add FFprobe tests requiring probe failure to return without starting an
      FFmpeg fallback.
- [x] Put a fake `ffmpeg` sentinel first on test `PATH`; boot the service and
      exercise probe, PlaybackInfo, original playback, and embedded/external
      subtitle requests, asserting that the sentinel is never executed.
- [x] Add direct-source regressions for local bytes, HTTP/HTTPS, STRM, 302 token
      propagation, GET/HEAD, Range/206, and invalid Range behavior. Cover
      literal prefix matching for both persisted STRM and post-mapping URLs,
      unmatched passthrough, successful resolution, relative `Location`, cache
      hit, one-hour expiry, exact `User-Agent` isolation, post-mapping cache
      identity, first-hop-only behavior, and failure fallback. Cover trimmed and
      empty setting lines, ignored non-HTTP/HTTPS entries, case-sensitive
      matching, and an empty setting.
- [x] Assert the player response is HTTP 302 with the resolved or cached target
      in `Location` on success and the original target on an unmatched prefix,
      upstream non-3xx response, missing `Location`, invalid `Location`, timeout,
      or request error; failure cases must not populate the cache.
- [x] Use an upstream request spy and captured test logger to prove the resolver
      forwards only the fixed Range and player `User-Agent` headers, never
      forwards Authorization, Cookie, or stored headers, and never logs query
      values, tokens, or complete signed targets on success or failure.
- [x] Record a baseline inventory of `cloud`, provider, HLS, transcoder, and
      FFmpeg references, excluding `docs/cankao`, generated output, dependencies,
      and intentionally retained FFprobe/package references. Store the reviewed
      allowlist and actionable baseline in task research, not raw secret-bearing
      logs.

Verify: the new tests fail for the intended old behavior and do not redefine
canonical metadata ownership.

### 2. Implement the Destructive Retirement Migrations

- [x] Reject new `cloud://` libraries, roots, and media at public/admin entry
      boundaries before deleting provider services.
- [x] Add idempotent PostgreSQL migration logic for the exact table, predicate,
      and ordering contract in design section 3.2: provider-protocol
      `strm_records`, selected `media_probe_metadata`, `media`, cloud
      `library_roots`, only empty cloud `libraries`, cloud/transcode settings,
      then `storage_configs`.
- [x] Remove `StorageConfig` from AutoMigrate only after the destructive
      migration runs.
- [x] Add migration tests proving local, HTTP/HTTPS, STRM, shared metadata,
      artwork, people, user state, playlists, and playback history survive;
      prove media-owned probe rows do not become orphaned.
- [x] Cover active and soft-deleted rows, a pure cloud library, a mixed
      cloud/local-root library, provider-protocol versus HTTP/HTTPS STRM rows,
      absent legacy tables, repeated migration, and an injected mid-transaction
      failure that leaves every table unchanged.
- [x] Assert case/whitespace-normalized `cloud://` matching, stable mixed-root
      selection by `sort_order`, `created_at`, and `id`, and rollback when a
      candidate retains media without a usable non-cloud root.
- [x] Remove `Site` from AutoMigrate and add idempotent PostgreSQL DDL that drops
      `sites` and only `user_permissions.can_manage_sites` after all runtime
      consumers have been removed.
- [x] Add migration coverage for populated and absent `sites`, stored tracker
      credentials, repeated execution, preservation of unrelated permissions,
      and fresh schema creation that does not recreate retired schema.

Verify: repeat the migration against isolated databases and query physical as
well as soft-deleted rows. Rollback point: revert before running the migration;
after execution, restoration requires a database backup.

Use `MEDIASTATION_TEST_POSTGRES_DSN` with an isolated schema. Also verify the
first runtime query after the migration connection is closed and the prepared
runtime connection is opened.

### 3. Retire Cloud/PT Backend and Preserve Generic Search and Playback

- [x] Remove provider clients, storage configuration repository/service wiring,
      cloud boot/health/scan/upload/mount/metadata/image/subtitle logic, and
      cloud-only tests.
- [x] Remove admin/public cloud routes and cloud scheduler jobs.
- [x] Remove PT/BT models, repositories, service-container wiring, handlers,
      routes, helpers, connection tests, tracker adapters, rate limiting,
      FlareSolverr configuration, and their focused tests.
- [x] Remove `can_manage_sites` from backend permission models and serialized
      permission maps without changing unrelated permissions.
- [x] Remove cloud visibility merging and provider path handling from shared
      playback/library code without changing normal visibility filters; retain
      only deny-only `cloud://` admission guards required by section 2.
- [x] Move only required protocol-neutral HTTP/STRM redirect resolution into
      the existing playback/stream boundary; preserve authorization, GET/HEAD,
      Range, content headers, seeking, and the exact internal/external 302 token
      rules from design section 2.2.
- [x] Add `playback.redirect_resolve_prefixes` as a newline-delimited literal
      prefix setting. Match after `playback.path_mappings` or directly against a
      persisted STRM URL; leave unmatched URLs unchanged.
- [x] Restore the bounded `Range: bytes=0-0` redirect probe and one-hour
      in-memory success cache keyed by source URL and player `User-Agent`.
      Resolve relative HTTP/HTTPS locations, disable automatic redirect
      following, consume only the first 3xx `Location`, and fall back to the
      original URL without caching failures. Use the actual post-mapping URL and
      exact `User-Agent` as the key; restart clears the cache.
- [x] Keep player-facing redirect responses `no-store` and keep signed URL,
      token, credential, and query values out of logs.
- [x] Remove provider protocols from STRM creation/validation while preserving
      local `.strm` plus plain HTTP/HTTPS targets and admin-owned path mappings.
- [x] Keep local scanners, metadata/artwork persistence, FFprobe backfill,
      organizer/recycle jobs, and generic STRM behavior intact.

Verify: focused handler/service/repository tests and route inventory pass; a
plain HTTP/STRM source plays with no provider configuration.

### 4. Retire Transcoding and FFmpeg Invocation Paths

- [x] Remove `TranscoderService`, HLS service/handlers/routes, transcode jobs,
      active-job/status APIs, HLS cache cleanup, and related models/tests.
- [x] Remove FFmpeg encoder discovery, auto-install, status/security endpoints,
      settings, defaults, and runtime updates. Retain only FFprobe discovery,
      configuration, bounded execution, and security needed by FFprobe.
- [x] Delete FFprobe's local and HTTP `ffmpeg -i` fallback and its parser/tests;
      preserve prior valid probe documents on failure.
- [x] Remove transcode/HLS service lifecycle and dependency-container wiring.
- [x] Audit process-launch sites to prove no application path can start
      `ffmpeg`; tolerate a distribution package name only when it is required
      to supply `ffprobe` and no executable path is used by the application.
- [x] Keep embedded subtitle facts in `MediaStreams` but remove their extraction
      `DeliveryUrl` and FFmpeg extraction path. Keep external sidecar discovery,
      stable indexes, token-aware delivery, and existing in-process text
      conversion.

Verify: FFprobe success/failure/backfill tests pass; static inventory finds no
FFmpeg process invocation or HLS/transcoder runtime path, and the sentinel plus
container smoke check observes no `ffmpeg` process during retained workflows.

### 5. Make the Emby Contract Direct-Only and Complete

- [x] Preserve system/authentication/users/views/items/search/people/images,
      Series/Season/Episode, PlaybackInfo, original stream/subtitle, sessions,
      and playback-state routes with existing compatible prefixes/case aliases.
- [x] Preserve canonical entity ownership and complete `MediaSources` /
      `MediaStreams` projections for every visible physical media version.
- [x] Always return `SupportsTranscoding=false`; omit `TranscodingUrl`; return
      false/empty conversion, remux, and profile capabilities at their exact
      serialized layers.
- [x] Remove Emby HLS master/main/segment routes instead of leaving disabled or
      busy responses.
- [x] Preserve selected media/audio/subtitle indexes through PlaybackInfo,
      controlled subtitle delivery, direct stream resolution, and progress
      events.
- [x] Assert embedded subtitles are non-external source tracks without a server
      extraction URL, while supported external sidecars retain controlled
      delivery URLs.
- [x] Add sanitized request/response fixtures only for protocol differences
      reproduced by Yamby, official Emby, or SenPlayer. No shared-contract
      difference was reported, so no fixture was required.

Verify: focused Emby service/handler JSON and route tests cover Movie and a full
Series hierarchy, images, tracks, source selection, direct playback, seeking,
subtitle delivery, sessions, resume, and completion.

Existing non-video Emby routes are compile-checked but receive no new behavior
or release-blocking compatibility work.

### 6. Remove Cloud, PT, and Conversion Web Surfaces

- [x] Remove storage/provider pages, forms, APIs, browser/mount/import/transfer
      controls, hooks/models, settings groups, navigation entries, and cloud
      progress states.
- [x] Remove PT site management/search pages, forms, API/types, navigation,
      admin shortcuts, and `can_manage_sites` Web permission contracts.
- [x] Preserve ordinary media search, metadata-provider search, AI search,
      manual scrape search, and their existing navigation/routes.
- [x] Keep generic STRM UI only where it does not require provider settings.
- [x] Add a multiline general playback field for
      `playback.redirect_resolve_prefixes`, positioned with the retained path
      mappings and described as generic URL-prefix matching.
- [x] Remove HLS player mode/button/status, dynamic `hls.js` use and dependency,
      active transcode tables, and FFmpeg/transcoder settings.
- [x] Keep local library administration, metadata/media details, direct player,
      FFprobe settings/status, remaining schedules, and background task views.

Verify: Web tests/type checks/build pass using project scripts; targeted UI
search shows no removed navigation or action.

### 7. Clean Deployment and Maintained Documentation

- [x] Remove cloud/transcoder environment variables, sample configuration,
      startup comments, and operator claims from maintained docs and deployment
      files.
- [x] Remove PT/BT tracker, supported-site, site-search, FlareSolverr, and
      `can_manage_sites` claims from maintained documentation and examples.
- [x] Keep only FFprobe runtime configuration. If the container's supported
      package manager has no separate FFprobe package, document the package as
      a probe-only runtime dependency without exposing FFmpeg controls.
- [x] Remove obsolete cloud/HLS/transcode fixtures and generated lockfile
      entries caused by dependency removal.

Verify: configuration/reference inventory contains no actionable provider or
conversion setting; secrets and signed URLs are absent from the diff.

### 8. Full Verification and Independent Review

- [x] Run focused backend tests for database, repository, service, and handler
      packages affected by retirement and Emby compatibility.
- [x] Run the repository's existing Web test, type-check, lint, and build
      scripts that cover changed code.
- [x] Run focused migration, permission, route-inventory, service-container,
      ordinary-search, HTTP/STRM, and Mgo-device tests after PT retirement.
- [x] Run the Web type-check, lint, test, and build scripts after removing PT
      pages, APIs, types, permissions, navigation, and documentation.
- [x] Run static route/config/process inventories, including PT adapter/site
      symbols and routes, and `git diff --check`.
- [x] Independently review the diff against PRD acceptance criteria and the
      direct-only replacement for superseded HLS clauses in the shared spec.
- [x] Independently review the PT retirement diff against the destructive data
      decision and the ordinary-search/playback/Mgo preservation boundaries.
- [ ] Execute the black-box acceptance matrix separately in Yamby, an official
      Emby client, and SenPlayer using each current stable build. Record the
      exact version/platform/server revision, synthetic fixture, timestamp,
      seven-step pass/fail table, and query-stripped method/path/status trace in
      `research/client-acceptance.md`; every step must pass in every client.
- [x] Update `.trellis/spec/backend/shared-media-metadata.md` after the code and
      tests prove the new direct-only contract, removing obsolete HLS clauses
      and replacing the unconditional external-URL passthrough rule with the
      approved configurable redirect-resolution contract.

Suggested automated commands, adjusted to actual package/test scripts at
execution time:

```bash
go test ./internal/database ./internal/repository ./internal/service ./internal/handler
cd web && npm test
cd web && npm run typecheck
cd web && npm run lint
cd web && npm run build
rg -n -i 'cloud|alist|openlist|webdav|cloud115|clouddrive2|hls|transcod|ffmpeg' \
  internal cmd web/src web/package.json Dockerfile docker-compose.yml README.md README_EN.md
rg -n -i 'can_manage_sites|siteadapter|siteservice|/sites|search/sites|nexusphp|gazelle|unit3d|m-team|yemapt|discuz|flaresolverr|pt/bt' \
  internal cmd web/src README.md README_EN.md CONTRIBUTING.md SECURITY.md
git diff --check
```

The final inventory is reviewed, not required to be empty: canonical history,
explicit `cloud://` migration guards, FFprobe package provenance, and negative
tests may legitimately contain matched terms.

## Remaining Verification Gates

- The retained workflow sentinel and container smoke completed on 2026-08-11.
  The current worktree image built successfully; the isolated PostgreSQL and
  application containers became healthy, `/api/health` returned 200, the
  process list contained only `mediastation-go`, and the fake `ffmpeg` marker
  remained absent. All temporary containers, network, image, and files were
  removed after verification.
- Execute every cell in `research/client-acceptance.md` on Yamby, an official
  Emby client, and SenPlayer. Automated tests do not replace this gate.

The user reported on 2026-08-11 that the Yamby, official Emby, and SenPlayer
workflows passed. Exact client versions, platforms, server revision, synthetic
fixture, timestamp, seven-step matrix, and sanitized method/path/status trace
were not supplied, so the evidence-complete acceptance checkbox remains open.

PostgreSQL verification completed on 2026-08-11 with an isolated test schema:
the migration connection closed, the prepared runtime connection completed its
first query, and the database, repository, service, and handler packages passed.

## Risk and Review Gates

- Do not delete provider files wholesale until generic HTTP/STRM call sites are
  covered by direct-play regression tests.
- Do not drop `sites` until the user-approved destructive migration is covered
  for populated, absent, and repeated-run schemas; restoration requires a
  database backup.
- Do not remove the provider model from AutoMigrate before the destructive
  migration has executed.
- Do not claim FFmpeg retirement while a probe fallback, process launcher,
  HLS route, policy flag, or Web control remains.
- Do not treat successful automated tests as the three-client acceptance gate.
- Do not log or commit playback tokens, credentials, signed URLs, request query
  strings, local debug exports, or client captures containing private data.
- After each implementation checkpoint, inspect the current working tree and
  preserve unrelated user changes.
- The fresh-database catalog hydration warning was resolved on 2026-08-12.
  `MIN(next_attempt_at)` now scans through `sql.NullTime`, with a PostgreSQL
  regression test covering an empty table, NULL-only jobs, eligible minimum
  selection, and completed-job exclusion. A fresh PostgreSQL container smoke
  confirmed an empty queue, healthy service, and no NULL scan or catalog
  hydration error in the application log.
- Web dependency vulnerabilities were resolved on 2026-08-12 by upgrading
  Axios, React Router, Vite, the Vite React plugin, PostCSS, and compatible
  transitive dependencies. Node 22 is used for the frontend image stage.
  Clean install, `npm audit`, lint, production build, full Docker build, and a
  browser route/redirect smoke all passed with no console error.
