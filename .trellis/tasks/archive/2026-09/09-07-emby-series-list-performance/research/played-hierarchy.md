# Emby watched hierarchy follow-up

## Confirmed behavior and boundary

- User approved recursive Series/Season watched and unwatched operations.
- Jellyfin public Folder.MarkPlayed/MarkUnplayed and Emby 3.2.70.0 recurse into
  non-folder children. Series/Season inherit Folder; legacy Emby IsPlayed checks
  all file-backed descendants. Current proprietary Emby was not runtime-tested.
- Only visible file-backed canonical episodes participate here. Season zero is
  supported. New/missing episodes are not pre-marked. Parent legacy history is
  retained but no longer overrides children. No data migration is performed.

## Implementation and verification

- Shared visible episode scope for transactional deduplicated batch writes and
  one current-page aggregate query. Child completed flags survive missing probe
  durations. Manual writes invalidate Emby item caches, not playback events.
- PostgreSQL tests passed for Series/Season detail/list and POST/DELETE handler
  responses, season isolation, zero-duration episodes, user/hidden-library
  isolation, duplicate versions, new episodes, playback event preservation,
  and rollback on the second batch of a 206-episode update.
- Existing series pagination plan regression now also checks the played
  aggregate: one query per page and no whole-catalog file probing.
- Service/repository/handler go vet, Web lint/build and diff checks passed.
- Independent source review found no blocking issue. Existing handler test
  fixture was missing the active history unique index; fixed only that fixture.
- No deployment or existing-service restart. Native clients and browser layouts
  were not exercised. Temporary PostgreSQL is stopped and removed at handoff.

## Sources

- https://github.com/jellyfin/jellyfin/blob/master/MediaBrowser.Controller/Entities/Folder.cs
- https://github.com/MediaBrowser/Emby/blob/3.2.70.0/MediaBrowser.Controller/Entities/Folder.cs
