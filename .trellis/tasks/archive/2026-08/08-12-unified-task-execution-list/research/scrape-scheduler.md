# 统一刮削调度器研究

## 已核实现状

- Catalog worker 是独立 worker：`StartCatalogHydrationWorker` 恢复 running 任务后启动 goroutine（`internal/service/catalog_hydration.go:22-40`），主循环逐个 claim/process（`:80-112`）。Repository claim 使用事务、`FOR UPDATE SKIP LOCKED`，状态从 pending/retry 改为 running，并递增 attempts（`internal/repository/catalog_repository.go:78-99`）。因此多实例可并发 claim；单实例当前只有一个 catalog goroutine，但尚未与媒体库刮削共享 worker/互斥。
- Catalog 优先级不是“媒体库 scrape_status 待办优先”。`claimNextCatalogJob` 以 `catalogRootBurst=4` 在 root 与 seasons 之间配额调度（`internal/service/catalog_hydration.go:114-136`）；不存在查询媒体 `scrape_status` 的逻辑。
- 电视剧 Catalog job 明确按 season turn 切分：root 完成后 requeue 到 `stage=seasons`（`:197-202`、`:247-259`）；`hydrateCatalogSeasonTurn` 只查一个未完成 season，处理完后再次 requeue 同一 job（`:271-291`）。每个 season 内 episode 循环直到该 season 完成（`:309-364`）。这与“最小不可抢占单元为完整电视剧”冲突：当前 season turn 可在 season 间被另一任务插入，且失败/重启会从 season 级 checkpoint 继续。
- 媒体库刮削候选查询包含 NULL/空值/pending/error，手动 `RetryNoMatch` 时再含 no_match；按 media id 顺序（剧集整体排后）返回全部行（`internal/service/scraper_library.go:261-290`）。`EnrichLibraryDetailedWithOptions` 将行按 metadata 分组，然后逐组调用 `EnrichOneWithOptions`，组间串行（`:133-180`）。但该 API 一次处理整库，不能作为调度器的单电影/电视剧 claim；没有持久化 running 状态或 lease。
- “重新刮削”语义目前由调用方传 `RetryNoMatch=true`，查询会纳入 no_match（`scraper_library.go:267-272`）；匹配状态更新在 `internal/service/scraper.go:...` 的 `applyScrapeMediaTypeResets`/EnrichOne 流程。若统一队列要求重置业务对象状态，应在入队/claim 前对该媒体对象（同 metadata 组的所有 media）显式置 `scrape_status=pending`，清理 `scrape_error`，而非只重置 CatalogJob 状态。
- 重启恢复目前只恢复 CatalogJob 的 running->retry（`internal/repository/catalog_repository.go:71-76`）。媒体刮削没有持久化 claim 状态，因此进程中断后只能依赖仍为 pending/error 的行；若处理中已写 matched，重启不会自动重做。要满足“从业务状态继续”，应以媒体/metadata 的状态和 checkpoint 为幂等依据，避免依赖内存进度。

## 设计冲突与风险

1. **抢占粒度**：统一单 worker 若直接复用现有 Catalog stage，会在 root/season 任务之间切换，违反完整电影/电视剧不可抢占。应把 series catalog job 的 process 调整为一次完成 root、所有 season、所有 episode，再一次 Complete；失败时保留已写的 per-item checkpoints，retry 从第一个未完成 child 继续，但 worker 不在中途主动 requeue。
2. **优先级竞态**：媒体待办查询若只在 claim 前执行一次，处理期间新产生的 pending media 可能被 Catalog job 抢到。统一循环应每次选择任务时先原子 claim 一个媒体“业务对象组”，仅当无媒体待办时才 claim CatalogJob；否则无法保证待办优先。
3. **去重/并发**：`EnqueueCatalogJob` 依赖 `(provider,entity_kind,external_id)` 唯一冲突 DoNothing（`:57-68`），但媒体待办没有类似数据库 claim。单 worker可避免本进程重复，仍需数据库条件更新（pending->running 或 lease）防止手动 API/扫描器并行调用 `EnrichLibrary` 重复刮削同一 metadata。
4. **事务边界**：现有 Catalog claim 事务只包行锁和状态更新；网络请求及 metadata/artwork 多次写入不在同一事务。统一调度不应把长网络过程放进 DB 事务；使用短 claim 事务 + 幂等 checkpoint，完成时条件更新 job 状态，避免 worker 崩溃留下 completed 覆盖。
5. **重试状态**：当前 `RetryCatalogJob` 每次失败后退避；`RequeueCatalogJob` 将 attempts 清零并重置 stage（`:101-105`）。整部 series 任务若内部失败后直接 Retry，应保留 stage/root 信息和业务 checkpoints，不要重置为 root 导致重复 API 请求。

## 推荐符号级改动（最小范围）

- `internal/service/catalog_hydration.go`：将 `runCatalogHydrationWorker` 改为统一调度循环；新增 `claimNextScrapeUnit`（媒体 metadata 组优先，随后 CatalogJob），移除 `catalogRootBurst`/`claimNextCatalogJob` 的 root-streak 配额。`processCatalogJob` 对 series 调用整剧流程（可将 `hydrateCatalogSeasonTurn` 合并为 `hydrateCatalogSeries`），仅在完整 series 成功后 `CompleteCatalogJob`。
- `internal/repository/catalog_repository.go`：保留现有短事务 claim；新增按业务对象（metadata_id 或 media group）条件 claim/recover 方法，或至少在统一 worker 入口使用 `UPDATE ... WHERE scrape_status IN (...)` 实现 pending->running。Catalog recover 应保留可恢复 checkpoint，不把业务对象状态误标 completed。
- `internal/service/scraper_library.go`：抽取“单 metadata 组/单电影或电视剧”处理函数，供统一 worker 调用；现有 `EnrichLibraryDetailedWithOptions` 保持批量 API 兼容，仅改为循环调用抽取函数。查询/claim 必须返回一个完整 metadata 组，电视剧包含其所有 media，避免按 episode 抢占。
- 重新触发入口（手动刮削 handler/service，调用 `EnrichLibrary(..., true)` 的位置）：统一调用“重置业务对象状态”方法，将目标组 media 置 pending、清空错误，再唤醒统一 worker；Catalog job 若已 completed 也应按 identity 重置/重新入队，而不是只改 job status。
- 启动 wiring（搜索 `StartCatalogHydrationWorker` 调用点）：只启动统一 worker；关闭路径继续调用 `WaitCatalogHydrationWorker`，确保单 worker 串行。

## 尚未覆盖/需主代理确认

- 未定位到统一调度器现有任务模型或媒体 claim 表；当前代码中只有 CatalogHydrationJob 持久化队列。若任务 PRD 已定义新表/状态枚举，应优先复用其契约。
- `applyScrapeMediaTypeResets` 的完整调用链需在实现阶段核对，避免重置用户手工 metadata；本研究仅确认其位于 `internal/service/scraper.go` 并参与匹配更新。
