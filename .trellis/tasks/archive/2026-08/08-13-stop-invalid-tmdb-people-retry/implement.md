# Implementation Plan

1. 为 TMDB HTTP 错误保留状态码，并提供最小的状态判断。
   - Verify: 404 可识别，429/5xx 仍是普通错误。
2. 为 metadata repository 增加事务化的失效 TMDB 标识清理与关联 media 重置操作。
   - Verify: 仅删除指定 metadata/kind/id；只清除相同 ID；关联处理单元进入 pending。
3. 人物补齐候选约束 metadata source 和 entity kind，并在 TMDB 来源对象的 credits 404 时调用清理、提交后唤醒 scrape worker。
   - Verify: 豆瓣/本地来源不入选且不删除交叉标识；404 后不再入选；临时错误仍入选；无关联 media 不误改其他对象。
4. 调整任务结果测试，确认部分失败显示异常指标且其他候选继续处理。
   - Verify: focused service/repository tests and `git diff --check`.
5. 独立复核数据范围与回滚行为。
   - Verify: no unrelated schema/UI/provider-priority changes.
