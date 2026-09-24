# Parallel three-catalog search

## Goal

Reduce Web keyword-search and suggestion latency by overlapping ordinary, NFO and HongGuo candidate recall, without changing which items users can find.

## Background

- The user approved task creation and the final implementation plan on 2026-09-24, after requesting concurrent source searches and preserving HongGuo's first-season/album index rule.
- At baseline `43c51b6`, Web waited for HongGuo before ordinary/NFO lookup (`internal/service/media_search.go:65`). NFO lookup also waited for ordinary candidate recall and revalidation (`internal/repository/media_search_repository.go:391`).
- The prior file-query optimization is committed as `45ee9d1`; its plan boundaries and regression coverage must remain intact.

## Requirements and Scope

- R1: For Web search pages and suggestions, ordinary, NFO and HongGuo keyword recall must overlap when all three sources are eligible, not merely run HongGuo beside a still-serial ordinary/NFO pair.
- R2: Preserve matching fields, visibility before candidate limits, logical identities, deterministic ranking, totals and existing candidate caps/pagination/suggestions. Each source recalls at most 100 candidates; Web's existing ordinary/NFO-only path can total 200, whereas merging HongGuo applies the final 100-result cap (`internal/repository/media_search_repository.go:391`, `internal/service/media_search.go:71`). Hydrate only final-page representatives; parallelization must not silently remove existing later pages.
- R3: Preserve independent ordinary/HongGuo OpenSearch backends and PostgreSQL fallback; NFO remains database-backed. Terminal database failures remain errors, not partial-success results. Cancel outstanding sibling work on a terminal failure or parent cancellation, and join started work before returning.
- R4: Preserve HongGuo's one-document-per-official-album rule, earliest-stored-season title, standalone/movie identities and visibility through any eligible season's files.
- R5: Keep changes within Web search and the shared repository keyword path; preserve existing Emby callers and empty-query browsing behavior.

## Acceptance Criteria

- [x] AC1 (R1): A deterministic overlap regression proves eligible ordinary, NFO and HongGuo work can enter before blocked sibling work is released, and fails with the former serial flow.
- [x] AC2 (R2, R4, R5): Isolated PostgreSQL regressions preserve mixed-source page/suggestion IDs, order, totals, hidden/locked scope and album presentation, plus shared-path Emby behavior and prior bounded-file plans.
- [x] AC3 (R3): Fallback, database-error and cancellation tests verify no successful partial response and no request-owned work remains running after return; focused tests also pass with the race detector.
- [x] AC4 (R1-R5): Independently review the diff and compare unchanged-fixture results. Report any safe real-backend timing samples separately from unverified deployed/browser/load-test performance.

## Out of Scope

- Index schema/content changes, new caches or dependencies, database migrations, connection-pool/background-task tuning and frontend UI changes.
- New Emby-specific parallel orchestration or changes to its parent/person/playback-state routing; shared repository changes still require compatibility tests.
- Service restart, deployment, remote push, or committing/archiving without a subsequent request.

## Acceptance Boundaries

- Parallel recall can increase instantaneous database/CPU/I/O demand. Success requires demonstrated overlap and preserved results; it does not promise a fixed latency or speedup under unknown production load.
- The final planning summary was approved before implementation; verified and unverified outcomes are recorded in `verification.md`.
