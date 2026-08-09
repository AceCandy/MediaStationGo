# Implementation Plan

## 1. Schema and Repository Contracts

- Add metadata-level runtime, separate catalog metadata/artwork checkpoints,
  provider snapshot, and staged durable hydration job models; register them
  with AutoMigrate.
- Add repository methods for snapshot upsert/read, job enqueue/claim/retry/
  complete, stale-running recovery, per-entity completion, and incomplete child
  queries.
- Extend Season/Episode upserts to persist provider identifiers and to use the
  existing explicit metadata merge path for an exact provider crosswalk.
- Verify PostgreSQL constraints, JSONB round-trip, job uniqueness, and
  Series/Season/Episode identity restoration.

## 2. TMDb Entity Detail Contracts

- Add typed Series, Season, and Episode catalog responses while retaining the
  validated raw JSON body.
- Request the approved stable append resources, map common typed fields and
  credits, and produce original image URLs.
- Add bounded HTTP 429 retry using `Retry-After` or exponential backoff with
  context cancellation.
- Replace full-URL provider errors with sanitized endpoint/entity/status errors
  before they can reach logs or durable job state.
- Test Series Season inventory, Season Episode inventory, season zero,
  Episode runtime/credits/IDs, fixed entity-to-artwork mapping, missing
  artwork, raw snapshot preservation, 429/cancellation behavior, and
  credential-free errors with an HTTP test server.

## 3. Idempotent Entity Persistence

- Reuse canonical metadata, artwork, people-image, and credit paths through one
  catalog entity persistence helper.
- Persist each entity's typed fields, provider IDs, raw snapshot, own credits,
  and selected original artwork before marking it complete. Aggregate every
  required entity artwork type into the single artwork checkpoint.
- Reuse best-effort people profile import and verify success writes local bytes
  while failure preserves the prior key without blocking entity completion.
- Preserve entity-owned artwork on transient download failure, leave the
  checkpoint incomplete, and never synthesize a parent fallback.
- Test repeated persistence, exact identity attachment/merge, empty own
  credits, failed image retry, and no `Media` creation.

## 4. Durable Full-Tree Worker

- Replace the in-memory pending map as queue truth with staged job upsert plus a
  coalescing wake signal and persisted retry timer.
- Reject unsupported kinds and non-positive IDs before job upsert. Claim one
  eligible job at a time; reset all running jobs on single-process startup and
  give root-stage work bounded burst priority without starving Season work.
- Persist the root and expected Season shells first. In seasons stage, process
  at most one Season per turn and requeue for fair progress across Series.
- Skip only own metadata/artwork work whose corresponding checkpoint is valid;
  skip a descendant scope only when its full checkpoint is valid.
- Mark Episode, Season, Series, and job completion bottom-up; record retry state
  and bounded errors on failure.
- Cover invalid IDs, duplicate Movie/Series rows, root-only Movie completion,
  bounded root priority, per-Season fairness, old root-only timestamps, partial
  Episode/image failure, resume after service reconstruction, ancestor
  completion gating, automatic retry scheduling, and clean shutdown.

## 5. Projection and Emby Behavior

- Remove parent/Series artwork fallback from `MediaView` and any sibling Emby
  image resolver.
- Remove Series credit fallback for Season/Episode payloads while preserving
  explicit parent IDs/names used for hierarchy navigation.
- Ensure a linked Episode exposes only its own still and own metadata/credits;
  missing own data remains empty or uses the existing route placeholder.
- Verify physical library top-level lists still contain logical Movie/Series
  items, Series opens to Seasons, Season opens to Episodes, and catalog-only
  metadata remains outside physical library membership.

## 6. Wiring and Compatibility

- Keep discover response assembly independent from provider/image work; only
  the idempotent job upsert occurs before return.
- Start and join the worker through the existing service container lifecycle.
- Ensure a new durable job causes a previously root-only hydrated Series to run
  the full-tree path.
- Keep scanner binding by canonical identity/hierarchy and verify it does not
  overwrite prehydrated metadata.

## 7. Validation and Independent Review

- Run `gofmt` on changed Go files.
- Run focused service, repository, handler, model, and database tests for the
  touched contracts. Use `MEDIASTATION_TEST_POSTGRES_DSN` integration tests
  when the environment provides it; otherwise record the skip.
- Run `go test` only for affected Go packages rather than an unrelated full
  repository test sweep.
- Run `git diff --check`.
- Run an independent Trellis check for spec compliance, cross-layer data flow,
  missing tests, and accidental inheritance paths; fix verified findings.
- Update `.trellis/spec/backend/shared-media-metadata.md` with the final
  executable contracts before commit.

## Risky Files and Rollback Points

- Schema/repository: additive models and completion semantics. Roll back code
  while leaving unused additive schema in place; do not delete catalog data.
- TMDb provider: shared HTTP behavior and detail mapping. Keep existing manual
  scrape entrypoints compatible and cover them with focused regressions.
- MediaView/Emby: removing fallback is a deliberate visible behavior change.
  Reverting only the projection restores the previous display behavior.
- Worker: disable the discover enqueue call to stop new jobs while preserving
  discover responses and all completed metadata.

## Pre-Start Gate

- PRD, design, implementation plan, and research note agree on primary-only
  original artwork and no metadata/credit/artwork inheritance.
- No unresolved product decisions remain.
- User explicitly approves the latest planning summary before product code is
  edited.
