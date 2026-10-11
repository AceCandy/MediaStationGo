# 豆瓣资料中心本地海报与清理

## Goal

仅保留电影电视剧筛选，复用本地图片资产展示独立豆瓣海报，并精确清理三条无效历史快照

## Requirements

- Web filters expose only all, Movie and Series (电视剧); legacy kind URLs normalize to all.
- Lists and detail dialogs display posters from authenticated same-origin image URLs.
- Posters persist in the existing content-addressed artwork store with independent Douban subject references, including subjects with no local media.
- On-demand images reuse a valid local original first, otherwise download through existing image rules. Downloads never call the subject API or mutate media artwork selections.
- Poster failures preserve existing images and all subject freshness/rating/retry fields. Stale downloads cannot overwrite newer subject data.
- Remove only the three explicitly approved legacy snapshots containing only an unusable r field; preserve media and all other metadata.

## Acceptance Criteria

- [x] Filters, mobile/desktop posters and detail posters pass responsive browser checks.
- [x] Local persistence, reuse, file repair, source changes, failure fallback and concurrent CAS pass isolated PostgreSQL regressions.
- [x] Admin auth, GET/HEAD, variants and failure cache headers pass tests.
- [x] Existing Douban/artwork regressions, Web lint/build and diff checks pass.
- [x] The user executed the exact guarded cleanup; all three targets and all r-only Douban snapshots are absent. Their three metadata items and 4/1/1 media references remain.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
- DBX initially rejected cleanup because high-risk SQL was disabled. The user then executed the provided guarded DELETE. Subsequent read-only verification confirmed zero remaining targets/r-only Douban snapshots, all three metadata items preserved and media references unchanged at 4/1/1.
