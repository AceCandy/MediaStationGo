# 播放统计明细与热榜实施计划

1. 扩展 repository 统计 DTO 与查询
   - 提取现有筛选查询的最小复用点。
   - 增加稳定分页明细查询和电影/电视剧季级 Top 10 聚合。
   - 验证：PostgreSQL 测试覆盖筛选一致性、倒序分页、电影版本合并、`episode -> season -> series` 聚合、缺父季退化分组、并列稳定排序、部分日/周与筛选范围交集及已删除媒体文件事件。

2. 扩展 handler 参数与响应
   - 校验 `page/page_size/rank_grain/rank_date`，统一日期解析与 SQL 使用的有效时区，保留现有参数与响应字段兼容性。
   - 验证：handler 测试覆盖默认值、分页 1..100 边界/偏移溢出、非法周期、越界日期、具名时区/UTC 回退和非管理员拒绝。

3. 扩展 Web API 类型和统计页
   - 复用现有筛选区、面板样式、日期输入和分页模式。
   - 增加每日/每周 Top 10，以及桌面表格/移动卡片明细。
   - 筛选变化时重置页码，已删除媒体不生成链接。
   - 验证：人工检查加载/空/错误状态、分页边界、日周切换和筛选同步。

4. 独立复核与质量门
   - 对照 PRD 检查所有返回字段、筛选和聚合口径。
   - 运行 `go test ./internal/repository ./internal/handler`；若未配置 `MEDIASTATION_TEST_POSTGRES_DSN`，明确记录数据库集成测试未执行。
   - 在 `web/` 运行 `npm run lint`、`npm run build`，并在仓库根运行 `git diff --check`。
   - 在 390x844、768x1024、1440x900 及受影响断点检查深浅主题、横向溢出、键盘焦点和控制尺寸。

## Risk and Rollback Points

- 季级聚合的父级 JOIN 是主要正确性风险，先以 repository 测试锁定，再接入 UI。
- 当前工作树有大量用户改动；实施时只修改统计相关文件并逐文件核对 diff。
- 无迁移和新依赖；发现问题可按 repository、handler、Web 三层分别回滚。
