# 实施计划

1. 调整 worker 生命周期和锁语义
   - 增加固定 3 worker 常量与媒体唤醒通道。
   - 拆分媒体 worker 与发现目录 worker。
   - catalog claim 前检查 `pending`/`running` 媒体，媒体完成后触发重新判断。
   - 自动媒体改用 `RLock`，其它现有入口继续使用 `Lock`。
   - 验证：不同组最多 3 路并发；同组 claim 不重复；媒体未清空时 catalog 不启动；worker 可随 context 退出并被 WaitGroup 等待。

2. 去除已知 TMDB ID 的重复详情请求
   - 为完整 TMDB 匹配增加内部标记。
   - 仅对未加载扩展字段的匹配调用 `GetDetails`。
   - 验证：movie/TV 已知 ID 各一次详情请求；搜索匹配仍补全扩展字段。

3. 增加单条分段耗时日志
   - 在现有候选、lookup、persist、artwork、extended details 边界累计耗时。
   - 自动媒体组完成时输出聚合结构化日志。
   - 验证：六类 duration 字段齐全，不含 URL、key、path。

4. 针对性验证与独立复核
   - 运行相关 service 测试。
   - 在环境支持时对并发测试运行 `go test -race`。
   - 使用 `trellis-check` 独立复核范围、锁语义、测试覆盖和无关 diff。

## 预计修改范围

- `internal/service/scraper_service.go`：worker 通道、固定数量、读写锁和 timing 类型所有权。
- `internal/service/catalog_hydration.go`：启动、等待、唤醒、媒体/catalog 循环拆分与入库优先检查。
- `internal/service/scrape_worker.go`：自动媒体读锁、总耗时日志。
- `internal/service/scraper.go`：lookup/persist/extended details 计时和重复详情跳过。
- `internal/service/scraper_metadata_persistence.go`：artwork 阶段计时。
- `internal/service/tmdb_types.go`、`tmdb_match.go`：完整详情标记。
- 对应 `_test.go`：请求次数、并发 claim/执行和日志字段回归测试。

## 明确不做

- 不增加第三方依赖。
- 不新增配置项、缓存层、队列或数据库迁移。
- 不改变图片同步持久化和 episode details 行为。

## 建议验证命令

```bash
go test ./internal/service -run 'Test.*(ScrapeWorker|TMDb|ExtendedMetadata|Timing)' -count=1
go test -race ./internal/service -run 'Test.*ScrapeWorker' -count=1
```

若测试命名与现有基线不匹配，实施后使用 `go test ./internal/service -list` 精确收敛过滤表达式，不运行全量仓库编译。
