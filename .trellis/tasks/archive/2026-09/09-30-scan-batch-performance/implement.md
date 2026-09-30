# Implementation

1. 修改 scanner builder/batch 的规则复用与仓库选择；只修改相关调用方。
2. 提取现有红果刷新实现，添加批次副本与提交后收集，保留公共 Prepare 接口。
3. 在现有隔离 PostgreSQL helpers 上增加读取次数、索引请求数及失败/取消回归测试。
4. 执行 service/repository 定向测试（含 `-race`）、相关包静态检查、`git diff --check`。
5. 独立子代理只读复核，由主代理修复并验证；更新扫描/索引 spec 契约。

只使用 `mediastation_test` 的临时 schema；不连接业务库执行测试，不启动服务或部署。提交需用户明确授权。

## Validation result

- 新增 4 个测试在真实隔离 PostgreSQL 通过；101 文件实测识别词设置读取 10 次、同作品索引写入 2 次，未变重扫均为 0。
- service/repository 相关扫描、NFO、红果索引、来源冲突及并发 Upsert 回归以 `go test -race -p 2 ... -count=1` 通过；`go vet -p 2 ./internal/service ./internal/repository` 通过。
- 两个只读独立审查已完成。提出的路径存在性计数是原有代码；来源分流仍由 `MediaRepository.Upsert` 的 `CatalogSource` 和事务内检查限制，没有新增跨来源绑定路径。
- 测试 HTTP 服务由 Cleanup 关闭，临时目录及隔离 schema 由既有 helper 清理。没有修改生产数据库、配置或运行中的服务。
- 未运行全仓测试或生产大库复扫，实际墙钟提速尚未验证；代码未提交、未部署。

## NFO 扩展计划

1. 传递 batch 到 NFO 媒体构造，条件化全库/单根的原快照读取。
2. 仓储增加兼容的入库结果入口，scanner 用事务结果计数，删除事务外统计查询。
3. 真实隔离 PostgreSQL 统计跨批规则读取、路径点查及快照读取次数；验证首扫、重扫、侧车变更、单根和单文件路径。
4. 定向 NFO/扫描/来源隔离及并发回归（含 race）、vet、diff 检查；独立复核；同步规范。

## NFO 扩展验证结果

- 真实隔离 PostgreSQL 新增测试通过。电影/电视剧各 101 文件首扫：设置读取 10 次，事务内路径读取 202 次（每文件锁查询与共享 upsert），普通扫描快照 0 次。未变重扫：设置读取 10 次、路径读取 101 次、快照 0 次。单根仅修改侧车仍更新 1 文件；单文件事件即时读取新规则。
- `go test -race -p 2 ./internal/service ./internal/repository -run '^(TestScan|TestIngestPath|TestNFO(Scan|Ingest|Fresh|Repository|Rejects|Only|Tasks|Task|Watcher|Scheduler|Series)|TestWatcherBatchRequeuesSidecarsWhenLibraryQueryFails|TestNFOLibraryCreatedAtUsesFirstItemCreation|TestMediaUpsertConcurrentPath|TestMediaUpsertDoesNotIgnoreOtherInsertErrors|TestArtworkAssetConcurrentReuse)' -count=1 -timeout=6m` 通过：service 58.194s，repository 7.864s。
- `GOMAXPROCS=4 go vet -p 2 ./internal/service ./internal/repository`、`git diff --check` 通过。两位只读复核已返回；仓储复核未发现阻塞项。scanner 复核的指针类型疑问经字段、返回签名及已通过的编译核查排除。
- 测试使用 mediastation_test 的自动清理随机 schema；凭据只在内存与子进程环境中使用，不输出或落盘。未启动调试服务、修改生产数据、部署或重启。
- 共享 NFO/图片缓存、持久化侧车快照及内部共享 upsert 冲突流程未改造；本轮只删除事务外的统计路径查询及无用的普通扫描快照。未运行全仓测试或生产整库性能基准，墙钟提速仍待部署后实测。

## NFO 增量扫描与共享读取计划

1. 主代理实现有界读取缓存、可靠文件版本与依赖记录，接入现有 NFO/XML 图片和文件候选选择。
2. 新增 binding 快照列、轻量查询与事务内快照保存；扫描构造后判定提前跳过。
3. 最小有意义测试覆盖缓存隔离、稳定性和候选变更；真实 PostgreSQL 验证重扫无写事务及资料/图片恢复。
4. service/repository 定向 race、vet、diff/gofmt 检查；两个独立只读审查；同步 NFO 规范及实际验证结果。

## NFO 增量扫描与共享读取验证结果

- 本轮授权的共享读取与提前跳过两项均已实现。101 文件电影/电视剧首扫：识别设置 10 次、依赖查询 101 次、入库锁 101 次、事务内媒体路径读取 202 次；完全未变重扫：设置 10 次、轻量依赖查询 101 次，入库锁和事务内路径读取均为 0。单个侧车变更仅执行 1 次入库事务、2 次事务内路径读取，更新 1 文件。
- 共享整剧/季/三个文件（两集含一个版本）首次入库：缓存含 5 个唯一 XML 文档及 1 张唯一图片；数据库保存 2 个独立分集、1 个共用图片资产。整剧/季 NFO 更新会更新三个文件；单集变更只更新对应文件。入库后缓存资产仍无 ID/更新时间。
- 已验证手工资料保留、旧记录/未知版本/截断 JSON 升级只改依赖列、不改 UpdatedAt；同大小恢复 mtime 的 NFO、真实图片和视频覆盖；新候选、拒绝的横图变竖图、XML 缺失引用恢复、删除恢复、读取中变化、断链符号链接、祖先链接换目标、Glob 嵌套模式目录新增匹配、有界依赖回退。视频更新会使已有探测失效。受控图片丢失且原图存在时重建；坏/缺 NFO 和取消保持成功资料/快照。
- 真实隔离 PostgreSQL 的 service/repository 定向扫描、NFO、普通本地读取、来源隔离及并发回归以 race 通过：service 57.716s，repository 7.943s；后续快照解码/上下文摘要/规则 fallback 等增补以定向 race 再次通过：service 12.604s，repository 1.268s；祖先符号链接及输入依赖的文件系统定向 race 通过（1.086s）。
- go vet、gofmt 检查和 git diff --check 通过。最初读取复核覆盖不足，已拆成两个小范围任务完整重核验。缓存复核无阻塞项；依赖复核的“不同目标目录版本相同”假设经 device/inode/ctime 实现及祖先链接换目标测试排除。事务审查的未知版本/上下文疑问经 unchanged 的明确校验与坏/未知快照测试排除；Media 使用 PermanentBase，无 soft-delete 字段。只读跳过的并发观察边界已写入设计，不恢复写事务，也不跨归属写入。
- 新列由现有 AutoMigrate 注册补充，重复完整启动迁移测试通过；仅更新 scan_inputs 不更改绑定身份或触发最新文件时间维护。
- 测试仅使用 mediastation_test 自动清理随机 schema 和临时目录；凭据未输出或落盘，未启动调试服务、提交、部署、修改生产数据库或重启。未做生产大库复扫、全仓测试及非 Linux 运行验证；真实墙钟提速仍待部署后实测。旧记录第一次建立快照仍需完整处理；无可靠版本的平台及超过依赖上限的文件回退完整读取。
