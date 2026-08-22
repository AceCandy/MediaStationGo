# 技术设计

## 边界

本次只修改自动媒体调度、匹配结果标记和自动任务耗时观测。保留现有数据库 claim、metadata 唯一约束、图片持久化和任务状态语义。

## Worker 结构

`StartCatalogHydrationWorker` 仍是统一启动入口，但启动：

- 3 个 `runMediaScrapeWorker`，只调用 `processNextMediaScrape`；
- 1 个 `runCatalogHydrationWorker`，只处理发现目录 catalog job。

新增独立媒体唤醒通道，容量与固定 worker 数一致。`WakeScrapeWorker` 同时唤醒媒体 worker 和发现目录 worker，保持现有调用方不变。媒体 worker 启动后先主动检查队列，队列为空才等待唤醒。

发现目录 worker 在领取 catalog job 前查询是否仍有 `pending` 或 `running` 媒体；存在时等待媒体完成信号或短周期兜底检查。每个媒体组结束后唤醒发现目录 worker 重新判断。数据库状态仍是队列真相，唤醒通道只缩短等待，不承担持久状态。

## 锁语义

将 `scrapeRunMu` 从 `sync.Mutex` 改为 `sync.RWMutex`：

- 自动媒体组在 `processNextMediaScrape` 中持 `RLock`，允许最多三个不同组并发；
- 手动单项、整库、发现目录和人员补全保留现有 `Lock`，继续独占；
- 不修改 `claimNextPendingMediaGroup` 的事务、`running` 状态过滤或组级分配逻辑。

这样既让自动入库获得并发，也不放宽其它入口对共享 metadata 图的串行约束。

## TMDB 重复请求消除

在 provider-neutral `Match` 上增加一个仅内部使用的布尔标记，表示该 TMDB 匹配已经从完整详情响应加载扩展字段。`GetMovieMatch` 和 `GetTVMatch` 设置该标记；名称搜索结果不设置。

`applyProviderMatchWithOptions` 只在标记未设置时调用 `fetchAndSaveTMDbExtendedMetadata`。首次完整匹配已经通过 `metadataItemFromMatch` 保存同一批字段，因此不再发第二次请求。

## 耗时观测

自动任务创建一个仅本次调用使用的 timing collector，通过现有 `ScrapeOptions` 私有字段向下传递。候选生成、lookup、persist、artwork、extended details 在现有边界累计耗时，`processNextMediaScrape` 完成时输出单条结构化日志和 total。

日志只记录 duration、media ID、TMDB ID、结果状态等非敏感字段，不记录 URL、API key 和文件路径。

## 兼容性与回滚

- 无 API、schema 或配置迁移。
- 回滚 worker 拆分、`RWMutex` 和完整详情标记即可恢复原串行行为。
- 若数据库环境在 3 worker 下出现不可接受的锁竞争，可先将固定常量恢复为 1，不影响数据格式。

## 主要风险

- 三个不同本地媒体可能最终解析到同一 provider identifier；依赖现有 canonical upsert 事务和唯一约束收敛，必须增加回归测试。
- 发现目录写锁等待期间，Go `RWMutex` 会阻止新读锁进入；这是刻意保留的正确性优先策略，可能短暂降低媒体吞吐，但不会放宽共享 metadata 写入边界。
- 分段耗时会增加一条每媒体组 Info 日志；使用单条聚合日志控制噪声。
