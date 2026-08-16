# Playback History and Statistics Contracts

## 1. Scope / Trigger

Apply this contract when changing Web or Emby playback progress, manual watched
state, resume results, or playback statistics. These paths share the same
per-user, per-metadata history state but playback events are append-only.

## 2. Signatures

- `POST /api/history` and `POST /api/playback/:id/progress` accept media ID,
  position, duration, and optional `session_id`.
- Emby `/Sessions/Playing`, `/Sessions/Playing/Progress`, and
  `/Sessions/Playing/Stopped` forward `PlaySessionId` as `session_id`.
- `GET /api/admin/playback-stats` accepts `grain=day|week|month`, `from`, `to`,
  optional `user_id`, `media_type=movie|tv`, and comma-separated `library_ids`.
- `playback_histories` is unique on active `(user_id, metadata_id)`;
  `playback_events` is unique on active `(user_id, session_id, metadata_id)`.

## 3. Contracts

- The service, not a request `completed` field, calculates completion:
  duration below ten minutes uses `duration - 30s`; otherwise use 90%.
- Automatic progress below 20 seconds is ignored. At 20 seconds or later it
  updates history; a non-empty session ID also creates one event in the same
  transaction.
- Reject a negative position, non-positive duration, or `position > duration`.
  Clamp read-side progress percentages to 0 through 100.
- Manual watched writes completed state without an event. Manual unwatched
  deletes the history row and preserves existing events.
- Only the authenticated user may read or mutate UserData, history, favorites,
  and realtime sessions. An administrator may target another user only when an
  explicit user ID is supplied.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Invalid progress bounds | Request is rejected; no history or event is written |
| Position below 20 seconds | Successful no-op for automatic progress |
| Invisible media | Request is rejected; no history or event is written |
| Non-admin explicit different user ID | `403` |
| Invalid statistics grain/date or `from > to` | `400` |
| Non-admin statistics request | `403` |

## 5. Good / Base / Bad Cases

- Good: a 20-second Web update with a UUID creates or updates history and one
  event; duplicate updates with that UUID do not increment the count.
- Base: a legacy client without a session ID still saves valid history but does
  not create an event.
- Bad: using a client-supplied `completed` value, or inserting an event outside
  the transaction that writes history.

## 6. Tests Required

- Cover the ten-minute completion boundary, invalid bounds, 20-second boundary,
  manual watched/unwatched behavior, and percentage clamping.
- Cover same-user, non-admin cross-user, and administrator explicit-target
  behavior for every user-scoped Emby route and `/Sessions`.
- With `MEDIASTATION_TEST_POSTGRES_DSN`, assert concurrent history upsert,
  event session de-duplication, partial-index migration, and rollback when
  event creation fails.
- Cover statistics filters and invalid parameter combinations, and synchronize
  `web/src/pages/embyApiCatalog.ts` when player-visible Emby behavior changes.

## 7. Wrong vs Correct

### Wrong

```go
history.Completed = request.Completed
repo.History.Upsert(ctx, history)
repo.PlaybackEvent.Insert(ctx, event)
```

### Correct

```go
completed := playbackCompleted(position, duration)
return repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	repos := repository.New(tx)
	if err := repos.History.Upsert(ctx, history); err != nil { return err }
	return repos.PlaybackEvent.Insert(ctx, event)
})
```
