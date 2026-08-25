# Metadata 图片补齐与硬删除执行计划

## 1. 成功标准

实现完成必须同时满足：

- canonical metadata 缺图查询不连接 `media`，按 ID keyset 有界返回。
- scheduler 只负责发现和排队，现有 catalog job/worker 负责下载、重试和恢复。
- 自动 catalog 导入在并发下也不能覆盖已有手工/本地 selection。
- Movie/Series/Season/Episode 的实体自有图片契约全部覆盖。
- 唯一可信豆瓣标识的 Movie 可作为第二数据源补齐原始快照、空字段和本地候选海报，且不覆盖当前图片。
- 精确范围内八张表完成硬删除迁移，不丢失有效 media、metadata、credit、selection 或 asset。
- PostgreSQL migration、repository、service、scheduler 和既有展示/Emby 回归验证通过。
- 真实规模候选查询完成 EXPLAIN 审查后才决定是否加索引。

## 2. 实施顺序

### Phase A. 固化基线与测试夹具

- [x] 记录实施前工作树，确认不覆盖用户现有修改。
- [x] 阅读即将修改文件的当前内容，并重新加载 backend database、shared metadata、background task specs。
- [x] 为 legacy soft-delete schema、缺图候选、completed job 重排、Series 子项补图和 selection 竞态准备最小 PostgreSQL 测试数据。
- [x] 先写会失败的行为测试：
  - metadata 缺图查询不依赖 media，按 kind 返回正确 missing types。
  - 同一 Series 多个缺图后代只返回一个 root。
  - completed 可重排，active/retry 不被重置，scheduled 不复活 failed，manual 可复活 failed。
  - root 完整但 Season/Episode artwork checkpoint 为空时 worker 会继续访问后代。
  - 已有/并发创建 selection 时 catalog 不覆盖。
  - 元数据更新请求不能清空或替换图片 selection。

验证门：新测试准确复现当前缺口，失败原因与设计一致；不通过修改断言来迁就实现。

### Phase B. Metadata 图硬删除迁移

#### B1. 模型与 schema

- [x] 在 `internal/model/model.go` 增加不含 DeletedAt、但保留 UUID BeforeCreate 行为的 `PermanentBase`。
- [x] 将 `Media`、`MetadataItem`、`MetadataIdentifier`、`MetadataProviderSnapshot`、`CatalogHydrationJob`、`MetadataArtwork`、`ArtworkAsset`、`MetadataCredit` 改为嵌入 `PermanentBase`。
- [x] 移除 MetadataItem Season/Episode 唯一索引 tag 中的 `deleted_at` predicate，只保留 kind predicate。
- [x] 在 `schema_migration.go` 增加事务化、幂等的 metadata soft-delete retirement migration，并放在 performance index 创建前。
- [x] 加入残留引用预检、历史 tombstone 处理、asset 保留、旧索引替换和八列删除。

#### B2. Repository 与读取路径

- [x] 删除八表相关的显式 `deleted_at IS NULL` JOIN/WHERE。
- [x] 删除 media、identifier、snapshot、selection、credit 的 restore/`deleted_at=nil` 分支。
- [x] 将 identifier replace/invalidate 和 selection clear 改为硬删除。
- [x] 将 media 与 metadata credit 改为硬删除；保留 favorite、history、playlist、people 等外部表的既有软删除过滤和 merge 处理。
- [x] 逐点更新 MediaView、search、playback event、people backfill、Emby metadata scope 等引用 metadata/artwork 的查询；只删目标表 predicate，不顺手重构 SQL。

主要风险文件：

- `internal/model/model.go`
- `internal/model/metadata.go`
- `internal/database/schema_migration.go`
- `internal/repository/metadata_repository.go`
- `internal/repository/metadata_repository_merge.go`
- `internal/repository/artwork_repository.go`
- `internal/repository/media_view_repository.go`
- `internal/repository/media_search_repository.go`
- 其他含八表 deleted predicate 的 repository/service 查询

验证门：

- legacy 数据迁移后八列不存在，活动 media、metadata、credit 和选择关系不变。
- 有残留 media/user/people 引用的软删除 metadata 会让 migration 失败且事务完整回滚。
- migration 连续执行两次无错误。
- identifier/artwork clear 后 Unscoped 查询也找不到被删关系。
- merge 引用迁移与 source 硬删除测试通过。

回滚点：此阶段未启用 scheduler；若 migration 测试不满足引用安全，不进入 Phase C。

### Phase C. 缺图候选与 job 重排

#### C1. Repository 查询

- [x] 在现有 metadata/catalog repository 增加逐 metadata 的 keyset 缺图查询。
- [x] 增加按 Movie/Series root 的 keyset 调度查询，使用 EXISTS 去重，不连接 media、不使用 offset/DISTINCT。
- [x] 查询接受 scheduled/manual 模式，以决定是否排除 failed job。
- [x] 增加 backfill 专用原子 ensure 操作，返回 created/requeued/unchanged。

#### C2. Job 状态

- [x] 增加 `CatalogJobStatusFailed` 和固定 job 生命周期最大尝试次数；stage 变化不重置 attempts。
- [x] 失败次数未耗尽时保持现有指数退避；耗尽后写 failed、清空 next attempt。
- [x] completed/failed 重排统一回到 root stage 并清理旧运行字段。
- [x] 保持 running 启动恢复为 retry 和 `SKIP LOCKED` claim 不变。

主要风险文件：

- `internal/model/metadata.go`
- `internal/repository/catalog_repository.go`
- `internal/repository/catalog_repository_test.go`

验证门：并发 ensure 最终仍只有一条唯一 job；running/retry 的 attempts 和 next_attempt_at 不变化；failed 不会形成周期热重试。

回滚点：此阶段尚无 scheduler 注册，新增查询和状态不会主动产生 provider 流量。

### Phase D. Catalog worker 补图语义

#### D1. Series 后代遍历

- [x] 扩展 child candidate：Season 自身缺 catalog/artwork 或拥有缺 artwork Episode 时可被访问。
- [x] Episode catalog/artwork 任一未完成时可被访问。
- [x] 确保 Season 自身完整而 Episode 缺图时不重复写 Season metadata，只处理 Episode。

#### D2. Selection insert-if-absent

- [x] 为 ArtworkRepository 增加 catalog 专用条件保存：有效 selection 冲突时保留，指向缺失 asset 的无效 selection 才允许替换。
- [x] ArtworkStore catalog 路径下载前快速检查；保存时使用 if-absent，不改变手工 overwrite 路径。
- [x] 统计 saved/existing/source-missing；明确远程路径为空时不调用 ImageProxy。
- [ ] 从元数据编辑前后端契约移除 poster/backdrop URL；删除不再可达的 selection 清空逻辑和对应旧测试。

#### D3. 失败与 metrics

- [x] worker 根据 attempts 选择 retry 或 failed。
- [x] 在现有 catalog TaskExecution 写入 artwork 结果、retry、failed metrics；日志继续脱敏 URL。

主要风险文件：

- `internal/service/catalog_hydration.go`
- `internal/service/artwork_store.go`
- `internal/repository/artwork_repository.go`
- `internal/service/media_metadata.go`
- 对应 service/repository tests

验证门：

- provider 空路径完成 checkpoint，不发下载请求。
- 下载/存储失败不写 checkpoint并进入退避。
- 既有本地图片和并发手工选择均保持不变。
- Series root completed 后重排可补 Season poster/Episode still。

回滚点：scheduler 仍未注册；可通过不调用 backfill 查询保持新路径无新增流量。

### Phase E. Scheduler 与任务中心接入

- [x] 在 `SchedulerService.Start` 注册 `metadata_artwork_backfill`，默认关闭、默认 24 小时。
- [x] 在 scheduler local jobs 增加薄调用，实际有界 pass 放在 ScraperService 的 metadata artwork backfill 文件/方法中。
- [x] 增加 task kind/name 和 task definition，action 使用现有 `scheduler`。
- [x] 单次 pass 使用 200 page、1,000 scanned、200 queued/requeued 上限；只在产生新 pending job 后唤醒 worker 一次。
- [x] 巡检 TaskExecution 记录 roots_scanned、missing_roots、jobs_created、jobs_requeued、jobs_unchanged。
- [x] 验证现有 `TasksPage` 可通用显示；除非类型检查证明必要，不修改前端。

主要风险文件：

- `internal/service/scheduler.go`
- `internal/service/scheduler_local_jobs.go`
- `internal/service/task_definitions.go`
- `internal/service/artwork_backfill.go`（若新建，保持单一职责和最小实现）
- `internal/service/scheduler_test.go`

验证门：默认配置不产生自动 provider 流量；手动触发异步返回；同一 scheduler job 不能并发运行；任务中心能配置、运行、查看历史和日志。

### Phase E2. 本地资产完整性预检

- [x] Repository 按 asset ID keyset 分页列出仍被 selection 引用的 `artwork_assets`，不重复返回共享资产。
- [x] Repository 事务化失效缺失 asset：清空全部关联 metadata 的图片 checkpoint，硬删除全部相关 selection，保留 asset 行。
- [x] ArtworkStore 复用受控 `storage_key` 路径解析，文件不存在才失效；权限、非法路径及其他 I/O 错误返回失败且不改数据库。
- [x] 复用现有 metadata artwork backfill pass，按 200 page / 1,000 scanned 有界预检，以内部 setting 保存跨 pass cursor，并记录 assets_scanned、assets_missing、selections_invalidated。
- [x] 测试共享资产、存在文件、缺失文件、错误不误删、cursor 续扫及同一次 pass 后续重新发现。

验证门：对外仍只读取本地图片；一个缺失共享文件只检查一次并清除全部失效选择；不删除 asset 或磁盘文件。

### Phase E3. 豆瓣详情快照

- [x] 豆瓣详情请求保留完整原始 JSON，并明确标记来源为 `douban`。
- [x] 仅在豆瓣 match 被接受进入 canonical 持久化后补取缺失详情并幂等保存 provider snapshot。
- [x] 已接受的豆瓣主匹配保留其已选字段；后续统一 Movie enrichment 只按固定主从规则补缺。
- [x] 测试未知字段保留、快照 round-trip、重复写更新以及 canonical 字段不被隐式融合。

验证门：搜索候选不产生数据库快照；接受豆瓣 match 后能按 metadata/provider 读取完整 JSONB，展示字段行为不变。

### Phase E4. 豆瓣电影第二数据源

#### E4.1 候选图片关系

- [x] 新增 `MetadataArtworkCandidate`，使用 `PermanentBase`；按 `(metadata_id, artwork_type, source_provider)` 唯一关联已本地化 asset。
- [x] 在 ArtworkRepository/ArtworkStore 增加豆瓣候选海报幂等保存和有效候选查询；复用现有下载、校验、内容哈希和 DataDir 存储。
- [x] 当前 poster selection 为空时原子提升候选；已有任意有效 selection 时只保存候选，不覆盖当前图片。
- [x] 扩展本地资产完整性巡检，使 selection 和 candidate 引用的 asset 都被有界检查；候选文件缺失时硬删失效候选并允许重新补齐。
- [x] metadata merge 迁移候选和 provider snapshot，metadata 删除级联清理候选关系，但不直接删除共享 asset 或文件。

#### E4.2 固定融合规则

- [x] 抽取豆瓣原始 JSON 到 Match 的解析函数，网络详情与已存 snapshot 共用，确保未知字段仍原样保留。
- [x] 新增单一幂等 enrichment 函数：仅接受 `kind=movie` 且恰好一个豆瓣 Movie identifier；Series/Season/Episode、多 identifier、明确 TMDB 冲突直接跳过。
- [x] snapshot 先幂等保存；canonical 只补空/零值字段，当前标题不含中文且豆瓣标题含中文时允许替换，保持 source、TMDB identity 和其他已有字段不变。
- [x] 豆瓣 poster 立即保存为候选，并按 E4.1 规则决定是否提升。
- [x] TMDB 主匹配携带可信电影豆瓣 ID 时保留该 identifier，并在主数据成功后 best-effort 调用同一 enrichment 函数；失败不把媒体刮削改为 error。

#### E4.3 慢速历史任务

- [x] 注册 `douban_movie_enrichment` scheduler/task definition，默认关闭并复用现有手动执行、周期配置、历史和日志界面。
- [x] Repository 按 metadata ID keyset 有界列出唯一豆瓣 identifier 的 Movie；不得连接 media、不得标题搜索、不得返回 Series/Season/Episode。
- [x] 单次固定小批、provider 请求串行且固定间隔限速，cursor 持久化并在末尾归零；已有 snapshot 时优先复用 payload，避免重复详情请求。
- [x] 临时请求/下载失败保留后续重试机会；多 identifier、类型不支持、identity 冲突计入 skipped，不形成热重试。
- [x] TaskExecution 记录 scanned、enriched、snapshot_saved、fields_filled、candidate_saved、candidate_promoted、ambiguous_skipped、failed。

验证门：历史和在线路径复用同一函数；重复运行幂等；豆瓣候选本地存在但不覆盖当前图片；当前图片缺失时可安全提升；不产生任何电视剧豆瓣快照或候选。

#### E4.4 元数据编辑边界

- [x] 从 `MetadataEditDialog` form、初始化、payload 和 UI 中移除 poster/backdrop URL。
- [x] 从前后端 `MediaMetadataUpdate` 移除 poster/backdrop 字段，后端更新流程不再调用图片 selection 修改逻辑。
- [x] 保留标题、简介、年份、评分、provider ID 等既有编辑能力；图片展示与专用图片来源流程不变。
- [x] 更新定向测试，证明元数据编辑不会清空/替换现有 selection，且其他字段仍正常保存。

### Phase F. 回归、性能与独立复核

- [x] 运行 gofmt，仅格式化本任务修改的 Go 文件。
- [ ] 运行 targeted PostgreSQL tests：migration、catalog repository、artwork repository、catalog hydration、scheduler。（环境未配置 `MEDIASTATION_TEST_POSTGRES_DSN`，目标用例均明确 skip）
- [x] 运行共享 metadata、MediaView/search、手工编辑、Emby/Jellyfin 图片读取回归测试。（`go test ./internal/...` 通过；依赖 PostgreSQL 的部分除外）
- [x] 前端移除图片 URL 编辑字段后运行 `npm run build` 与 `npm run lint`，均通过。
- [ ] 在真实规模 PostgreSQL 数据执行诊断/root 查询 EXPLAIN；记录脱敏后的节点、rows、buffers、耗时和是否需要索引。（当前无可用生产规模 PostgreSQL 数据源）
- [x] 独立检查一次 spec compliance、数据流、竞态、migration rollback 和未预期 diff。（独立子代理上游失败；主线程在实现后重新审查并补齐 merge snapshot 保留）
- [x] 检查没有临时导出、DSN、完整远程 URL、日志或本地调试产物进入工作树。

最终门：所有 AC 有对应测试/证据，定时任务仍默认关闭，才允许交付。

## 3. 验证命令

实际执行前使用已配置的测试 PostgreSQL DSN；不得在终端记录、测试日志或文档中打印 DSN。

### 3.1 格式与静态检查

```bash
gofmt -w <本任务修改的 Go 文件>
git diff --check
```

如果前端未修改，前端命令是兼容性检查而非必需改动来源：

```bash
cd web
npm run build
npm run lint
```

### 3.2 Targeted Go tests

```bash
go test ./internal/database -run 'Test.*(Metadata|Artwork|SoftDelete|Migration)'
go test ./internal/repository -run 'Test.*(Metadata|Catalog|Artwork)'
go test ./internal/service -run 'Test.*(CatalogHydration|Artwork|Scheduler|SharedMetadata|Emby)'
```

### 3.3 包级回归

```bash
go test ./internal/database ./internal/repository ./internal/service
```

测试依赖 `MEDIASTATION_TEST_POSTGRES_DSN` 时沿用 `internal/testdb.OpenPostgres`：未配置应明确 skip；最终 PostgreSQL migration 验证不能以 skip 作为通过。

### 3.4 生产规模执行计划

```sql
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
<diagnostic missing-artwork query>;

EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
<root scheduling query>;
```

索引新增门槛：出现不可接受的全表扫描/重复循环，且 rows、buffers 与真实耗时证明现有索引不足。禁止仅凭 SQL 外观添加索引。

## 4. 独立复核清单

- [x] 候选查询没有 `media` JOIN，也没有 media-level `DISTINCT`。
- [x] checkpoint 空值、provider 明确无图、失败三种状态没有混淆。
- [x] 元数据编辑不能清空 selection；手工/local selection 不会被自动 catalog 或豆瓣候选覆盖。
- [x] completed 重排不会重置 active/retry；failed 只有 manual 可恢复。
- [x] root 完整、后代缺图的 Series 可以继续遍历。
- [x] 八表硬删除与外部软删除表边界清晰，没有误删活动 media、user/people 数据。
- [x] migration 遇到残留引用时回滚，且不碰磁盘图片文件。
- [x] 旧 metadata partial indexes 已替换；无悬空 deleted predicate。
- [x] 默认关闭，未产生升级即全库请求的行为变化。
- [x] 除豆瓣候选图片关系外无额外框架、队列表、handler 或前端页面；历史补齐复用现有 scheduler/task tracker。

## 5. 上线与回滚操作

上线：

1. 备份数据库。
2. 部署并确认 migration 成功。
3. 执行 EXPLAIN 和一个手动有界 pass。
4. 核对任务统计、catalog retry/failed、实际图片展示。
5. 再由管理员决定是否启用 24 小时周期。

回滚：

1. 关闭 `metadata_artwork_backfill` scheduler。
2. 若仅功能异常，部署修复/上一版本前先处理现有 pending/retry job。
3. 若需回滚到仍依赖 DeletedAt 的旧二进制，维护窗口内重新添加八表 nullable `deleted_at` 和旧索引，再部署旧版本。
4. 若 migration 尚在事务内失败，无需修复半成品 schema；排查残留引用后重试。

## 6. 明确不做

- 不实现负 checkpoint TTL 刷新。
- 不实现逐图片类型 suppression。
- 不搜索豆瓣关联，不处理 Series/Season/Episode，只补已有唯一豆瓣标识的 Movie。
- 不实现孤儿 asset/file GC。
- 不新增前端专用页面。
- 不增加未经 EXPLAIN 证明的索引。
