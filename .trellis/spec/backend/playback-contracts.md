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
  `page`, `page_size`, `rank_grain=day|week`, `rank_date`, and
  `system=all|catalog|hongguo|nfo` (API default remains `catalog`).
- `playback_histories` is unique on active `(user_id, metadata_id)`;
  `playback_events` is unique on active `(user_id, session_id, metadata_id)`.
- Web continuation is `GET /api/watch-history/continue`; Emby separates
  `/Items/Resume` and `/Items?Filters=IsResumable` from `/Shows/NextUp` (also
  user-scoped, lowercase, and `/emby` variants).
- `playback.auto_mark_previous_episodes` is an administrator setting, default
  false, edited through the existing single-key settings API.

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
- Ordinary, NFO and HongGuo state have nullable internal `resume_position_ms`.
  NULL interprets legacy completed snapshots as zero resume, otherwise uses
  `position_ms`. Automatic reports store zero on completion and a positive
  replay position independently of sticky `completed` (old OR new). Explicit
  unwatch resets state. Keep original position/duration snapshots for statistics.
- `repository.PlaybackStates` is a read-only effective-state projection shared
  by detail, hierarchy, Resume and NextUp before filtering/grouping/paging.
  When the recorded file is unavailable, compare its resume position with a
  visible same-identity preferred replacement's probe duration. Unknown duration
  or multipart replacement cannot infer completion. Do not rewrite snapshots or
  events on reads, and never downgrade a completed mark. A deleted source's late
  Emby report must not resolve to a different file and overwrite progress.
- A concrete non-multipart playback report uses that file's known probe duration
  after validating client bounds; clamp position to that duration. Do not use a
  different version's duration. Multipart retains its reported group timeline.
- Automatic progress requires a position of at least 60 seconds when duration
  exceeds ten minutes, or 20 seconds otherwise (including exactly ten minutes).
  Earlier positions are ignored; eligible updates save history and a non-empty
  session ID also creates one event in the same transaction.
- Full history remains Episode-grained. Emby resume reads group visible,
  resumable Episodes (including watched replays) by canonical Series before pagination and retain the most
  recently watched Episode; Movies and items without a Series group by their own
  logical identity. HongGuo groups by its source work or official album and
  never merges with canonical media by title.
- Web continuation and Emby NextUp share `HistoryRepository.Continuations`.
  Prefer the latest visible resumable episode; without a resumable episode,
  advance from the furthest completed coordinate to the first later visible,
  file-backed unplayed episode. Canonical/NFO order by season/episode; HongGuo
  orders by season_index/source_id/episode, preserving source state identity
  when several works have the same season number. Bulk marking is not ordered
  playback: different per-episode timestamps must not move the completion
  boundary backward. Group activity uses the maximum watched timestamp.
  Untouched and exhausted groups produce no next candidate. Specials can
  resume; positive-season progress never automatically returns to season zero.
- Web preserves canonical/NFO 20-second resume eligibility and HongGuo positive
  progress eligibility; NextUp excludes any group with positive resumable
  progress to avoid duplicating Emby mixed Resume. NextUp excludes movies.
  Its `UserId` defaults to the authenticated user; `SeriesId` filters public
  series identities, `Fields` uses existing payload rules, `Limit` is 1..100
  (default/fallback 20), negative/overflow `StartIndex` falls back to zero.
  Visibility precedes grouping, successor selection, exact count and paging.
  Bound each source by StartIndex+Limit only after finding valid candidates;
  hydrate current-page items in batches. Never cap raw history or expand a
  whole catalog into display nodes to find successors.
- A Web continuation response keeps `{history, media}` and adds
  `history.is_next=true` for derived recommendations. A `next:<item ID>` history
  ID is a read-only projection, never persisted or included in full history or
  statistics. Next-episode cards link directly to the selected concrete file
  (`/media/<media.id>`), not a series page that defaults to its first episode.
- Mixed-catalog Emby global `IsResumable` uses `globalResumeItems`: filter each
  source's current-user state and visible files before grouping, count distinct
  resume keys per source, then merge at most `StartIndex + Limit` grouped rows
  per source in PostgreSQL. Preserve database collation, NULL ordering and
  `played_at DESC, id DESC` representative selection. Do not expand the full
  catalog into Series/Season nodes or join artwork/probe data for candidates.
  This path retains `position_ms > 0`; the no-NFO `ResumeItems` history path
  retains its existing threshold and preferred-version behavior. Album grouping
  still returns the selected playable Episode, never an album container.
- Web history and continue cards fill an absent Episode poster with its Season
  poster, then Series poster, then the existing display backdrop. Batch-load
  artwork for the returned page only; preserve canonical selections and concrete
  playback identity. Web history, continue and featured sections show SeriesTitle
  as the heading and season/episode coordinates plus a non-generic episode title
  below it. Season zero is displayed as specials; Movies keep their own title.
- Emby progress with both metadata `ItemId` and concrete `MediaSourceId` must
  resolve the visible media directly and verify `media.metadata_id == ItemId`.
  Do not load a full `MediaView`; mismatched or legacy IDs retain the generic
  compatibility resolver.
- Reject a negative position, non-positive duration, or `position > duration`.
  Clamp read-side progress percentages to 0 through 100.
- Manual watched writes completed state without an event. Manual unwatched
  deletes the history row and preserves existing events.
- With automatic previous-episode marking enabled, an eligible completed
  progress report marks only the current user's visible, file-backed earlier
  Episodes in the same canonical Season (including season zero). HongGuo uses
  its source work and source episode number, never a manual display group.
  Missing/invalid settings disable the feature; saving the setting or manually
  marking an item does not backfill history. Existing completed rows retain
  their timestamps and positions, including during conflict updates. Current
  history, supplemental marks and the current session event commit atomically;
  supplemental marks never create playback events. Web and Emby canonical
  progress share `PlaybackService.saveProgress`, preserving the Emby lightweight
  source resolver without loading identifiers or artwork.
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
- Statistics read independent event tables through source projections. `all`
  uses SQL `UNION ALL` before counting, bucketing, detail pagination and Top 10.
  Detail and ranking rows expose `system`; rank identity is `(system, group_id)`
  and details break cross-table ID/time ties by `system ASC`. Never merge titles,
  concatenate independently paginated results or sum independently capped ranks.
- NFO movies rank by local item, episodes by local season. HongGuo preserves
  source-work ranking even when official albums group display. NFO file links
  require the current binding to match the event item; deleted/rebound files
  retain events but are unavailable. No metadata surrogate or summary table.
- The statistics Web page defaults to `all`; explicit source URLs remain valid.
  Switching source resets filters/page and shows only matching library types.
  Ordinary library choices exclude HongGuo and both NFO library types. Combined
  details/ranking show source labels and use source-qualified React keys.
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
| Position below 60 seconds for duration > ten minutes, or below 20 seconds otherwise | Successful no-op for automatic progress |
| Several incomplete Episodes belong to one visible Series | Continue watching returns only the most recently watched Episode; full history keeps every Episode |
| Completed season, later visible unplayed episode | Web returns an is_next card; Emby NextUp returns the same logical Episode |
| Incomplete episode in a started group | Web resumes it; NextUp omits the group; Resume semantics remain unchanged |
| Watched episode replayed past recording threshold | Played remains true; Resume shows new position; NextUp omits the group |
| Deleted long file, shorter visible replacement already finished | Effective Played=true and resume=0; season and NextUp use the corrected state |
| Missing replacement duration or no visible replacement | Do not infer completion from stale duration |
| No history, all completed, or no visible successor | No next-episode recommendation |
| NextUp offset beyond final page | Empty Items with the exact unchanged TotalRecordCount |
| Invisible media | Request is rejected; no history or event is written |
| Auto-mark off or progress incomplete | No earlier episode is changed |
| Auto-mark on and progress completed | Only visible earlier episodes in the same season are completed; failures roll back the progress transaction |
| Non-admin explicit different user ID | `403` |
| Invalid statistics grain/date or `from > to` | `400` |
| Unknown statistics system | `400`; never silently select another source |
| `page < 1`, `page_size` outside 1..100, or overflowing offset | `400` |
| Invalid `rank_grain`, or `rank_date` outside `from` through `to` | `400` |
| Non-admin statistics request | `403` |

## 5. Good / Base / Bad Cases

- Good: a Series favorite displays its Series poster and links to the Series detail.
- Bad: return the latest imported episode's title/poster as a whole-series favorite.
- Good: a 20-second Web update for a two-minute video with a UUID creates or updates history and one
  event; duplicate updates with that UUID do not increment the count.
- Good: Emby `ItemId + MediaSourceId` resolves one visible media row without a
  `metadata_identifiers` projection.
- Base: a mismatched Emby `MediaSourceId` never changes another item's history
  target and follows the compatibility resolver.
- Good: two episodes from one season contribute to one season ranking row,
  while two files sharing movie metadata contribute to one movie row.
- Good: Episodes 6 and 7 both retain history, while every Web/Emby resume read
  returns only the more recently watched incomplete Episode 7.
- Good: S01 is marked complete in arbitrary write order and both continuation
  consumers select S02E01. Redoing the S01E01 mark does not rewind selection.
- Bad: order HongGuo only by season/episode and collide two source works, or
  let the most recently written bulk mark define the completed boundary.
- Bad: deduplicate after `LIMIT`, because repeated Episodes can consume the
  candidate window and hide older resumable Series or Movies.
- Base: a legacy client without a session ID still saves valid history but does
  not create an event.
- Good: completing S02E05 with auto-mark enabled completes missing visible
  S02E01–E04 history, preserving already completed rows and every other season.
- Bad: apply auto-mark only to Web, or merge HongGuo seasons by display group.
- Base: a deleted media file remains in details as an unavailable, unlinked
  audit event.
- Bad: using a client-supplied `completed` value, or inserting an event outside
  the transaction that writes history.
- Bad: ranking episodes separately or counting events outside the intersection
  of the ranking period and selected date range.

## 6. Tests Required

- `TestPlaybackStateReplayAndDeletedVersion` covers all three sources, replay,
  completion, explicit unwatch, legacy snapshots, unknown duration, hidden
  replacements, user isolation and read-only reconciliation on PostgreSQL.
- `TestContinuationCrossSeason` covers canonical/NFO/HongGuo manual season
  completion, out-of-order marks, equal-season album members, hidden successor
  files, version preference, resume priority, user isolation and read-only state.
- `TestContinuationPlansAndMixedPagination` checks real PostgreSQL plan
  rows/loops against 2,000 series/4,000 episodes per catalog and other-user
  states, stable mixed pages/exact totals and exhausted groups before valid
  candidates. No wall-clock assertion or assumption that a LIMIT bounds scans.
- `TestNextUpRoutesAndWebContinuation` and `TestEmbyTargetUserRequired` cover
  all route aliases, current-user and cross-user access and nested Web markers.
  Run `node scripts/check-history-presentation.mjs` and
  `node scripts/check-nextup.mjs` from `web` (the latter uses a local preview,
  mocked APIs and an isolated browser) for target links and catalog behavior.

- `TestFavoritesOnlyMoviesAndSeries`, `TestFavoriteHandlersRejectEpisodes`, and
  `TestListFavourites*` verify supported round-trips, unsupported writes, entity-owned
  presentation, and visibility against PostgreSQL. The Web series-presentation check
  verifies movie/series favorite controls and no season/episode controls.
- Cover the ten-minute completion and recording boundaries, invalid bounds, 20/60-second recording boundaries,
  manual watched/unwatched behavior, and percentage clamping.
- `TestPlaybackAutoMarkPreviousEpisodes` and `TestHongGuoAutoMarkPreviousEpisodes`
  exercise the toggle, Web/Emby writes, no-session compatibility, same-season
  bounds, visibility, version deduplication, user isolation, completed-row
  preservation, event isolation and rollback against PostgreSQL.
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
- Cover canonical and HongGuo same-Series resume grouping, newest-Episode
  selection, visibility, user isolation, and pagination after grouping across
  Web Continue, Emby Resume, and `IsResumable`; assert full history is unchanged.
- `TestEmbyResumeSourcesGroupBeforeMerge` compares mixed-source pages with the
  original node query and covers filters, offsets, exact totals, versions,
  albums, time ties and missing timestamps. `TestEmbyResumeCandidatesIgnoreUnwatchedCatalog`
  checks actual PostgreSQL plan rows/loops against large unrelated catalogs and
  other users' states; SQL shape or elapsed-time assertions alone are insufficient.
- Run playback-statistics repository tests against real PostgreSQL; assert
  newest-first pagination, movie/season aggregation, date-range intersection,
  deleted-media availability, and a page beyond the last item.
- `TestPlaybackStatsAllSystems` covers interleaved source events, identical
  cross-table IDs/timestamps, timezone buckets, per-source and combined filters,
  stable global pagination, rank-window intersection, duplicate local titles,
  Top 10 and NFO deleted/rebound file links. Existing ordinary/HongGuo stats
  tests must still pass. HTTP tests cover all four systems and admin boundaries.
- `node scripts/check-playback-stats.mjs` uses an isolated browser and mocked
  API against a local preview: defaults/deep links, source switching, filter
  reset, paging, labels/links, empty/error states and light/dark responsive views.
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

// Group visible resumable Episodes before applying the requested page.
grouped := visibleResumeScope.Distinct("series_id").Order("series_id, watched_at DESC")
page := db.Table("(?) AS grouped_resume", grouped).Order("watched_at DESC").Limit(limit)
```

Wrong: persist a zero-progress row for S02E01 just to display a recommendation,
or limit the latest 20 history rows before discarding exhausted series.
Correct: derive the next visible episode from current state, then page valid
group candidates and return a read-only `is_next` projection.
