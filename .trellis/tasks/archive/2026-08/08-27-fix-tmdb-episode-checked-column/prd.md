# 修复 TMDb 集复查列名迁移

## Goal

修复 TMDb 集信息补全/复查任务因 GORM 自动列名与手写 SQL 不一致而失败的问题，并在升级时保留旧列中的检查时间。

## Background

- `MetadataItem.TMDbEpisodeCheckedAt` 当前未显式声明数据库列名；GORM v1.25.7 将其映射为 `tm_db_episode_checked_at`。
- `ListTMDbEpisodeMetadataRecheckAfter` 和既有元数据契约使用 `tmdb_episode_checked_at`，导致 PostgreSQL 返回 `SQLSTATE 42703`。
- 应用启动时通过 `database.AutoMigrate` 更新 PostgreSQL schema，兼容修复必须走同一路径且可重复执行。

## Requirements

- `MetadataItem.TMDbEpisodeCheckedAt` 必须显式映射到 `metadata_items.tmdb_episode_checked_at`，与仓库查询和既有 spec 一致。
- 现有 `metadata_items.tm_db_episode_checked_at` 必须迁移到规范列；规范列已有值时不得被旧值覆盖。
- 成功迁移后移除错误拼写的旧列，避免两个列继续分叉。
- 迁移失败必须中止启动，不得静默忽略。
- 修改仅限列映射、兼容迁移和针对性回归测试；不改变 TMDb 复查业务规则、调度配置或 API。

## Acceptance Criteria

- [x] 新建数据库执行迁移后存在 `tmdb_episode_checked_at`，不存在 `tm_db_episode_checked_at`。
- [x] 从仅含旧列的数据库升级后，非空检查时间保留到规范列。
- [x] 规范列与旧列同时存在时，规范列已有值优先保留。
- [x] 连续执行两次 `database.AutoMigrate` 均成功，旧列不会恢复。
- [x] TMDb 集复查候选查询不再触发 `column episode.tmdb_episode_checked_at does not exist`。

## Out of Scope

- 手工修改用户数据库。
- 修改 Docker 发布、部署或任务调度流程。
- 清理与本列无关的 schema 或代码。

## Risks

- PostgreSQL 集成测试依赖 `MEDIASTATION_TEST_POSTGRES_DSN`；未配置时只能验证静态映射与编译，不能证明真实迁移行为。
