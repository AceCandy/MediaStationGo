# TMDb 集信息补全与复查实施计划

## 1. 数据状态与查询

- 在 `MetadataItem` 和 schema 测试中加入 nullable `tmdb_episode_checked_at`。
- metadata graph merge 保留较新的 Episode 检查时间。
- 在适当 Repository 增加按 ID 分页的候选查询：只取有 media、字段缺失、冷却到期且具备 Series TMDb identity 的 Episode。
- Repository 测试覆盖：无 media、字段完整、3 天冷却、生成标题、缺 still、重复 media 版本和分页。

## 2. Episode 同步

- 将现有 `fetchAndSaveTMDbEpisodeDetails` 整理为可报告 provider 成功、实际更新字段和失败的共享内部同步逻辑。
- 成功后写 checkpoint；任一持久化失败不写 checkpoint。
- 复用现有标题生成判断、credits 替换和 ArtworkStore 并发保护。
- Service 测试覆盖覆盖更新、仍缺字段冷却、失败重试、图片保存和日志静默/详情。

## 3. 任务与调度

- 增加 `runTMDbEpisodeMetadataRecheck`，单次从 ID 起点分批遍历到空页，不持久化 cursor。
- 注册默认关闭、24 小时的 Scheduler job、settings、definition、手动 action 和任务日志映射。
- 从 TMDb 无图复查候选及类型处理中移除 Episode；保留 Episode 本地化修复。
- 更新 scheduler、task definition、设置清理和图片任务测试。

## 4. 规范与验证

- 更新后台任务与共享 metadata 规范。
- 运行 `gofmt`。
- 运行 Episode/TMDb artwork/scheduler/task definition 相关 Go 测试。
- 运行 repository PostgreSQL 测试（需要 `MEDIASTATION_TEST_POSTGRES_DSN`）；未配置时明确记录跳过。
- 运行 `git diff --check` 并独立复核查询边界、checkpoint 写入顺序和 Episode 从无图任务移除后的覆盖关系。

## 回滚点

- definition/scheduler 可独立移除。
- nullable checkpoint 列可保留，不要求破坏性回滚。
- Episode 重新加入旧无图复查只需恢复候选 kind；本地 still selection 和 recheck 状态不删除。
