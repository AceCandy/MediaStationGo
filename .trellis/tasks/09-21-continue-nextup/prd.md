# 继续观看跨季与红果合集下一集

## Goal

网页继续观看与 Emby NextUp 共用普通、NFO、红果 Group 的后续分集查询

## Requirements

- Web continue watching combines resumable items and the next playable unplayed episode, one per series or official HongGuo album.
- Emby NextUp uses the same selection, excluding movies and groups with an active resumable episode. Resume/IsResumable and full history remain unchanged.
- Support canonical, NFO and HongGuo identities, visibility, version selection and cross-season/work ordering without persisting synthetic history.

## Acceptance Criteria

- [x] Completed season advances to the next available unplayed episode; entirely watched or untouched works produce no recommendation.
- [x] HongGuo albums preserve source identities, including equal season numbers ordered by source ID.
- [x] Incomplete progress wins; batch completion uses the furthest completed coordinate, independent of write timestamps.
- [x] Permissions apply before selection/count/pagination; mixed-source totals and stable pages are correct.
- [x] PostgreSQL regression and query-plan checks pass; Web lint/build and Emby catalog synchronization pass.

## Notes

- User approved both surfaces after the shared-selection proposal. No new persistent progress, background task, dependency or speculative cache.
