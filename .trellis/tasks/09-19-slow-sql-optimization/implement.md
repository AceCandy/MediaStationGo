# 执行与验证

1. 为两类领取添加部分索引及固定状态谓词；仅触及仓储领取、迁移及针对性测试。
2. 在隔离 PostgreSQL schema 中比较大队列执行计划，强制 generic plan，验证无全表排序及索引命中。
3. 验证状态/检查点/时间顺序，运行相关 repository/database/service Go 测试。
4. 独立审阅 diff，检查用户原有修改未被覆盖；持续检查日志增量。
5. 记录结果和部署限制；若需要运行库迁移，提供确切索引 SQL 和影响。

## 首批结果

- 原始回归失败：40010 行 fixture，generic plan 为 Seq Scan + Sort，执行 37.771ms。
- 仅增加索引仍失败：BitmapOr + Sort，23.649ms；提出五状态 AND 谓词后通过。
- 优化后下载有序索引执行 0.080ms / shared hit=5；校验空队列 0.061ms / shared hit=3。
- `go test -race ./internal/repository ./internal/database ./internal/service -run 'TestHongGuoDownload(ClaimOrderAndPlan|QueuePlacementCancelAndRecovery|PublicationRecoveryAndConflict|SeparateVerificationAndRecovery|CancelThenRetryRunningLease|DynamicConcurrencyAndShutdown)$|TestEnsurePerformanceIndexesCreatesHotPathIndexes$' -count=1`：三包通过，使用 mediastation_test 隔离 schema，非跳过。
- 独立只读复核无阻断问题；git diff --check 通过。未运行全量测试、前端检查或真实下载。
- 截至 16:39:21，最近 200 条日志中 199 条仍为旧领取查询，平均约 241ms；尚未应用运行库索引或重启服务。
- 运行库带 FOR UPDATE 的 EXPLAIN 被数据库工具策略拒绝；改用不加锁的只读执行计划完成诊断，没有执行运行库领取。
- 后续：部署两条索引并加载新代码，按相同窗口观察新增日志；其余历史慢 SQL 尚未修改。

## 第二批结果

- FindByLogicalMetadataIDs 已改为先限定媒体候选，再关联展示字段；运行库单 movie 样本只读计划从 1847.422ms / 1655828 shared hits 降至 0.649ms / 42 shared hits。此为 SQL 实验，非已部署服务耗时。
- ListWorks 改为按作品先分页，再统计当页全部分集；completed 筛选样本原计划 321.786ms，单纯提前分页仍 331.981ms，进一步以 BOOL_OR 消除重复全表身份连接后 178.524ms。不同时间采样存在缓存与并发影响，不作为稳定延迟承诺。
- 新增新旧媒体查询完整结果对照测试，覆盖 series/season/episode/movie、混合重复 ID、多版本、缺失 ID、可见性及展示过滤。
- 真实 mediastation_test 隔离 schema 中，带 -race 的 TestMediaViewLogicalScopePreservesResults、TestHongGuoDownloadWorkGroupingAndRetry、TestListRecentSeriesCardsCountsAllEpisodesInSeries 全部通过（非跳过）。
- 扩大 TestMediaView 测试发现既有 TestMediaViewProjectsEpisodeArtworkAndParentIdentifiers:325 期望 episode2 backdrop 为空，但当前投影回退到 series backdrop。该测试走 FindByID，不经过本次改动；未修改不相关展示语义或断言，整体测试不全绿。
- 独立审查下载汇总未发现阻断。审查提出请求 season 丢失父 series 文件，经原 CASE 比较方向核验属误报：父 series 媒体逻辑 ID 为 series 自身，并不等于请求 season ID；新旧完整结果对照通过。
- 未运行全量测试，未应用运行库索引、重启服务或验证部署后接口延迟。TMDb 复查、目录统计、搜索等剩余慢查询尚未修改。

## 第三批结果

- 搜索索引关系查询原 1024 阈值过早切换整库媒体扫描。当前媒体约 375269 行；日志样本 1026–1181 候选集，旧计划 189.706–237.999ms，索引点查 12.442–13.646ms。重复首样本旧 183.503ms、新 13.472ms，收益并非仅首次冷读。
- 扩大候选范围的只读对照：4143 集为 234.611→62.118ms；8001 集为 286.039→122.207ms；32321 集为 470.303→417.317ms。保守提高阈值至 8192，保留更大批次扫描，不宣称普适最优阈值。
- TestMetadataSearchDocumentsWithManyUnplayableEpisodes 在真实测试库带 -race 通过，覆盖原小批、8192 点查、8193 扫描及完整文档相等。BackfillSkipsEmptyCandidates、BackfillPauseAndCancellation、RefreshesMediaAndMetadataChanges 均通过，TMDb 列表三个测试亦通过。
- 扩大 `TestMetadataSearch|TestTMDbRecheckList` 测试集整体失败：TestMetadataSearchCountsPlayableTopLevelWorks:98 返回 total=0，预期2。其 SearchMetadataIDs→searchMetadataIDsPostgres/rankMetadataSearchIDs 路径不调用本次修改的 metadataSearchDocuments；未扩展修改用户搜索语义，也未声称整体测试通过。
- 独立只读复核阈值、计数上限、fixture 的已有1集及两侧边界，无阻断问题。git diff --check 通过。
- TMDb 统计另试季分支非相关 IN/hash：1869.983ms、860691 shared hits，未表现出明确收益，未采用。前轮去掉 OFFSET 0 与全量可见集合候选也未采用。
- 本轮未改运行库、未重启服务、未跑全量测试或验证部署后效果；17:04:38 最新日志仍为旧下载领取，241ms。存储统计、目录查询等剩余项目未完成。

## 17:09 重启后验证

- 用户已重启，17:09–17:18 共 37 条慢 SQL；前相邻 9 分钟共 530 条，其中下载领取 517 条，重启后领取为 0。负载不同，不能视为严格基准。
- 两条领取部分索引 indisvalid/indisready 均 true；媒体展示日志已出现 requested CTE 新 SQL。
- 重启后 6 次搜索索引慢批次包含 8239–22094 集，均超过8192阈值，仍属于保留的大批扫描路径。

## 第四批结果

- 存储统计：三轮旧计划 1816.913/1768.340/1771.788ms，新计划 1492.973/1470.247/1554.258ms；shared hit 358391→约302235，临时写入11534→约5828块。双向EXCEPT ALL差异0。
- 目录快照候选：保留原ID选择规则，只改变缺失快照候选关系。旧暖缓存计数516.785/495.456/491.816ms，候选改写约362–368ms；去掉无必要的OFFSET0后约360ms。分页旧1405.581ms，最终简化版373.748ms，完整投影双向EXCEPT ALL差异0，计数均49。
- 库计数回归补充重复直接series/season文件；目录测试补充多标识、错误provider/kind、溢出/超长ID。首次目录fixture误复用全局唯一外部ID，修正为不同溢出值后重跑通过；未放宽数据库约束。
- 新增 TestMissingTMDbSnapshotCountScopesMetadataProbes：10000无关元数据、一个合法identifier，经ANALYZE后检查实际计划只访问候选元数据。独立审查提醒跨数据库版本的计划选择风险；作为明确性能回归门槛保留，升级PostgreSQL时需重新验证。
- `go test -race ./internal/repository ./internal/service -run 'TestMissingTMDbSnapshotCountScopesMetadataProbes|TestListMissingTMDbSnapshotsWithoutMediaTable|TestStorageBreakdownCountsLibraryMetadata|TestTMDbSnapshot|TestAutomaticTMDbSnapshot|TestNonTMDbIngestionFillsSnapshot' -count=1`：真实mediastation_test隔离schema，两包通过（非跳过）。
- 两次独立只读复核未发现SQL语义问题；最终git diff --check通过。未执行全量测试或前端检查。
- 未采用的实验：存储顺序去重增加随机读取；直接四类COUNT DISTINCT更慢；仅EXISTS合法标识会误导计划导致约8秒，故保留原LATERAL选择；TMDb linked_episodes共享集合候选30秒超时，未采用，随后确认pg_stat_activity无该实验残留查询。
- 本轮无运行库写入、无服务重启、无新索引或全局参数调整；运行库实验均只读，新代码尚未部署。TMDb复查统计、媒体库分页、最近作品与大批索引查询仍有剩余优化空间。

## 第五批结果与后续边界

- 用户指出阶段汇报不应成为停止优化的理由：后续保持连续推进；只有需要业务选择或运行库/部署权限时才暂停确认，不把单项测试通过当作整体完成。
- 最近作品增加128文件游标快路径，保留原聚合回退。16批未确定结果或NULL时间均回退，不截断全集。新增全库时间部分索引及幂等迁移断言，在线SQL已列出，运行库尚无此索引。
- 只读样本最新128文件已覆盖53作品；有界128候选关联实验928.753→209.614ms（尚无新索引），仅证明缩小关联范围的方向，不等同于实际多语句接口耗时或上线承诺。
- 媒体库普通series范围只读取窄季集合：重复对照旧912.919/979.354ms，新688.871/678.501/727.432ms；shared hits336521→161338。完整结果双向EXCEPT ALL差异0。仅改季LATERAL为952.863ms无收益，去除scoped物化10秒超时，两者均未采用。
- 新增2000集计划测试确认季集合只读一次；继续覆盖直接series/season文件、分页平局、空页总数、旧哈希深链、权限和缺失信息筛选及统计滞后。最近作品测试覆盖跨128边界、NULL、严格筛选触发回退，以及首页/深页时间索引计划。
- 最后一次真实PostgreSQL验证：`go test -race ./internal/repository ./internal/database ./internal/service -run 'TestRecentLogicalWorks|TestMediaViewLogicalScope|TestListRecentSeriesCards|TestEnsurePerformanceIndexesCreatesHotPathIndexes|TestLibrarySeriesPage|TestLibraryMetadataPagination|TestLibraryFilteredSeriesPage|TestLibrarySeriesEpisodes|TestMissingTMDbSnapshotCountScopesMetadataProbes|TestListMissingTMDbSnapshotsWithoutMediaTable|TestStorageBreakdownCountsLibraryMetadata' -count=1`，三包通过（非跳过）。
- 独立只读复核未发现阻断。审查提到游标测试遗漏，经核验为误报：NULL、跨128边界及低命中16批回退已在TestRecentLogicalWorksPreservesBatchTiesAndFilters覆盖；执行计划Alias依赖为真实维护风险，数据库升级需复核。
- 剩余：TMDb计数只读计划2319.829ms；任务执行历史hongguo计数204.472ms，约60万匹配记录、约299MB表，不是领取问题。media/metadata/task表有死元组，但单次统计不能证明偶发慢写根因；当前活动会话快照未见锁等待，不排除历史等待。未擅自VACUUM、建运行库索引或调整参数。
- 第四/五批需部署后继续观察。已询问是否允许仅并发创建idx_media_recent_metadata，未收到批准前不执行；不重启服务、不提交代码。全量测试、真实接口耗时与并发负载尚未验证，前述两项既有测试失败未处理。

## 第五批索引部署验证（2026-09-19）

- 用户明确批准后，仅对mediastation.public.media执行单条 `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_media_recent_metadata ON public.media(created_at DESC,id DESC) WHERE metadata_id IS NOT NULL`；预检确认同名索引不存在，media没有正在创建的索引。未执行文件中的其他索引语句。
- 独立连接核验索引定义一致，indisvalid/indisready均true，占用24MB。
- 运行库只读、force_generic_plan验证：首页128条0.576ms，时间游标页128条0.724ms；两者均为Limit→Index Only Scan，命中idx_media_recent_metadata，无Sort或Seq Scan。仅为候选选页计划，不代表完整接口延迟。
- 此前第五批 `go vet ./internal/repository ./internal/database ./internal/service` 及git diff --check均通过。
- 未重启服务、未部署代码、未提交；第四/五批代码的真实接口及并发负载效果仍待用户部署后观察。此次没有修改其他运行库对象。

## 18:14 重启与第六批

- 18:14重启后截至18:16:35有54条慢SQL，新目录谓词和季集合均出现，普通库分页689/491ms；下载领取0条，下载UPDATE13条（最高1227ms）。最近作品选页无慢记录，但不能将短窗口无记录当作全面消除。
- 指定剧卡片/详情仍通过CASE反查整个元数据目录。强化既有执行计划测试后原实现失败：读取10014条元数据；现在先限定库内目标剧/季/集文件，测试通过。保持原权限和work级缺失字段过滤，补充直接series/season附件、无效seriesID测试。
- 生产只读样本卡片229.552→2.774ms，shared hits62215→266；分集详情197.380→4.463ms，hits63124→1220。完整结果双向EXCEPT ALL均为0，未执行生产写入。
- 执行计划测试首次重跑因日志中的 `'[bounded-series]'` 不是PostgreSQL数组而失败；修正为占位符并绑定数组，未改变查询语义或断言。
- 对TMDb统计尝试仅物化episode分支媒体ID（无去重/有去重）：原1552.233ms，候选1814.087/2330.610ms，均未采用。保留现有统计准确性，无缓存和新索引。
- 慢日志新增db_pool_max_open/in_use/idle及db_pool_wait_count_total、db_pool_wait_ms_total；仅成功慢查询采样，单位和累计语义经测试验证，保留禁用/快速/错误路径和参数脱敏。需要新代码上线后才能判断等待增量，不能据默认连接池容量猜测根因。
- `go test -race ./internal/service ./internal/repository ./cmd/server -run 'TestLibrary|TestMediaViewLogicalScope|TestRecentLogicalWorks|TestListRecentSeriesCards|TestSlowSQL' -count=1` 三包通过，数据库测试使用真实mediastation_test隔离schema，非跳过；go vet对应三包和git diff --check通过。未跑全量测试或真实接口并发压测。
- 独立只读审查提出UNION ALL重叠候选会复制媒体行，经实际IN半连接和TestMediaViewLogicalScopePreservesResults核验为误报，不添加无收益DISTINCT；无剩余已确认阻断。
- 18:20–18:27:48新增18条慢日志：11条下载UPDATE、2条任务UPDATE、5条搜索索引查询。随后会话快照3个idle、blocked_sessions=0，只能说明采样时无阻塞，不能排除历史等待。
- 当前第六批代码尚未部署，无运行库变更、服务重启、代码提交或调试服务；TMDb等全库统计仍有耗时，下一步加载新代码后核验单剧接口，并对照新等待字段诊断偶发慢写。

## 提交归档检查

- 用户明确要求提交、推送、归档，并确认把现有下载前后端改动一并提交：下载状态筛选接口与作品汇总签名配套，目录按作品ID分64桶，启动只迁移整部从未开始的旧排队任务，不移动已下载文件。
- 合并范围验证：真实PostgreSQL隔离schema下，service/repository/handler/database/cmd/server五包带-race针对性测试全部通过，覆盖下载目录迁移、下载状态HTTP筛选、领取、作品汇总、媒体分页、搜索批次、目录/存储统计及慢日志。相关五包go vet、git diff --check通过。
- 前端TypeScript检查、改动文件ESLint、全量npm run lint和npm run build均通过。未执行浏览器端到端测试、完整Go测试集或真实下载；已知既有测试失败仍按前述记录保留。
- 本次归档仅结束本轮交付，不表示消除所有慢SQL。TMDb/存储/目录统计和偶发慢写仍需跟进，第六批代码需部署后观察；其他活动任务不在本次归档范围。
