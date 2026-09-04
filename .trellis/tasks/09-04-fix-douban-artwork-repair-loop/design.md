# 豆瓣海报修复检查点设计

## Boundary

问题位于豆瓣候选的业务状态与修复任务之间：现有宽度阈值能发现待升级候选，却不能表达“这个大图 URL 已经尝试并得到可接受终态”。任务历史仅用于观测，不能承担该状态。

## Data Contract

在 `MetadataArtworkCandidate` 增加 text 字段 `RepairCheckedURL`，数据库列为 `repair_checked_url`，空字符串表示尚未检查。它记录已得到可接受终态的官方修复 URL：

- 下载成功：候选资产、`source_url` 与 `repair_checked_url` 在同一次 CAS 更新中写入。
- 最终官方源精确 404 且旧文件可用：保留资产和 `source_url`，仅通过候选快照 CAS 写入 `repair_checked_url`。
- 临时失败或本地文件缺失：不写入检查点。

不需要状态枚举。是否跳过由两个事实共同决定：本地文件可用，且当前派生 URL 等于 `repair_checked_url`。来源变化后比较自然失配并重新尝试。

## Flow

1. 查询豆瓣候选时同时投影 `repair_checked_url`。
2. 检查本地文件并派生稳定的官方大图 URL。
3. 文件可用且派生 URL 已检查：静默跳过。
4. 否则沿用 `ArtworkStore -> ImageProxy` 下载链路。
5. 成功时使用现有候选 CAS，同时保存检查点。
6. `isRemoteImageHTTPStatus(err, 404)` 且旧文件可用时，使用相同快照身份写检查点；CAS 失败按并发跳过处理。
7. 其他错误保持现有失败与后续重试行为。

## Compatibility and Migration

`MetadataArtworkCandidate` 只有一个映射模型，并已注册到 `model.AllModels()`；新增列由现有 PostgreSQL `AutoMigrate` 创建，无需历史数据回填。空值表示从未完成该 URL 的检查，旧部署升级后会各尝试一次。

回滚代码后新增列不会影响旧版本；不需要删除列。检查点不改变图片来源、缓存键或对外 API。

## Trade-offs

- 选择一列 URL 检查点，而不是状态枚举和时间戳：足以区分 URL 是否变化，避免额外状态机。
- 404 仅在旧图片可用时视为“当前最佳”；缺图仍失败，避免把不可展示状态静默固化。
- 已确认 404 的同一 URL 不做周期探活；只有上游元数据给出不同 URL 时重试。若未来需要定期探活，应另行引入明确过期策略。

## Validation

聚焦覆盖候选投影与 CAS、成功后的第二轮跳过、可用小图下的官方 404、缺图 404、临时错误和 URL 变化。数据库集成测试仅在 `MEDIASTATION_TEST_POSTGRES_DSN` 已配置时执行。
