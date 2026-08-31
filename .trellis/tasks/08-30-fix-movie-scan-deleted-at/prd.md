# 修复电影媒体库扫描 deleted_at 回归

## Goal

修复电影媒体库扫描后的季集纠正查询，使其与 `metadata_items` 的硬删除模型和实际 PostgreSQL 表结构一致，不再按媒体库路径累计无效 SQL 错误。

## Background

- `MetadataItem` 使用 `PermanentBase`，实际 `metadata_items` 表没有 `deleted_at` 列。
- `reconcileMovieLibraryEpisodes` 当前 JOIN 引用了 `mi.deleted_at`，PostgreSQL 返回 `column mi.deleted_at does not exist`。
- 该纠正步骤每条启用路径执行一次，因此有五条路径的媒体库稳定显示“错误 5”。
- 现有 `TestScanLibraryReconcilesDirtyMovieEpisodes` 已覆盖纠正行为，但未配置 `MEDIASTATION_TEST_POSTGRES_DSN` 时会被跳过。

## Requirements

- R1：删除季集纠正 JOIN 对不存在的 `metadata_items.deleted_at` 列的引用。
- R2：保留现有电影季集纠正、错误计数和其他扫描行为，不改变公共接口。
- R3：复用现有 PostgreSQL 回归测试，不新增重复测试或抽象。
- R4：复核生产代码中硬删除表的 `deleted_at` 运行时引用，确认本次修复后不存在同类已知悬空引用。

## Acceptance Criteria

- [ ] AC1：电影媒体库扫描不再因 `metadata_items.deleted_at` 不存在而增加错误数。
- [ ] AC2：`TestScanLibraryReconcilesDirtyMovieEpisodes` 在配置隔离 PostgreSQL 测试 DSN 时通过且不被跳过。
- [ ] AC3：针对八张 `PermanentBase` 硬删除表的生产运行时检索不再命中无效 `deleted_at` 条件；兼容迁移中的受保护引用除外。
- [ ] AC4：产品代码 diff 仅包含根因处的必要修改，并通过 `git diff --check`。

## Out of Scope

- 不修改全库扫描任务的错误明细展示。
- 不新增静态检查框架或 CI 配置。
- 不重构季集纠正流程，也不调整数据库迁移。
