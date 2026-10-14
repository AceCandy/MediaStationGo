# 验证记录

## 已完成

- 运行时已无 `HongGuoDiscovery` 模型或 `hongguo_discoveries` 查询；作品摘要与详情均在 `hongguo_works`。
- 独立只读复核两轮：身份保持、摘要优先级、并发冲突更新、任务资格和迁移 SQL。合集谓词虽未重复检查完整度，但唯一关系写入 SaveAlbum 已拒绝摘要，摘要写入与迁移均不会产生合集关系，因此未添加不可达状态的重复防御。
- 使用本地隔离 PostgreSQL 15。未访问或修改用户数据库，未重启用户服务。
- 为避开并行任务影响，用 HEAD 导出的独立副本，仅应用本任务红果文件：仓储、handler、database 的 `TestHongGuo|TestCurrentSchemaPreservesDataAndConstraints` 通过（排除以下已验证基线失败的查询计划用例）。
- 服务层定向覆盖 discovery、rank、refresh、wakeup、catchup、图片生命周期、补充下载、下架清理和下载重试；以下两项旧断言除外，其余通过。
- 新增真实数据库测试：摘要不进入详情/普通刷新/合集队列；失败补齐事务回滚；下载须先补齐；迁移保留详情 UUID/分集/海报、摘要原位升级、重复执行、遇依赖整笔回滚、旧表不再重建。
- `git diff --check` 通过。

## 已隔离的非本任务失败

1. `TestHongGuoSearchBoundsFileWork/revalidation`：PostgreSQL 15 的 GENERIC_PLAN 检查报 `there is no parameter $1`；纯 HEAD 同样失败。
2. `TestHongGuoDownloadQueuePlacementCancelAndRecovery`：`leaked video_id` 断言失败；纯 HEAD 同样失败（未改此测试或下载响应字段）。
3. `TestHongGuoImportTaskAndIsolation`：预期 5 个任务定义，现有 6 个；纯 HEAD 同样失败。
4. 合并工作区重复 AutoMigrate 报 media.metadata_id 被触发器引用而不能改类型；HEAD+本任务独立副本不出现，未改写其它会话的迁移代码。
5. 初始扩展运行出现弹幕 shutdown 取消时序失败；纯 HEAD 单独复跑未复现。广域下载源优先级用例等待较久，已停止该次扩展运行，改为与本任务相关的定向回归。没有宣称全套测试通过。

## 部署与未验证

未执行生产迁移、未访问真实红果上游、未做浏览器回归；前端代码和响应字段契约未改。

部署顺序：停止服务并备份数据库 → 执行 `scripts/migrations/20261013_hongguo_single_catalog.sql`（psql 应启用 ON_ERROR_STOP）→ 确认成功后启动新版本。直接重启不会自动合并旧摘要；SQL 删除旧表后旧版本不能继续使用。需回滚时恢复备份及旧版本。

本任务不提交或归档其它会话的未提交改动。

最终复验：当前合并工作区的迁移、摘要资格、检查点、App 补录、搜索和下载资格定向测试均通过。
