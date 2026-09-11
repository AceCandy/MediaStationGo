# 实施与验证

1. 增加季级租约与 200 条分页，固定截止计数；保留逐目标提交校验与结果。
2. 接入三路季 worker、季内成功/失败共享、统一续租；入库登记未收录元数据问题。
3. 独立审查 diff，运行隔离 PostgreSQL 测试和相关 Go 检查；只读 EXPLAIN 验证查询开销。
4. 更新执行契约，记录验证与未上线限制。所有临时验证服务和文件结束时清理。

## 验证结果

- 独立审查与主线程复核领取、续租、提交、取消和错误分类。补齐请求身份即时核对，避免仅依赖异步归并清除旧问题。
- 隔离 PostgreSQL 17：`go test -race ./internal/repository ./internal/service -run 'TestTMDbRecheck|TestTMDbSeasonBatch|TestTMDbMetadataRecheck|TestSeriesInventory' -count=1 -timeout=120s` 通过，无数据库测试跳过；入库/刮削相关回归及 Go vet 也通过。
- 36 季/72 集乱序队列恰好请求 36 次、全部核对且剩余为零。HTTP 500 和超时各只请求一次并将该季三个目标分别重排；取消保留零失败次数并释放未完成目标。
- 两个独立连接竞争同季只能一个获胜；205 个目标分成 200+5 页；续租覆盖整批，季租约过期时旧 token 即使目标仍有效也不能写回，新执行者能恢复。
- 30,000 条记录的通用预编译计划验证季分页使用目标主键探测、全局季定位使用到期索引。这里只验证隔离测试库，不将上一轮逐集查询的生产 EXPLAIN 当作本轮季级查询证据。
- 未收录目标保留文件及占位关联、重复入库不延长冷却、活动租约不被抢占、本地资料齐全不擦除问题、上游恢复后清除问题；有效空清单和缺失/畸形清单分别测试。
- Web lint、构建、`node scripts/check-task-log.mjs`、`node scripts/check-recheck-dialog.mjs` 与 `git diff --check` 均通过。
- 新增季租约表及目标 token 部分索引，由标准迁移创建。未进行生产升级、重启、实际 TMDb 吞吐对比或浏览器视觉验收；上线迁移可能短暂阻塞索引涉及的写入。临时测试容器及其测试库已关闭移除，本轮临时日志已删除。
- 用户已确认提交，工作提交为 `5d55cd2`，未推送。

## 已确认提交计划

一条工作提交：`fix: 按季调度元数据复查并标记未收录剧集`。

范围仅含本任务下列代码、测试和规范；没有未知来源改动。任务目录在工作提交之后随归档流程处理，不推送远端。

- `.trellis/spec/backend/background-task-execution.md`
- `internal/model/model.go`
- `internal/model/tmdb_recheck.go`
- `internal/repository/tmdb_recheck_queue.go`
- `internal/repository/tmdb_recheck_queue_test.go`
- `internal/repository/tmdb_recheck_inventory.go`
- `internal/repository/tmdb_recheck_inventory_test.go`
- `internal/repository/tmdb_recheck_pass_test.go`
- `internal/repository/tmdb_recheck_season.go`
- `internal/repository/tmdb_recheck_season_test.go`
- `internal/service/episode_metadata_recheck_test.go`
- `internal/service/scraper_series_ingest.go`
- `internal/service/scraper_series_ingest_test.go`
- `internal/service/scraper_test_helpers_test.go`
- `internal/service/tmdb_recheck_not_found_test.go`
- `internal/service/tmdb_recheck_queue.go`
- `internal/service/tmdb_recheck_season.go`
- `internal/service/tmdb_recheck_pass_test.go`
- `internal/service/tmdb_season_batch.go`
- `internal/service/tmdb_season_batch_test.go`
- `web/scripts/check-recheck-dialog.mjs`
- `web/scripts/check-task-log.mjs`
- `web/src/pages/TMDbRecheckPanel.tsx`
- `web/src/pages/TasksPage.tsx`
