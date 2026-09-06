# Implementation

## Approved NFO specials and repeated profile downloads

Fix NFO coordinate merging at its source: preserve the raw season field so an
explicit zero overrides a show-level -1, while omitted/invalid/negative values
do not overwrite an existing season. The prior filename/database repair was
overwritten by a later NFO read; its fixture lacked actual conflicting sidecars.
Extend the PostgreSQL retry fixture with show -1 and episode 0, including both
negative stored coordinates and coordinates already repaired to zero.

Reuse the existing bounded in-process runtime cache for successful remote
profile URL-to-key imports (24 hours); validate local content against its key
before reuse. Preserve direct explicit refresh, local file rereads, independent
people storage and retry after failed imports. Do not increase worker counts,
change database schema, edit user sidecars or execute production jobs.

## Approved season heading and retry-entry follow-up

Compose the visible Season heading from its number/specials label plus a concrete
own title, suppressing duplicate generated names. This is presentation only.
The scan-only negative-season repair missed manual scrape retries, which reuse
stored coordinates. Repair explicit SxxExx negative coordinates at shared
enrichment and sibling binding, guarded by original path/coordinates; never
coerce unknown names. Verify library/single retries without scanning and retain
the scan regression. No production restart or task execution during tests.

## Approved invalid-season scan recovery

Preserve Season 0 in single/range episode parsing. Revisit unchanged scan rows
with negative season numbers only when their filename has explicit SxxExx
coordinates. Corrected error rows return to pending with stale errors cleared;
unrecognized names remain untouched. Test both snapshot/batch and direct scan
reads, normal unchanged files, Special ranges, and actual PostgreSQL persistence.
Do not weaken metadata validation, guess a season for every negative value, or
put file-coordinate repair in the local title/overview correction task.

## Approved visible local-correction task

1. Reuse task definitions/tracker and the existing keyset snapshot reader; expose
   manual execution and version-gated asynchronous startup. Track totals,
   processed/updated/skipped/failed and finish with failure on any item error.
2. Replace unconditional metadata Save with a field-limited atomic update guarded
   by TMDb source and the read-time updated_at. Refresh views only after writes.
3. Verify stale Web edits, unchanged rows, task version success/failure/retry,
   task definition/action and independent ingestion startup in focused tests.
   Do not deploy, restart, trigger production jobs or touch unrelated changes.

## Approved title-language correction

Restrict Season/Episode translation title candidates to Chinese and English in
the shared selector. Preserve concrete upstream titles and existing overview
fallback. Reuse startup snapshot localization for historical repair; do not
rescrape or restart services. Verify the reported Thai replacement with a
failing-then-passing parser/unit regression and real PostgreSQL backfill tests.
The user authorized fixing the reported data. The attempted guarded update of
only that Season was blocked by DBX write policy; no production rows changed.
Deployment/startup of the fixed backend remains required for historical repair.

Root cause: an unrestricted language fallback combined with a Chinese/English
placeholder recognizer treated a Thai numbered label as a concrete title.
The missing regression was cross-language placeholder fallback, not hierarchy
or frontend rendering. Record the bounded title-language contract in the shared
metadata spec so future localization changes preserve this boundary.

## Approved detail-menu cleanup

Remove organizing from movie and Episode detail menus, delete the unused
detail-only dialog, state and client wrapper. Preserve metadata refresh/edit,
probing, deletion and playback. Dedicated directory/library organizing tools
and backend routes remain intact. Verify shared and Episode admin menus via
render regressions, compile both detail callers, and review remaining references.

## Approved Series card presentation correction

- Reuse canonical Series presentation in batches for library/recent cards.
- Keep file identity, technical fields, navigation keys and LinkMedia unchanged;
  apply missing-artwork filters after projection. No rescrape or data repair.
- Root cause: correcting per-Episode attachments exposed an older assumption
  that an Episode representative could double as a Series presentation.

1. Add scoped canonical Series reads/favorites and exact library lookup; preserve visibility.
2. Implement URL selection and logical episode grouping while retaining all administrative files.
3. Reuse movie presentation components for Series and selected Episode, with cancellation and explicit file targeting.
4. Run regression checks, lint/build and independent review; report database/browser limitations honestly.

Approved in the conversation; no further planning approval required. Do not commit/archive automatically.

## Approved long-series ingestion and scheduling

1. Correct whole-series synchronization: bind each claimed file by its own
   season/episode coordinates; never copy a representative Episode ID to siblings.
2. Parse and reuse Season inventory/snapshots for basic Episode fields; retain
   independent full-detail and artwork completion checkpoints.
3. Create waiting task records before acquiring the scrape lock. Yield catalog
   work at entity boundaries to newly pending media, retaining the same durable
   job and shared write protection. Report current season/episode progress.
4. Verify identity/version isolation, basic-versus-full hydration, request counts,
   lock handoff, cancellation and regressions using isolated PostgreSQL schemas.

User approved this expanded correctness scope after the group-binding defect
was reported. No production repair/retry, restart, commit or concurrency increase.

## Approved scrape identity correction

Fix the missing library entries caused by scan directory hashes being used as
canonical Series primary keys. Both provider and NFO persistence resolve the
preferred Series through the existing metadata attachment, never `series_hint`.
Verify a previously failing unlinked episode against isolated PostgreSQL, then
check repeat persistence and direct Series/Season/Episode ownership. Do not
change production media or restart the running backend as part of code tests.

## Approved presentation refinement

Follow-up: group season selection, episode strip and selected Episode into one
content surface. Use a landscape Episode still and version/tracks column beside
metadata/actions on desktop; on mobile show still, metadata/actions, then tracks.
Use current season number and playable count as the section heading, hiding the
season selector only when there is one season. Approved follow-up adds
GET /media/:id/season through the visible file's canonical SeasonID, reusing
metadata/artwork projection and checking Season NSFW. Render a small own-season
cover/title with cancellation, keyed results and retry. Return season:null when
no visible Season can be resolved; never substitute Series/Episode artwork.

Keep playback and URL behavior unchanged. Move Series and Episode playback actions before secondary metadata and distinguish the real version selector from read-only track information. User follow-up supersedes the earlier disclosure design: render synopsis, file details and informational tracks directly, without folding panels. Preserve movie defaults and existing season/version choices. Verify action order, visible content, selected-file targeting and light/dark responsive layouts, then run lint/build and the selection regression check.
