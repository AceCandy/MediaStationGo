# HuangGuo AI Catalog Isolation

## 1. Scope / Trigger

Read before changing `internal/huangguoai`, `huangguoai_*` persistence, or shared
Web/Emby/scanner/organizer/playback consumers. This catalog is independent of
HongGuo, HuangGuo Video, old CloudFront and article pages.

## 2. Signatures

- Source: `huangguoai`; library type: `huangguoai`; 15 independent model tables
  from `model.HuangGuoAIModels()`. No `metadata_items` surrogate.
- `/api/catalogs/huangguoai/works` is local search/list; `/search` is upstream
  search. Both are paged. `/works/:sourceID/state|favorite|media|episodes|refresh`
  retain numeric source IDs as strings, never UUIDs or display IDs.
- Display IDs: Movie `hga-work-UUID`, Series `hga-group-sourceID`, virtual S01
  `hga-season-workUUID`, Episode `hga-episode-episodeUUID`.
- File tag: `[huangguoai-sourceID] S01Exxx`; Movie keeps episode 1 internally.
  Shared library `/series` returns mixed Movie/Series work cards.
- Settings: `huangguoai.enabled`, `huangguoai.download_root`, source-specific
  scheduler keys. See download config service for current supported keys.

## 3. Contracts

- `ai-huanlian`/`ai-mogai` -> Movie; `ai-duanju`/`ai-manju` -> Series. Counts and
  completion never determine type. Conflicting cross-type membership suspends
  projection; real Movie episodes beyond 1 remain metadata only.
- Works/episodes keep UUIDs on upsert. Favorites use user/sourceID; progress uses
  user/sourceID/episode number. Renaming/rebinding cannot reset these identities.
- Source disable stops new jobs/downloads/bindings; preserves existing files and
  states. `Wait` closes admission, cancels every admitted job and joins them.
  Refresh wakes coalesce without losing a wake during a running manual refresh.
- Discovery requires discover permission plus adult/profile visibility. File
  consumption needs visible bound files; an image can use discovery authorization
  or visible-file authorization. Admin role never bypasses profile adult locks.
- Download re-resolves media per attempt; credentials, keys and signed media URLs
  stay transient. Verify duration and complete decode before no-overwrite publish.
  Completed output and scanner ingestion are separate stages.
- Public Media keeps `metadata_id IS NULL`. Source library scans skip ordinary
  metadata/sidecars. Organizers retain tag/S01 and reject moving bound source files
  into ordinary libraries before file transfer. Auto-mark preserves completed rows.
- Work qualification/counting precedes pagination; hydrate only current-page
  files. Scope node work IDs before joins; SQL LIMIT alone does not bound scans.

## 4. Validation & Error Matrix

| Input/state | Required outcome |
| --- | --- |
| Locked profile/global adult off | 404 on source endpoints, even for admin |
| Non-admin download request | 403 |
| Invalid ID/page/category/rank | 400 |
| Missing classification/true episode | Pending metadata, no invented projection |
| Disabled source scan | Keep old bindings; defer new binding |
| Preview/unknown duration/unsupported encryption | Fail safely, no completed file |
| Lost lease/cancel/publish collision | No overwrite or another attempt's deletion |
| Completed auto-mark predecessor | Preserve position, timestamps and events |

## 5. Good / Base / Bad Cases

Good: a one-episode AI Manju still exposes Series/S01/Episode; a Movie's scanned
S01E001 file exposes Movie without a visible season. Base: fileless discoveries
stay in Discover. Bad: use title equality, source count or current filter category
as identity/type evidence; scan files as ordinary metadata to make them visible.

## 6. Tests Required

Run `TestHuangGuoAI*` in repository/service/handler with isolated
`MEDIASTATION_TEST_POSTGRES_DSN`; source package parser/HLS tests separately.
Assert identity, binding, derived library/latest time, projection with zero legacy
metadata, favorites/progress/NextUp, permission boundaries, leases, cancellation,
verification, source disable/re-enable, organizer coordinates and shutdown wakes.
`TestHuangGuoAIWorkPagePlanAndExactCounts` checks 2,000 works/4,000 episodes, actual
EXPLAIN rows/loops for public page payloads and exact totals, plus global/Latest/Web
behavior. Do not claim it covers every filter or generic prepared plan.
Run Web lint/build and `node web/scripts/check-huangguoai.mjs` with local Vite on
4179. This uses synthetic responses only. Close browser/server/test DB afterward.
Live acceptance evidence is in `docs/huangguoai-design.md`; never commit temporary
media, raw HTML, keys or signed URLs. Real posters and player-device QA are separate.

## 7. Wrong vs Correct

Wrong: infer four real episodes from `episode_count=4`, clear S01 coordinates when
organizing a Movie, or place a source UUID into a source-ID API path.
Correct: persist reported counts separately, bind confirmed episode coordinates,
preserve the numeric source key and existing internal UUID, and use explicit source
projection branches through Web/Emby and shared playback state.

Discover `downloaded` means at least one completed download record, not all
episodes complete, current file existence or ingestion. Hydrate this flag in one
batch scoped to the current list/search rows; details use the same helper.
Download work summaries expose each processing phase count in addition to the
compatible aggregate `active`. Filter and page works before aggregating their
whole episode queues; never truncate a work's counts by the selected status.
`TestHuangGuoAIListDownloadedBadge` and
`TestHuangGuoAIDownloadWorkStatusCounts` verify these projections in PostgreSQL.


## Download execution summaries

- Both Movie (one internal episode) and Series keep one task execution per source
  work. The ID is a deterministic UUID namespaced with
  `huangguoai_download:<sourceID>`; SourcePath stores `huangguoai://<sourceID>`.
  HongGuo identities and queues remain separate; no schema migration is needed.
- Download rows remain authoritative for leases, retries, verification and
  publication. Summaries count the complete work queue using its source-ID index;
  pending phases remain running, and completion requires every row completed.
  Serialize refresh with a per-system/work transaction advisory lock before
  reading a fresh queue snapshot and upserting the stable task ID.
- Refresh only after committed enqueue/action operations and worker entry/exit,
  never on byte heartbeats. Summary failures must not alter business outcomes.
  Startup reconciles running/interrupted summaries once and joins on shutdown,
  including a terminal queue commit followed by a crash before summary refresh.
- Each episode/stage writes a log-only handle, without execution rows or active/
  recent tracker entries. Names use source ID and episode number; source titles,
  title-bearing paths and raw upstream errors never enter summaries or logs.
- `TestHuangGuoAIDownloadWorkTaskLifecycle` covers Movie/Series, supplement,
  mixed states, retry/cancel, recovery, concurrency and cross-system identity.
  `TestHuangGuoAIDownloadSummaryFailureKeepsPublicationAndLogs` verifies publication
  despite missing summary storage and safe diagnostics including upstream errors.
  The transfer/verify/publish integration test asserts both stages share one task.
