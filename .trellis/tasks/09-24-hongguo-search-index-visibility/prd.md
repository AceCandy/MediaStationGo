# HongGuo search index visibility

## Goal

Reduce Web search and suggestion latency by indexing file-backed HongGuo logical works with library membership, instead of enumerating all currently visible HongGuo identities for every keyword request. Preserve search results, catalog identity and current-user access checks.

## Background

- The user approved this direction, task creation and the final planning summary on 2026-09-24. Implementation is now authorized.
- Current HongGuo documents are built from canonical works/albums without media membership (`internal/repository/hongguo_search_index.go:16`). Search enumerates visible IDs before OpenSearch and reuses the same database scope for candidate revalidation (`:52`).
- Read-only diagnosis of the current parallel implementation found that a median-total mixed-page sample spent 574 ms enumerating 4,862 visible identities, 13 ms in HongGuo OpenSearch, and 370 ms revalidating candidates. These are live-data samples, not a production latency guarantee.
- The completed but uncommitted `09-24-search-source-parallel` changes remain separate and must be preserved.

## Requirements

- R1: Healthy indexed Web keyword/suggestion search must not enumerate all visible HongGuo work IDs before calling OpenSearch. File/library eligibility must be applied before candidate limits; specialized person/favorite/playback filters retain their existing pre-limit semantics.
- R2: Index only file-backed logical works and keep their media-library membership current after supported file additions, removals, rebinding and library changes, as well as official-album/title changes.
- R3: Preserve standalone/movie/official-album identities, earliest-stored-season titles, visibility through any eligible member season, Web and shared Emby search contracts, ordering and candidate/page caps.
- R4: Retain bounded current-database verification of candidate existence, visibility and title matching. A successful empty index response ends without a redundant candidate query; index failure remains distinguishable from empty success.
- R5: Existing or incompatible indexes, index rebuilds and known failed incremental synchronization must not be mistaken for successful empty search. Preserve safe fallback and rebuild cancellation behavior; ordinary search must remain compatible with its current index.
- R6: Preserve the existing near-real-time indexing boundary rather than promise immediate cross-system read-after-write consistency. Current database checks must prevent forbidden results. After process restart, HongGuo search must not trust potentially interrupted synchronization before safe reconstruction; disabled/failed warmup retains usable database search.

## Acceptance Criteria

- [x] AC1 (R1, R4): Query capture and realistic PostgreSQL execution plans prove the healthy indexed path avoids global visible-ID enumeration and scopes revalidation to returned candidate works. Permission-before-limit tests cover allowed-only, hidden-only, empty intersection, locked-empty and mixed visible/hidden membership against more than 100 ineligible competing hits.
- [x] AC2 (R2, R3): Lifecycle tests cover file add/delete/rebind/library movement, multi-library and multi-season albums, fileless works, and title/album changes without modifying source identities.
- [x] AC3 (R3, R5, R6): On stable data, mixed Web page/suggestion and shared Emby regressions preserve access, results, order and totals; compatibility/failure/rebuild tests preserve fallback and prevent stale writes from activating incomplete indexes. Rollback emits no document writes or committed dirty IDs; cancelled/partial committed changes still invalidate correctly. Mutation, search and rebuild owners share failure state. Restart with an existing new-version alias still requires initial reconstruction.
- [x] AC4 (R1-R6): Independent review and scoped tests pass; report real-backend timing evidence separately from unverified deployment/load behavior. No production data/index mutations are needed to complete planning.

Verification evidence and explicit live OpenSearch/deployment limits: `verification.md`.

## Out of Scope

- Ordinary/NFO index redesign, frontend changes, broader card-loading optimization, new dependencies or caches, and changes to playback or source discovery behavior.
- Deploying/restarting services, rebuilding live indexes, committing/archiving or pushing without a separate request.

## Constraints and Acceptance Boundaries

- The mutation ownership and index lifecycle audit is recorded in `research/findings.md`; the design must cover each supported entry point, not only scanner Upsert.
- Reuse existing index synchronization, batching and fallback; no new cache, durable queue, background polling system or relational schema is planned.
- Short index-refresh delays remain possible for newly added/moved files. Out-of-band administrator SQL and multiple concurrent application writers are not newly supported consistency modes.
- The first safe reconstruction is required before the new indexed speedup; if warmup is disabled or fails, database search remains available. Deployment/rebuild behavior and load-dependent performance remain separate from implementation acceptance.
- Implementation follows the approved design; deployment, live rebuild and submission remain separately authorized operations.
