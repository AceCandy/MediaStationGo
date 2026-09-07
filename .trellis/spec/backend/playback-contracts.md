# Playback History and Statistics Contracts

## 1. Scope / Trigger

Apply this contract when changing Web or Emby playback progress, manual watched
state, favorites, resume results, or playback statistics. These paths share the same
per-user, per-metadata history state but playback events are append-only.

## 2. Signatures

- `POST /api/history` and `POST /api/playback/:id/progress` accept media ID,
  position, duration, and optional `session_id`.
- Favorites use `FavoriteRepository.SetByIdentity` and `Toggle`; Web exposes
  `/api/favourites/:id`, `/api/media/:id/favorite`, and the explicit
  `/api/media/:id/series/favorite` route. Emby uses
  `/Users/:userId/FavoriteItems/:itemId`.
- Emby `/Sessions/Playing`, `/Sessions/Playing/Progress`, and
  `/Sessions/Playing/Stopped` forward `PlaySessionId` as `session_id`.
- `GET /api/admin/playback-stats` accepts `grain=day|week|month`, `from`, `to`,
  optional `user_id`, `media_type=movie|tv`, comma-separated `library_ids`,
  `page`, `page_size`, `rank_grain=day|week`, and `rank_date`.
- `playback_histories` is unique on active `(user_id, metadata_id)`;
  `playback_events` is unique on active `(user_id, session_id, metadata_id)`.

## 3. Contracts

- Favorites accept only canonical Movie or Series metadata. The shared repository
  rejects Season/Episode writes with `ErrFavoriteUnsupportedType`; Web and Emby
  return `400`, without converting the target to its parent. Web hides unsupported
  controls and retains the explicit whole-series favorite action. No historical
  favorite migration or compatibility conversion is required before launch.
- `ListFavoriteCards` returns each favorite's own metadata ID, kind, title,
  artwork, and ratings. A Series card retains a visible concrete file/library for
  navigation but clears season/episode coordinates. Apply both representative-file
  and favorite-metadata visibility before selecting one file per favorite.
- The service, not a request `completed` field, calculates completion:
  duration below ten minutes uses `duration - 30s`; otherwise use 90%.
- Automatic progress below 20 seconds is ignored. At 20 seconds or later it
  updates history; a non-empty session ID also creates one event in the same
  transaction.
- Emby progress with both metadata `ItemId` and concrete `MediaSourceId` must
  resolve the visible media directly and verify `media.metadata_id == ItemId`.
  Do not load a full `MediaView`; mismatched or legacy IDs retain the generic
  compatibility resolver.
- Reject a negative position, non-positive duration, or `position > duration`.
  Clamp read-side progress percentages to 0 through 100.
- Manual watched writes completed state without an event. Manual unwatched
  deletes the history row and preserves existing events.
- Emby Series/Season manual watched/unwatched recursively updates visible,
  file-backed episodes in one transaction, deduplicated by metadata ID. A
  Season affects only its own episodes (including season zero). Missing and
  invisible episodes and other users' history remain unchanged; no events
  are added or deleted. Batch upserts use the active history identity index.
- Series/Season `Played` is derived from all visible file-backed episodes,
  not the container's legacy history row. New episodes start unplayed.
  Detail and list payloads agree; lists use one current-page aggregate query,
  never per-item history queries or whole-catalog file probes.
- Movie/Episode payloads honor history `completed` even without probe duration.
  Completed payloads return `Played=true`, `PlayCount=1`, and
  `PlayedPercentage=100`; containers have no playback position of their own.
  Successful manual watched/unwatched writes invalidate `media:emby:` caches.
- Only the authenticated user may read or mutate UserData, history, favorites,
  and realtime sessions. An administrator may target another user only when an
  explicit user ID is supplied.
- Playback statistics retain the legacy `total` and `buckets` fields and add
  `details` and `ranking`. Details are ordered by `played_at DESC, id DESC`;
  the Web UI requests 20 rows per page.
- Ranking returns at most ten rows. Movies group by canonical metadata ID;
  episodes group by their season metadata ID. A day/week window is intersected
  with the selected `from`/`to` range, and weeks start on Monday.
- A deleted media file does not remove its event. Details return
  `media_available=false`, keep the snapshot media ID, and must not link to the
  missing media. Missing titles display `媒体已不可用`.

## 4. Validation & Error Matrix

| Condition | Result |
| --- | --- |
| Favorite mutation targets Season/Episode | `400`; no favorite row or parent favorite is written |
| Invalid progress bounds | Request is rejected; no history or event is written |
| `MediaSourceId` belongs to another `ItemId` | Ignore the mismatched source and retain generic item resolution |
| Position below 20 seconds | Successful no-op for automatic progress |
| Invisible media | Request is rejected; no history or event is written |
| Non-admin explicit different user ID | `403` |
| Invalid statistics grain/date or `from > to` | `400` |
| `page < 1`, `page_size` outside 1..100, or overflowing offset | `400` |
| Invalid `rank_grain`, or `rank_date` outside `from` through `to` | `400` |
| Non-admin statistics request | `403` |

## 5. Good / Base / Bad Cases

- Good: a Series favorite displays its Series poster and links to the Series detail.
- Bad: return the latest imported episode's title/poster as a whole-series favorite.
- Good: a 20-second Web update with a UUID creates or updates history and one
  event; duplicate updates with that UUID do not increment the count.
- Good: Emby `ItemId + MediaSourceId` resolves one visible media row without a
  `metadata_identifiers` projection.
- Base: a mismatched Emby `MediaSourceId` never changes another item's history
  target and follows the compatibility resolver.
- Good: two episodes from one season contribute to one season ranking row,
  while two files sharing movie metadata contribute to one movie row.
- Base: a legacy client without a session ID still saves valid history but does
  not create an event.
- Base: a deleted media file remains in details as an unavailable, unlinked
  audit event.
- Bad: using a client-supplied `completed` value, or inserting an event outside
  the transaction that writes history.
- Bad: ranking episodes separately or counting events outside the intersection
  of the ranking period and selected date range.

## 6. Tests Required

- `TestFavoritesOnlyMoviesAndSeries`, `TestFavoriteHandlersRejectEpisodes`, and
  `TestListFavourites*` verify supported round-trips, unsupported writes, entity-owned
  presentation, and visibility against PostgreSQL. The Web series-presentation check
  verifies movie/series favorite controls and no season/episode controls.
- Cover the ten-minute completion boundary, invalid bounds, 20-second boundary,
  manual watched/unwatched behavior, and percentage clamping.
- `TestEmbySeriesAndSeasonPlayedState` covers Series/Season detail and list
  readback, missing probe duration, repeated mark/unmark, user isolation,
  and manual-write cache invalidation against PostgreSQL.
- `TestEmbyPlayedHierarchyScopeAndRollback` covers season isolation, season
  zero, multi-version deduplication, missing/hidden children, legacy parent
  history, child-to-parent aggregation, and transactional rollback.
- Cover the Emby concrete-source fast path with a query callback asserting no
  SQL contains `metadata_identifiers`, plus a mismatched-source ownership case.
- Cover same-user, non-admin cross-user, and administrator explicit-target
  behavior for every user-scoped Emby route and `/Sessions`.
- With `MEDIASTATION_TEST_POSTGRES_DSN`, assert concurrent history upsert,
  event session de-duplication, partial-index migration, and rollback when
  event creation fails.
- Cover statistics filters and invalid parameter combinations, and synchronize
  `web/src/pages/embyApiCatalog.ts` when player-visible Emby behavior changes.
- Run playback-statistics repository tests against real PostgreSQL; assert
  newest-first pagination, movie/season aggregation, date-range intersection,
  deleted-media availability, and a page beyond the last item.
- For the Web page, run lint/build and check desktop/mobile, light/dark,
  day/week switching, empty/error states, and horizontal overflow.

## 7. Wrong vs Correct

### Wrong

```go
history.Completed = request.Completed
repo.History.Upsert(ctx, history)
repo.PlaybackEvent.Insert(ctx, event)

// A progress ping does not need artwork, identifiers, or display projection.
repo.MediaView.FindByIDs(ctx, []string{mediaSourceID}, filter)

// Reusing this statement after Group/Order leaks aggregation state.
q.Select("DATE_TRUNC(...) AS period").Group("period").Order("period")
q.Select("pe.id").Scan(&details)
```

### Correct

```go
completed := playbackCompleted(position, duration)
return repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	repos := repository.New(tx)
	if err := repos.History.Upsert(ctx, history); err != nil { return err }
	return repos.PlaybackEvent.Insert(ctx, event)
})

// Resolve the visible concrete source and verify it belongs to ItemId.
query := repo.DB.Model(&model.Media{}).Where("media.id = ?", mediaSourceID)
var media model.Media
err := applyUserMediaVisibility(ctx, query, userID).Take(&media).Error

// Build each statistics branch from a clean common-filter statement.
buckets := playbackStatsQuery(ctx, filter).Group("period")
details := playbackStatsQuery(ctx, filter).Order("pe.played_at DESC, pe.id DESC")
```
