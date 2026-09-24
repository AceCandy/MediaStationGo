# Implementation verification — 2026-09-24

## Outcome and boundary

Implemented file-backed HongGuo schema-v2 documents with sorted library unions,
positive library filtering before the candidate limit, bounded hit verification,
successful-empty short circuit and conservative startup fallback. Ordinary schema
v1, frontend behavior and existing three-source orchestration remain unchanged.
The prior parallel task's `internal/service/media_search.go` SHA-256 remains
`4cefe3885f4eb184364886909d96eccee99205e5f3c4878c23261fb4197ee699`.

Media writes share the configured HongGuo repository. All mutation families from
`research/findings.md` notify after commit; partial rebind/prune progress is covered.
Uncaptured committed changes invalidate both indexed reads and an ongoing rebuild.
No dependency, relational migration, cache, queue or production deployment added.

## Checks executed

Database checks ran against the task-owned PostgreSQL 17 container, not production.
The selected PostgreSQL tests executed rather than skipping.
The task-owned container was stopped and automatically removed after verification;
no test service remains running. Its disposable test data was removed with it.

- Initial red regressions reproduced fileless index documents and global
  `CandidateIDs`. Both passed after the implementation.
- Expanded `go test ./internal/repository ./internal/service ./internal/handler`
  with `-run 'TestHongGuo|TestOpenSearch|TestMetadataSearch.*(Backfill|Rebuild|Cancel)|TestWebSourceSearch|TestEmbySourceSearch|TestEmbyHongGuo|Test.*(DeleteLibrary|DeleteByLibrary|PruneMissing|RemovePath|Reclassify|Duplicate)' -count=1`
  passed: repository 35.809 s, service 130.731 s, handler 1.912 s.
- Focused `-race` checks for committed cancellation, fileless-batch membership dirty
  replay, mutation/recovery, partial rebind, service mutation owners and all
  `TestWebSourceSearchParallel*` passed: repository 7.307 s, service 22.977 s.
- `TestHongGuoSearchServiceMutationOwners` covers media/library/root deletion,
  prune and partial prune, watcher, duplicate cleanup, organizer path deletion,
  replacement followed by transfer failure, both library-movement owners, and outer
  library-transaction rollback. The HTTP stub observes only post-commit writes.
- `TestHongGuoSearchPermissionBeforeLimit` uses 150 hidden competitors plus one
  mixed visible/hidden work; allowed-only, hidden-only, raw restricted, mixed,
  empty-intersection and locked scopes pass.
- Version compatibility rejects HongGuo v1 and preserves ordinary v1. Startup and
  restarted repositories stay on PostgreSQL until successful reconstruction.
- `go vet ./internal/repository ./internal/service ./internal/handler`,
  `node web/tests/search-source-cards.mjs`, gofmt and `git diff --check` passed.
  Node emitted only the existing NO_COLOR/FORCE_COLOR environment warning.
- Two separate read-only reviews found no confirmed correctness or mutation-coverage
  blocker. Main-thread tests additionally cover the suggested cancellation and
  permission-before-limit cases. Reviewers' non-isolated test claims are not used as
  substitutes for the isolated test results above.

## Query-plan evidence

`TestHongGuoSearchBoundsFileWork` uses 10,000 works and 40,000 realistic-width files.
Healthy search captures two relevant queries (bounded verification and existing
representative-file loading), not the former global visible-identity query.
The verifier bounds work, album and file processing; PostgreSQL 17's generic plan
also avoids sequential catalog scans. Arrays retain parameter binding.

Last isolated sample: verifier **0.167 ms**, representative query **3.784 ms**.
These are SQL execution samples, not an end-to-end latency guarantee. The unchanged
representative/card path can still scan the work table; only its existing file-work
bound is asserted. That remaining work is outside this task's approved boundary.

## Unverified behavior and rollout requirements

- Opt-in real OpenSearch test was attempted, but configuration resolved to
  `OpenSearch not configured`, including from the repository root. It stopped before
  creating any index. No real search alias was changed and no temporary index exists
  from these attempts. HTTP contract tests and the document-filter test double pass,
  but they do not establish actual OpenSearch analyzer/refresh behavior or latency.
- No deployment, production index rebuild, browser end-to-end timing, load test or
  live stable-snapshot equivalence measurement was performed.
- Deployment must complete the first safe HongGuo v2 reconstruction before gaining
  the indexed speedup. Disabled/failed warmup retains PostgreSQL search.
- Incremental visibility remains near-real-time. Current database verification
  prevents forbidden hits, but newly added/moved files may wait for index refresh.
- Multiple application writers and out-of-band SQL are not newly supported.
- At the end of implementation, code was deliberately left uncommitted/unarchived.
  The user authorized committing and archiving both related search tasks on
  2026-09-25. Deployment and remote push remain outside that authorization.

## Bug analysis

### 1. Root cause category

B (cross-layer contract) and D (test coverage): HongGuo index documents did not
own file/library eligibility, so each keyword request reconstructed the entire
visible identity set. Limiting file expansion alone left that catalog-wide work.

### 2. Why earlier optimization was insufficient

Parallel sources reduce serial waiting and bounded file EXISTS reduces episode
expansion, but neither removes global identity enumeration or broad hit verification.
During this implementation, a test initially assumed scanner Upsert moves a file
between nonempty libraries. Actual `addMediaPlacementUpdates` preserves the existing
assignment; the test was corrected, and real movement owners were tested separately.

### 3. Prevention mechanisms

| Priority | Mechanism | Action | Status |
| --- | --- | --- | --- |
| P0 | Executable document contract | File-backed library union and permission-before-limit tests | Done |
| P0 | Mutation ownership | Shared capture/post-commit refresh; all audited owners exercised | Done |
| P0 | Recovery | Startup fallback, rollback/partial/cancel checks, invalid rebuild rejection | Done |
| P1 | Plan regression | Bounded actual and generic verifier plans | Done |

### 4. Systematic expansion

Audited scanner, library, duplicate and organizer writers rather than fixing only
Upsert. No unrelated source/card redesign was included. Future mutation owners must
preserve the same capture/commit/refresh boundary.

### 5. Knowledge capture

Updated the existing Independent catalog search indexes scenario in
`.trellis/spec/backend/hongguo-catalog.md`, preserving the preceding parallel task's
rules. No new global guide, template or automatic commit was needed.
