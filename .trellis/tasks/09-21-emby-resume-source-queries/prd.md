# 优化混合来源继续观看查询

## Goal

传统资料、红果与 NFO 分别筛选用户继续观看候选，归组限量后统一分页并保持权限和总数

## Requirements

- Optimize mixed-catalog Emby resume reads without changing visibility, progress eligibility, logical grouping, ordering, counts or payload fields.
- Each source filters the current user's states and visible files, groups logical works before limiting candidates, and contributes its exact count.
- Preserve canonical/NFO series deduplication and HongGuo official album grouping; never merge identities across sources.
- Scope is global IsResumable and the NFO-enabled ResumeItems path. Existing single-source resume thresholds and ordinary browse queries remain unchanged.

## Acceptance Criteria

- [x] PostgreSQL regressions cover interleaved sources, duplicate versions/episodes, album seasons, offsets, ties, empty pages and exact totals.
- [x] User/library/NSFW filtering occurs before grouping; completed, zero-progress and unavailable items are excluded.
- [x] Candidate selection does not expand all media into container nodes or load artwork/probe data.
- [x] Existing resume and NFO/HongGuo integration tests pass against an isolated PostgreSQL database.

## Notes

- User approved source-specific candidate queries and bounded merging after discussion. No schema/index changes, caching, pool tuning or unrelated browse optimization.
