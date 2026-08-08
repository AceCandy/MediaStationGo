# Refactor Emby library item queries around metadata scopes

## Goal

Make Emby browse, search, count, detail, and playback behavior use
`MetadataItem` as the logical work identity while `Media` remains the source of
physical library membership and playable versions. The result must paginate
works correctly, preserve per-library placement, and provide a small scope
boundary that can later support virtual libraries without implementing them now.

## Background

- Emby item identity and user state already use `MetadataItem.ID`; concrete
  source identity uses `Media.ID`.
- Current `/Items` queries start from `Media`, then collapse duplicate versions
  in memory. Some Series, mixed-movie, and Latest paths compensate by reading up
  to 50,000 or 500 rows before logical pagination
  (`internal/service/emby_items_list.go:14`,
  `internal/service/emby_items_list.go:256`,
  `internal/service/emby_movie_items.go:37`, and
  `internal/service/emby_items_detail.go:61`).
- One metadata work may have multiple Media versions and may legitimately be
  present in more than one physical library.
- Catalog metadata persisted while browsing the Web UI may have no Media. Such
  metadata is retained permanently but is not a member of a normal Emby library.
- `mediaVersionSiblings` currently loads sibling versions with
  `IncludeNSFW: true` and no user-library visibility filter
  (`internal/service/emby_media_sources.go:115`). When one work spans visible
  and hidden libraries, item detail or PlaybackInfo can expose hidden versions.

## Requirements

### R1. Logical work queries

- Emby library item lists must count, sort, and paginate logical
  `MetadataItem` rows, not physical `Media` rows.
- Multiple Media versions of the same metadata in one library must consume one
  result slot and contribute one to `TotalRecordCount`.
- Count and page queries must apply the same type, library, user visibility,
  NSFW, and search filters.
- Stable ordering must include a deterministic metadata-ID tie-breaker.
- Sorting by media creation time must aggregate only Media rows allowed by the
  current scope.

### R2. Physical library membership

- Membership of a metadata work in a physical library is derived from at least
  one valid, user-visible `Media{LibraryID, MetadataID}` relation.
- The same metadata may appear once in each of multiple physical libraries
  containing its Media.
- Metadata without a valid Media relation must not appear in a normal physical
  Emby library.
- Do not add `MetadataItem.LibraryID`, `MetadataItem.IsInLibrary`, a generic
  library-metadata join table, or a duplicated `in_library` flag.

### R3. Hierarchy and mixed libraries

- Movie, Series, Season, and Episode IDs and parent-child IDs must remain real
  metadata IDs.
- A Series belongs to a physical scope when a visible Episode Media under its
  hierarchy belongs to that scope; the same rule applies when resolving Seasons.
- Series roots and child lists must paginate logical metadata before loading
  playable versions.
- Existing mixed-movie-library behavior must remain: episodic paths aggregate
  to Series while ordinary movie paths remain Movie items.

### R4. Detail and playback versions

- After selecting a metadata page, payload construction must batch-load only
  Media versions visible to the current user.
- Item detail, Resume items, Latest items, and PlaybackInfo must never expose a
  source from a hidden/disallowed library or disallowed NSFW content.
- Detail and PlaybackInfo must continue to enumerate all visible versions and
  preserve concrete `Media.ID` source selection, tracks, paths, and existing
  playback contracts.

### R5. Global projections

- Global search, favorites, continue-watching, and SearchHints must return one
  item per metadata work even when visible Media exists in multiple libraries.
- A library-scoped response must set `ParentId` to the requested container;
  global projections must not invent a physical-library owner for shared
  metadata.
- `/Items`, `/Items/Counts`, and SearchHints totals must agree with the payload
  after equivalent filters.

### R6. Future virtual-library boundary

- The service/repository query boundary must represent a library scope without
  assuming every scope is a physical `LibraryID`.
- This task implements only physical scopes and the global visible scope.
- Future rule libraries may supply metadata predicates; future fixed/ranked
  libraries may supply an ordered metadata membership set without changing
  Emby item identity or Media playback selection.

## Acceptance Criteria

- [x] In a library containing several Media versions of one movie, `/Items`
  returns one movie, consumes one page slot, and reports one logical item.
- [x] If the same movie has Media in two libraries, each library lists it once;
  a global search lists it once.
- [x] Web-catalog metadata with no Media is absent from all ordinary physical
  libraries.
- [x] Series root, Season, and Episode queries preserve metadata IDs and parent
  IDs, and large numbers of Episode versions cannot displace logical Series
  from a requested page.
- [x] Latest and mixed-movie-library results remain complete without depending
  on fixed 500/50,000-row Media prefetch limits.
- [x] A user who cannot access one sibling Media's library never receives that
  sibling in Items, item detail, Resume, Latest, or PlaybackInfo MediaSources.
- [x] A user with access to multiple sibling versions receives all of them, and
  concrete MediaSource selection and track behavior remain unchanged.
- [x] `/Items`, `/Items/Counts`, SearchHints, and their totals agree under the
  same library, type, search, NSFW, and user filters.
- [x] Existing focused Emby tests pass, followed by `go test ./...`,
  `go vet ./...`, and `git diff --check`.

## Out of Scope

- Virtual-library database tables, CRUD APIs, rule editor, ranking scheduler,
  recommendation engine, or UI.
- Placeholder Emby items for ranked works that do not have playable Media.
- A `virtual_library_members` table before an ordered fixed-membership product
  requirement exists.
- Artwork or people-image storage changes; those belong to the existing
  `08-07-persist-people-images` task.
- Historical data migration or compatibility dual-write logic; this project is
  still using a fresh-install schema contract.

## Technical Constraints

- The implementation must work with both PostgreSQL and the current SQLite test
  database; do not rely on PostgreSQL-only `DISTINCT ON` behavior.
- Page queries must avoid N+1 reads: select a page of metadata IDs, then batch
  load visible `MediaView` rows.
- Existing uncommitted work in the repository must be preserved and not
  reformatted or reverted.
- No blocking product or scope questions remain.
