# 优化 Emby 剧集列表首次加载

## Goal

测量并优化剧集列表查询，保持分页、统计与用户可见性契约，验证首次加载改善。

## Requirements

- 优化 Yamby 请求的剧集列表首次加载，不依赖新增结果缓存。
- 保持列表排序、分页、逻辑总数、季集统计、收藏和用户可见性。
- 不改变单剧详情、播放、图片链路和已有刮削改动。
- 同时优化 Web 剧集列表分页查询，保留代表文件选择、排序、直接关联整剧/季/分集与缺失元数据筛选规则。

## Acceptance Criteria

- [x] 在同一数据库、同一请求参数下记录优化前后分段耗时，列表显著快于约 1.6 秒基线。
- [x] PostgreSQL 回归覆盖多版本去重、跨库可见性、排序、空页与摘要后详情。
- [x] 独立复核通过；记录实测范围和未部署边界。
- [x] Web 同库首次列表服务提速，响应一致，相关 PostgreSQL 回归通过。

## Notes

### 用户追加：剧集已观看兼容行为

- 整剧与季的标记/取消应联动当前用户可见且有文件的单集，季只影响本季。
- 剧/季按单集汇总已观看状态，不让旧父级历史覆盖单集，不预标记未来入库的集。
- 多版本按作品去重，写入原子化，不改其他用户或真实播放事件。
- 验证详情/列表一致、零时长、季零、用户/隐藏库隔离、事务回滚及列表执行计划。
- 不部署、不重启现有服务，不修改刮削任务。

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
