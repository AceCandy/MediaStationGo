# Implementation Plan

1. [x] 将人物补齐改为全局缺失对象扫描，复用现有串行 worker/唤醒机制，并保留旧入口兼容。
   - Verify: service tests cover global selection and no-library behavior.
2. [x] 为人物翻译 pass 接入现有任务跟踪器，补充指标并确保空 pass 不留记录。
   - Verify: service tests cover pending, empty and failure paths.
3. [x] 在任务中心增加全局人物补齐触发按钮，不加载媒体库选择。
   - Verify: lint/build and focused UI behavior checks.
4. [x] 核对单对象 metadata 重刮削的人物同步链路并补齐缺口。
   - Verify: focused service/handler tests.
5. [x] 独立复核后台批次审计结论和跨层数据流，运行完整质量门禁。
   - Verify: relevant Go tests, `go vet ./...`, `npm run lint`, `npm run build`, `git diff --check`.

## Risk And Rollback Points

- Worker retry must not leave a task running after a failed pass.
- Task creation failure must stop the pass so work is not invisible.
- Existing library-detail backfill action must remain unchanged.
