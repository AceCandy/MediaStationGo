# 实施与验证

1. 实现明确下架二次核对和事务清理；验证保护条件、租约失效、关联记录清理。
2. 实现完整分集变化比对；保护已下载文件，验证尾集减少、空位、重排和重新入队。
3. 使用隔离 PostgreSQL 运行下载相关 Go 回归和 race 检查，独立复核 diff。
4. 更新红果契约和任务记录，不提交、不部署、不接触生产数据。

## 验证结果

- 使用本次新建的本地隔离 PostgreSQL 数据库，所有数据库测试实际运行，未依赖默认跳过。
- `go test ./internal/hongguo ./internal/service ./internal/repository ./internal/handler -run 'Test(HongGuoDownload|DownloadApp|HongGuoSearchResults|HongGuoDownloadedBadge)' -count=1` 通过。
- `go test -race ./internal/service -run 'TestHongGuoDownload(UnavailableConfirmation|EpisodeReconciliation|CancelThenRetryRunningLease|DynamicConcurrencyAndShutdown|SupplementOrderAndConcurrent|ResolvesLatestEpisode)$' -count=1` 通过。
- 最终补充 `go test -race ./internal/service -run 'TestHongGuoDownload(ConcurrentRemoval|UnavailableConfirmation|EpisodeReconciliation)$' -count=1` 通过，涵盖相同集数重排、空位重复核对和并发清理。
- 独立复核后补齐相同集数下已知视频换位的识别。媒体保护分支在事务闭包内 `return nil` 会立即结束闭包并提交已完成的任务清理，保留 metadata；已用实际 PostgreSQL 用例核验。
- `git diff --check` 通过。没有前端改动，未运行浏览器/Web构建；未做真实源站下架、生产数据删除、部署或外部备份验收。
- 旧失败项需点击重试触发核对；已下载内容遇重新分集时保守提示人工确认，不自动覆盖。
