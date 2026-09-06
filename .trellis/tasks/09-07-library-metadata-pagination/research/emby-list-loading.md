# Emby list loading audit

Source inspection only. No endpoint timings or query plans were collected.

## Entry and branches

`EmbyService.Items` in `internal/service/emby_compat.go:123` routes ordinary
file lists to `mediaItems`, library Series lists to `seriesItemsForLibrary`,
Series/Season children through `findSeriesGroup`/`findSeasonGroup`, and mixed
movie-library browsing to `movieLibraryItems`.

## Findings

1. Ordinary movies already count/group/page metadata IDs in SQL via
   `metadataPage`. File versions and payload relations are loaded only for the
   selected metadata page. This is not Web's whole-library loading pattern.
2. Ordinary Series lists also page metadata IDs in SQL via
   `seriesMetadataPage`, but then load every file version of every episode of
   each selected Series. Work is page-scoped but not bounded by card count.
3. Mixed movie-library browsing passes zero limits to both paging helpers,
   constructs all Movie/Series payloads, sorts them, and only then calls
   `pageSlice`. This reproduces whole-library loading. The branch requires a
   non-recursive movie-library browse with no explicit item types and detected
   episodic content.
4. Series/Season children first materialize all visible files in that parent,
   then collapse versions, sort and slice. This is parent-scoped, not global.
5. Recent Series reuse `seriesMetadataPage`; search Series batch-load current
   page episodes through `FindByLogicalMetadataIDs`. Both retain the same
   per-Series expansion cost.

## Change boundaries

- Preserve Emby metadata IDs, MediaSource file IDs, total counts, field
  selection, ordering, user state and visibility contracts.
- A lightweight Series list projection must supply child/recursive counts and
  user-state aggregates without loading complete episode views.
- Fixing mixed browsing requires selecting the combined Movie/Series logical
  page before constructing payloads; independently paging both kinds is not a
  valid combined-page implementation.
- Compare the administrator API catalog if any player-visible contract changes.
- 用户已批准 Emby 与 Web 同时实现；普通电影原有 metadata 分页保持不变。
