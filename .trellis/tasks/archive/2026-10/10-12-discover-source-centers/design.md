# Design

The user's approvals on 2026-10-12 and 2026-10-13 cover source tabs,
Data/Rankings, ordinary-viewer reads, missing-source supplementation and separate
task ownership. The contracts below describe the final combined scope.

## Frontend

DiscoverHubPage owns `system=tmdb|douban|hongguo|huangguoai|bangumi` (default TMDb) and `section=data|rank` for TMDb/Douban. Branch/section switches reset incompatible URL state. Lazy-mount only selected content and key it by source and current account/profile access. Remove DataCenterPage and its route/navigation leaf.

Reuse existing data pages, dialogs and 30-record paging. Replace administrative row lists with shared poster cards, keep administrators' import/refresh and diagnostics, and hide those controls for viewers. TMDb hierarchy, cast and person works remain on-demand; complete saved records do not refresh merely on viewing. Douban detail selection becomes URL-owned by subject UUID.

DiscoverPage accepts a provider and filters section definitions before selecting or fetching a ranking. Its `rank` URL chooses one section; keep existing ordered results, cache, cancellation and explicit ranking refresh. TMDb search belongs to TMDb rankings only. Bangumi reuses the same provider-filtered feed.

Ranking summaries persist without per-item detail calls. Douban ranking dialogs
request exact-ID ensure and reuse the stored-detail dialog with the same
administrator-only force-refresh menu as category details. Missing base/API credits register durable work; ensure failure keeps
the summary and offers retry. TMDb previews always read source facts, preserving
local metadata/translations. Missing source detail registers component work.
Both source dialogs stop polling after 45 two-second attempts, retain the current
data and offer manual reread while background processing continues. TMDb UUIDs
are validated before rendering, and duplicate selections canonicalize to one.

## Backend

Register TMDb root lists in its existing `/catalogs/tmdb` permission-guarded group. Reuse its safe source projections. Admin raw responses and force writes remain under `/admin/tmdb`; Discover-permitted missing-source ensure returns only safe pending state.

Register `/catalogs/douban/subjects` list/detail/poster/credit-image GET/HEAD and
exact-ID ensure POST routes with `can_view_discover`, reusing the current handlers
with an explicit admin projection flag. Public details clear raw payload and
attempt/retry timestamps; safe pending/source-error flags remain. Existing admin
routes retain force writes and diagnostics. Same-origin source-image URLs point
at shared permission-guarded paths; storage and variants do not change.

## Boundaries and Risks

No new catalog/person/queue table or refresh schedule is introduced. Douban
subject schema gains summary, API-credit timestamp and durable supplement fields;
startup reclassifies task history to the current six-system mapping. Ordinary data
GETs remain persisted reads; existing image acquisition remains on-demand.
Public route tests demonstrate authentication/permission enforcement and exclusion
of raw data. Route changes remove the old page rather than adding compatibility
routes. Mocked browser tests verify UI/API contracts; production data and complete
live upstream behavior are outside this task's checks.

## Source supplement and task execution

The follow-up supersedes the initial no-supplement fallback and task grouping. Persisted source summaries are separate from complete payloads. Keep TMDb's existing source request queue; track its explicit background batches as tmdb_data_supplement separately from periodic tmdb_data_refresh. Douban retains its existing subject table and shared API gate/cache: add durable supplement intent, lease/token and retry fields on the subject, plus a separate summary JSON projection. Its service-owned serial worker supplements base and celebrities APIs, preserving old rich fields and local image references; task history is observability, never the queue.

Task kinds map to common/media/tmdb/douban/hongguo/huangguoai. Existing local artwork/people/scrape kinds remain media; provider source-refresh and supplement kinds map by source prefix. Reclassify persisted executions on schema initialization, only where their stored system differs. Do not introduce old catalog URL or dual-read support.

Public source details keep missing scopes and a refresh_pending flag; jobs and raw payloads are admin-only. Ranking GETs never apply local work edits. Viewer supplement POSTs validate source identity and preserve freshness, retry backoff, deduplication and per-user queue bounds. Automatic work does not recurse into all seasons, episodes or persons. Service shutdown cancels and joins workers; failed/leased work is recoverable from business rows.

Shared ModalShell owns initial focus, top-dialog keyboard containment and focus restoration; remove the redundant Douban trap. Feed cursors advance only for successful pages; failed continuation pauses observers and offers same-page retry. Include auth session version in in-flight sections deduplication and expose section-read errors instead of treating them as empty sources. Preserve card identity when images fail.

Douban saves every director/actor returned by the celebrities API, including
source IDs, roles and avatars, but does not treat `total` as the size of its arrays.
Live subject 36600459 returned one director and 37 actors with total 87, unchanged
when requesting count 100. Automatic supplementation remains API-only. Existing
manual webpage supplement preserves an already successful API personnel list.

## Unified browsing and TMDb details

Use shared visual controls for source sections, category chips, poster grids and
ranking rows, retaining the existing source-owned request lifecycles. TMDb and
Douban expose local category options and optional genre/department filters;
genre membership is existential so multiple values never duplicate a subject.
TMDb genre values use source IDs; Douban uses normalized source names. Person
department is single-valued primary occupation. No category table or upstream
fetch is needed. Missing category metadata stays visible in All.

Extract the current TMDb detail controller into a shared source modal. Ranking
identity resolves through the existing Discover read to a center UUID, then uses
the same modal as categories. Reuse source detail/children/credits/works routes,
source image URLs and optional click support on shared circular MediaCredits.
Keep navigation in one shell with a history stack. Only the selected entity is
ensured; filmography remains bounded and source-owned. Centralize human-readable
status/department labels and keep nonexistent fields hidden.

The header combines a faint decorative backdrop, portrait/poster, aligned facts
and a distinct score/count area. Synopsis and circular credits follow; series
show season poster cards, seasons show landscape episode rows, and people show
filterable work posters. Admin mutation actions use the existing menu primitive;
diagnostics remain folded. Pagination and error controls preserve successful
data and retry the failed page. Existing unrelated uncommitted work is retained.
