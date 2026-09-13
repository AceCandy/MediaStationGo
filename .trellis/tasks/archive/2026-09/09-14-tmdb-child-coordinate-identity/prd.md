# 季集按坐标修复 TMDb 标识

## Goal

自动复查以整剧 TMDb ID 和季集坐标为权威，允许安全替换过期的季/集 TMDb ID，避免正确坐标因历史子级 ID 不一致而永久重试。

## Background

- TMDb 季接口使用 `series_id + season_number`，集接口使用 `series_id + season_number + episode_number`。
- 本地季、集 canonical 位置分别由父级与季号/集号唯一确定。
- 自动复查的 Episode 路径已按坐标请求并以返回 ID 替换旧 ID；Season 路径额外要求旧季 ID 与返回 ID 相等，造成行为不一致。
- 季/集 TMDb ID 仍被快照、目录补全、精确查询、合并和冲突检测使用，不能仅删除字段或唯一索引。

## Requirements

- 自动复查 Season 时，以唯一有效的整剧 TMDb ID 与本地 `season_num` 定位上游数据。
- TMDb 返回的 `season_number` 必须等于请求季号，返回季 ID 和原始 JSON 必须有效。
- 本地旧季 TMDb ID 缺失、无效或与返回 ID 不同时，不得仅因此拒绝结果；成功保存时原子替换为返回 ID 并更新快照。
- Episode 继续沿用现有坐标定位和返回 ID 替换行为。
- 保留 `(provider, entity_kind, external_id)` 唯一约束；若返回 ID 已属于另一 metadata，继续按现有冲突规则失败并回滚，不能任意抢占。
- 手动“按当前 TMDb 身份刷新”、catalog 合并、失效清理及其他 provider 流程保持不变。
- 不新增表、迁移、配置、后台任务或兼容分支。

## Acceptance Criteria

- [ ] Season 候选的旧 TMDb ID 与坐标请求返回 ID 不同时，自动复查成功并将 identifier/snapshot 更新为返回值。
- [ ] Season 候选的旧 TMDb ID 非数字时，自动复查仍能按坐标修复。
- [ ] TMDb 返回错误季号、无效 ID 或无效 JSON 时仍拒绝保存。
- [ ] 返回季 ID 与其他 metadata 的唯一标识冲突时事务回滚，不覆盖另一实体。
- [ ] 现有 Episode 自动复查和手动刷新身份校验不发生行为变化。
- [ ] 目标 Go 测试通过；真实 PostgreSQL 测试若因 DSN 缺失跳过，必须明确记录。

## Out of Scope

- 删除季/集 TMDb identifier 或放宽其数据库唯一约束。
- 自动合并已被其他 metadata 占用的返回 ID。
- 修改媒体文件、季号、集号或整剧匹配。
