# 拆分 TMDb 图片本地化与无图复查

## Goal

将当前混合本地文件巡检、缺图发现和 catalog hydration 排队的“元数据图片补齐”替换为两个职责独立、可单独调度和观察的图片任务：

1. 已有 TMDb 远程图片链接但本地权威文件缺失时，优先从原链接恢复；仅在原链接明确返回 HTTP 404 时重新查询 TMDb。
2. 对 TMDb 曾明确返回无图的实体按图片类型每日复查；本次仍无图后 24 小时内不再命中。

两个任务只处理图片。首次 metadata 入库、首次图片刮削、文本元数据、人物和 provider identity 继续由现有入库刮削负责。

## Background

- 当前任务在 `internal/service/artwork_backfill.go:18` 先巡检本地 asset，再从 `internal/service/artwork_backfill.go:41` 查找缺图根并写入 catalog hydration job；这正是日志里“扫描 200 个缺图根，新增 200”的来源。
- 当前本地文件缺失会经 `internal/service/artwork_store.go:170` 调用 `internal/repository/artwork_repository.go:192` 删除 selection/candidate 并清空实体图片 checkpoint，随后才重新进入目录 hydration；没有“保留关系并先按旧链接恢复”的路径。
- `metadata_artworks.source_provider/source_url` 保存当前选择的来源和原始链接，`asset_id -> artwork_assets.storage_key` 指向本地权威文件（`internal/model/metadata.go:73`）；远程图片标准写入链已经由 `ArtworkStore` 提供（`internal/service/artwork_store.go:45`）。
- `metadata_artwork_candidates` 是独立的 provider 候选表（`internal/model/metadata.go:87`），不能把豆瓣候选或其他 provider 图片当成 TMDb 当前选择。
- `catalog_artwork_hydrated_at` 同时表示“所需图片已保存”和“provider 明确无图”（`internal/model/metadata.go:42`），只有实体级时间，不能表达逐图片类型的 24 小时复查冷却。
- ImageProxy 当前把所有 HTTP 4xx/5xx 折叠成普通字符串错误（`internal/service/image_proxy_remote_fetch.go:48`），业务层无法稳定判断只有 404 才刷新 TMDb 链接。
- 任务中心已具备独立 definition、Scheduler 手动/周期执行、TaskTracker 持久化摘要和按任务分日详细日志（`internal/service/task_definitions.go:53`、`internal/service/scheduler.go:106`、`internal/service/task_log.go:29`），无需新增任务框架。

## Requirements

### R1. TMDb 远程图片本地化修复

- 只扫描当前 `metadata_artworks` 中 `source_provider='tmdb'`、具有合法 HTTP(S) `source_url` 的 selection。
- 仅当 selection 指向的本地权威文件不存在时处理；文件正常时不得下载、替换或覆盖。
- 每张缺失图片先从已保存的旧链接下载，并通过现有 ArtworkStore 校验、哈希去重和原子文件写入恢复 selection。
- 只有旧链接明确返回 HTTP 404 时，才按该 metadata 的有效 TMDb identity 查询当前图片来源；超时、连接失败、403、429、其他 4xx 和 5xx 都只记为可重试失败。
- TMDb 重查只读取当前缺失图片类型的来源结果，不持久化标题、简介、评分、人物、identifier 或任何 catalog checkpoint。
- 404 后 TMDb 返回新图片时，以条件写入替换原 dangling TMDb selection；若处理期间 selection 已被手工、本地或其他来源更新，保留新选择并记录跳过。
- 404 后 TMDb 仍无对应图片时，保留当前数据库关系，记录该 metadata/图片类型的无图复查时间；第二任务最早在 24 小时后处理。
- 手工图片、本地 NFO、豆瓣候选及其他 provider 图片不进入任务，也不得因修复失败被替换为 TMDb 图片。
- 不得写入或消费 `catalog_hydration_jobs`，不得唤醒或复用“发现目录刮削” worker。

### R2. TMDb 无图复查

- 只扫描具有所需 TMDb identity，且满足以下任一已处理证据的 metadata：`catalog_artwork_hydrated_at IS NOT NULL`，或第一任务/本任务已经记录了逐类型 TMDb 无图状态。
- `catalog_artwork_hydrated_at IS NULL` 且没有逐类型无图状态的从未完成图片处理记录不进入任务；首次处理继续由现有入库刮削负责。
- 只扫描自身或下级 Episode 已有 `media` 资源的 metadata；没有媒体资源的 metadata 不进入无图复查任务。
- 图片类型按实体所有权处理：Movie/Series 为 poster、backdrop，Season 为 poster，Episode 为 still；不得使用父级图片补齐。
- 逐 metadata/图片类型以最后一次“TMDb 明确无图”时间冷却 24 小时；未到期时记录冷却跳过，到期后才请求 TMDb。
- 同一 metadata 有多个到期图片类型时复用一次对应的 TMDb 详情请求，只消费这些图片字段，不写其他返回字段。
- TMDb 新增图片时，通过现有 ArtworkStore 下载并建立本地选择；无 selection 时采用只填空缺的写入，dangling TMDb selection 采用条件修复写入，均不得覆盖并发产生的有效选择。
- 本次仍无图时更新该图片类型的最后复查时间；网络、限流、服务端失败或下载失败不得更新“无图”时间。
- 已存在有效手工、本地或其他来源选择时跳过，不得覆盖。
- 不得写入或消费 `catalog_hydration_jobs`，不得唤醒或复用“发现目录刮削” worker。

### R3. 两个独立任务与调度

- 任务中心显示“TMDb 图片本地化修复”和“TMDb 无图复查”两个独立 definition，各自拥有手动执行、定时开关、周期配置、运行状态、历史和日志。
- 两个任务使用互不共享的全新 scheduler 名称和配置键；均默认关闭、默认周期 24 小时，不继承旧任务开关或周期。
- 每个任务使用现有 Scheduler 的单任务并发保护；两个任务互不触发、互不等待。
- 每次执行使用内存中的 selection ID 或 metadata ID keyset 分页扫描完当时的全部候选，不一次加载全库，不保存跨执行游标，也不限制单次扫描或 TMDb 请求总数。
- 允许复用现有 TMDb provider、ImageProxy、ArtworkStore、repository、Scheduler 和 TaskTracker；不得新增通用队列、常驻 worker 或第二套下载器。

### R4. 可观察性

- 每个任务记录独立汇总指标，并通过 `TaskUpdate.Details` 为需要处理或关注的图片类型追加详情；正常本地文件只进入汇总指标。
- 详情至少包含 metadata 标题、kind、TMDb ID、图片类型、处理动作和结果；失败详情使用现有脱敏逻辑。
- 日志不得输出完整远程 URL、请求 query、凭证或本地路径。
- 本地化修复至少区分：旧链接修复、404 后 TMDb 修复、TMDb 仍无图、并发选择跳过、可重试失败。
- 无图复查至少区分：扫描、冷却跳过、已有选择跳过、发现并保存新图、仍无图、并发选择跳过、可重试失败。

### R5. 兼容、退役与安全

- 对外图片 URL 和 Emby/Jellyfin 图片读取行为不变，仍只读取本地权威图片。
- 图片文件成功写入前不得破坏现有 selection；失败不得把可恢复状态误记为“无图”或完成。
- 完全移除旧“元数据图片补齐”任务入口、任务 definition 和 scheduler，精确清理其旧开关、周期和内部扫描游标设置。
- 保留旧任务的 `task_executions` 数据库记录和已有日志文件，不迁移、不删除；旧 definition 移除后不保证还能从任务中心按旧入口访问。

## Acceptance Criteria

- [ ] AC1: 任务中心只显示两个新图片任务，不再显示旧“元数据图片补齐”；两个任务均默认关闭、默认 24 小时并支持独立手动执行、配置、历史和日志。
- [ ] AC2: 本地文件缺失且旧链接可用时，第一任务直接恢复本地图片，全程不请求 TMDb 详情。
- [ ] AC3: 旧链接返回 404 时，第一任务只为缺失图片类型读取 TMDb 图片来源；403、429、其他 4xx、5xx、超时和连接失败均不触发 TMDb 重查。
- [ ] AC4: 404 后 TMDb 仍无对应图片时写入逐类型复查时间，24 小时内第二任务记录冷却跳过，满 24 小时后重新请求。
- [ ] AC5: 第二任务不命中 `catalog_artwork_hydrated_at IS NULL` 且没有逐类型无图状态的从未处理记录；第一任务明确写入的无图状态即使没有旧 checkpoint，也能在 24 小时后被第二任务处理。
- [ ] AC6: 第二任务同一 metadata 的多个到期类型只发一次对应 TMDb 详情请求；新图片通过现有本地图片存储链落库，仍无图则分别刷新类型时间。
- [ ] AC7: 两任务在下载期间遇到手工/本地选择并发写入时均保留该选择，不覆盖有效图片；下载或 provider 失败保留原关系和可重试状态。
- [ ] AC8: 两任务均不创建、认领或唤醒 `catalog_hydration_jobs`，不产生“发现目录刮削”执行记录。
- [ ] AC9: 两任务只改变图片资产、图片关系和逐类型复查状态，不改变标题、简介、评分、人物、provider identity 或 catalog checkpoint。
- [ ] AC10: 每个扫描项可在对应任务日志中定位标题、kind、TMDb ID、图片类型、动作和结果，且日志不泄露完整 URL、query、凭证或本地路径。
- [ ] AC11: 旧 scheduler 和 definition 不再存在，旧配置键被精确清理且不控制新任务；旧执行记录与日志文件仍保留。
- [ ] AC12: 现有入库刮削、发现目录 hydration、手工图片、豆瓣候选和对外图片读取行为保持兼容。

## Out of Scope

- 不处理从未完成 TMDb 图片 hydration 的 metadata。
- 不修复手工图片、本地 NFO、豆瓣候选或其他 provider 的本地文件。
- 不重新匹配作品，不修改非图片 metadata、人物关系或 provider identity。
- 不新增通用图片队列表、常驻图片 worker 或第三个图片任务。
- 不清理孤儿 artwork asset 或磁盘文件。
- 不验证恢复后的内容哈希是否等于历史 asset；继续沿用现有图片格式校验与按内容哈希存储。
