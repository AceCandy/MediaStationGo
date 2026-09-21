# Emby 首页 Resume 合并下一集推荐

## Goal

复用跨来源续播候选查询，让 Emby Resume 返回断点或下一集，并保持版本选择、分组去重和准确分页。

## Requirements

- The client requests `/emby/Users/:userId/Items/Resume`; return either a resume
  or the next visible unplayed episode per series/official HongGuo album.
- Reuse HistoryRepository.Continuations and existing batch payload/version
  selection. Share paging and response assembly with NextUp.
- Keep Web eligibility and IsResumable filtering unchanged. No production writes.

## Acceptance Criteria

- [x] All Resume aliases return S2E1 after S1 completion for canonical/NFO/HongGuo.
- [x] Replay takes precedence; version/part selection and visibility are retained.
- [x] Group before paging; exact totals even for empty end pages.
- [x] Isolated PostgreSQL regression, Web checks and independent review pass.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
