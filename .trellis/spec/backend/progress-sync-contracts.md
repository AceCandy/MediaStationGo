# Exact PlaybackProgress Synchronization

## Scenario: Exact PlaybackProgress synchronization

### 1. Scope / Trigger
Explicit cross-server progress synchronization uses a versioned API; ordinary playback reporting keeps the existing 20/60-second eligibility rules and event behavior.

### 2. Signatures
`GET|POST /Users/:userId/Items/:id/PlaybackProgress`, also under `/emby`; no lowercase alias. `playback_progress_revisions` is keyed by `(user_id, source, item_id, episode_number)`.

### 3. Contracts
GET optionally accepts `MediaSourceId` and returns `MediaSourceId`, `PositionTicks`, `RunTimeTicks`, `Played`, `Revision` (decimal string). POST requires all five snapshot fields except `Revision`, replaced by `ExpectedRevision`. Ticks must have millisecond precision; duration is positive and position is within bounds. Zero and explicit false are valid; writes set exact resume/played state without sticky completion or playback events. Resolve and verify the visible concrete file and known runtime before updating. Read effective `PlaybackStates` plus revision in one REPEATABLE READ snapshot.

All legacy/NFO/HongGuo/HuangGuoAI state INSERT/UPDATE/DELETE writers advance revisions through triggers, including unwatch, soft deletion and identity changes. Favorite/updated_at-only updates do not advance revisions. Tombstones survive state deletion; old rows start at revision zero without backfill. CAS locks state before revision, matching existing writers; first-write placeholder creation and its revision increment roll back on conflict.

### 4. Validation & Error Matrix
Missing/malformed fields, bounds, precision or known selected-file runtime mismatch → 400; unauthenticated → 401; unauthorized explicit user → 403; invisible/missing/container or mismatched concrete version → 404; stale revision → 409 with no mutation. Existing administrator explicit-user authorization is retained.

### 5. Good / Base / Bad Cases
Good: store one second, then zero/false with the current revision and preserve history/events. Base: ordinary automatic reports retain their existing threshold. Bad: stale cross-server state overwrites a newer device's progress, or a wrong MediaSourceId redirects the write to another file.

### 6. Tests Required
`TestPlaybackProgressRevision` covers all four state sources, ordinary writers, favorites, deletion tombstones, legacy soft deletion/recreation and identity changes; assert no playback events. `TestPlaybackProgressConcurrentFirstWrite` uses separate PostgreSQL connections and asserts one success and one conflict per source. `TestEmbyProgressSnapshotRoutes` covers authentication, both prefixes, validation, short/zero state and conflict rollback. Cross-repository HTTP validation must use the actual client and router backed by isolated PostgreSQL, never production credentials.

### 7. Wrong vs Correct
Wrong: an application-only revision check followed by an unconditional state upsert, or locking revision before the old writer's state row. Correct: transactionally lock state then revision, compare the baseline and update exact state; trigger revisions cover all existing mutation paths.
