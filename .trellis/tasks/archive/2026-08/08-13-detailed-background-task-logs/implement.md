# 实施计划

1. 补充人物任务明细
   - 人物翻译返回并上报每组来源与“原文 -> 译文”结果、无效结果。
   - 人物补齐补足成功与跳过明细。
   - 验证：扩展 `people_translation_worker_test.go`、`people_backfill_test.go`。

2. 补充刮削任务明细
   - 已入库媒体刮削输出对象、状态、TMDB 标识和错误。
   - 发现目录刮削输出 job 身份、标题、成功或安全化失败信息。
   - 验证：新增或扩展 service 层聚焦测试。

3. 补充本地维护任务明细
   - 全库扫描按库输出结果或错误。
   - 回收站清理输出截止条件和删除数量。
   - 点验整理、轨道回填继续使用现有有界明细。
   - 验证：扩展 scheduler 和既有 task detail 测试。

4. 独立质量复核
   - 检查每个稳定任务定义对应的 `StartTriggered/Update/Finish` 路径。
   - 检查日志是否泄露密钥、请求正文或原始响应，是否重复写入累积明细。
   - 运行受影响包的聚焦 Go 测试；不运行全量编译。

## Risky Files And Rollback

- `internal/service/people_translation_worker.go` 的返回值变化会影响已有测试调用，需同步所有调用方。
- `internal/service/catalog_hydration.go` 的错误日志必须继续使用 `sanitizeCatalogError`。
- 所有改动均为任务可观测性附加信息；若出现日志量或隐私问题，可逐执行器撤销 `Details`，不影响业务状态。
