# 验证记录

## 已通过

数据库相关测试均设置 `MEDIASTATION_TEST_POSTGRES_DSN`，使用临时 PostgreSQL 16 和每测试独立 schema，未以跳过数据库测试作为通过依据。

- 豆瓣、独立评分、快照兼容、绑定、迁移、刷新任务定义的聚焦测试通过，最终使用 `-race` 同时覆盖 model/database/repository/service/handler 五个包。
- database 完整包通过（61.684 秒），repository 完整包通过（111.218 秒）；后续候选查询调整也经过聚焦回归。
- handler 完整包通过（42.335 秒），最终任务定义聚焦测试也通过。
- service 中豆瓣、Emby、媒体读取、scheduler、任务、刮削和持久化相关回归通过（254.535 秒），排除下述已在基线复现的分页断言。
- 最新获取时间修正后的刮削、持久化、非 TMDb 摄取及 TMDb 快照自动补录回归通过（6.075 秒）。
- `TestDoubanLateLocalCompletionDoesNotOverwriteRefreshedSubject` 验证旧补齐结果无法覆盖更新后的共享评分和详情。
- `TestDoubanSeriesPersistenceKeepsOriginalFetchingTime` 验证无类型详情按现有电视剧绑定持久化时保留原始获取时间；普通刮削的自动绑定边界不变。
- 最终 `go vet` 覆盖上述五个包；`git diff --check` 通过。
- 独立只读复核确认生产搜索摘要不携带旧 RawJSON，获取入口复制真实时间，图片修复仅解析图片；未发现生产写入路径丢失获取时间。

## 完整测试的限制

- 完整 service 包未通过：`TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog` 的 filtered page `RecursiveItemCount=99` 断言失败。已在未修改的 HEAD `79ea60c` 独立工作树复现相同失败，本次不修改无关分页逻辑。
- 完整 service 包达到 10 分钟测试上限；触发时正在 `TestHongGuoDownloadSourcePriorityAndFallback`，不能据此认定单测死锁。没有声称全量回归通过。
- 未执行生产数据库迁移、真实豆瓣网络/风控验证或产品服务端到端测试；本轮前端界面使用模拟 API 验证。

## 剩余约束

- 请求串行准入、去重和限速以单个应用进程为边界；多个实例同时运行时没有分布式请求锁。
- `doubanMatchFetchedAt` 对兼容手工构造的零时间 Match 回退当前时间；已核验的生产获取路径均携带原始时间。
- 保留旧豆瓣快照便于回滚，但切换后旧版本只会看到保留时的数据；旧副本不会继续同步更新。
- 新资料刷新任务默认关闭，每天检查到期数据；详情默认七天有效，降级详情最多一天有效。
- 未提交、未部署；测试数据库、基线工作树和临时测试日志已清理，原有未跟踪 core 文件未改动。


## 管理页面验收（2026-10-11）

用户 `ok` 批准后新增管理空间“资料中心”入口（`/admin/data-center`）及三条管理员 API；列表和详情只读共享数据库，单条更新只刷新共享资料/评分。

- 真实 PostgreSQL 16 下执行 `go test -race ./internal/repository ./internal/service ./internal/handler -run 'DoubanManagement|AdminRouteSurfacesAreRegistered' -count=1 -timeout=3m`，三个包全部通过。
- 仓储覆盖标题/精确 ID/类型、分页、字面 `%_`、注入式关键词、subject/data 年份、失败占位和无完整 JSON 投影。
- 服务覆盖新鲜缓存强刷、实际远端类型、读取不联网、canonical 数据不变、缺失行不联网、失败保留评分/成功时间、降级后的实际存储读回、历史只读及无成功资料详情。
- 真实注册路由覆盖三个接口的未登录 401、普通用户 403、管理员进入业务及服务未装配的 503；验证非法分页、类型、关键词长度与 UUID 的 400。
- Web `npm run lint`、`npm run build` 通过；`go vet` 覆盖 model/database/repository/service/handler，`git diff --check` 通过。
- `node scripts/check-douban-data-center.mjs` 在独立 Vite 服务和命名浏览器会话中通过：管理员入口/访问、URL 规范化、搜索/类型/分页、未获取/完整/降级/摘要资料、空态、详情与折叠 JSON、成功/失败/降级反馈、防重复、读取取消和刷新中账号切换取消。
- 深浅主题分别覆盖 390、768、1279、1280、1440 宽度，列表及弹窗无页面横向溢出；独立截图复核手机、平板详情和桌面表格。浏览器没有未捕获错误。自动化启用 reduced motion，并通过关闭按钮的键盘激活等待弹窗卸载，避免动态布局中的指针坐标竞态。
- 前后端分别经过独立只读复核，未发现阻断问题；生产 builder 为管理 Provider 配置共享仓储，HTTP 层负责分页边界。
- 未新增批量更新、JSON 编辑、删除、新 ID 或季集远端获取能力；没有改变此前底层刷新任务的默认关闭策略。
- 未进行真实豆瓣调用、线上 Cookie/代理/风控测试或生产迁移；完整 service 包仍保留上文已确认的基线失败/超时限制，本轮没有声称全量通过。
- 本轮测试数据库、Vite 服务、浏览器会话及临时脚本/截图/构建产物已清理；原有未跟踪 core 未改动。代码保持未提交、未部署。

## 追加完整审查（2026-10-11，修复前发现记录）

用户要求再次审查后，分别独立核验存储/迁移、Provider/调度、业务消费者和管理端跨层调用；主代理沿调用链复核，并通过临时 Go overlay 测试在 PostgreSQL 16 下确认以下两项 P2。没有修改业务代码。

1. `DoubanRepository.Save` 无条件更新 `last_attempt_at` 和 `next_retry_at`，其冲突条件只比较成功获取时间与质量。可在单实例复现：普通获取返回无类型剧集响应，较新的 TV 请求失败并创建 series 退避行，较早的资料随后通过 `persistProviderMetadata` 补录到实际 series 行。补录清空新失败的 `next_retry_at` 并回退 `last_attempt_at`。成功 payload 的时间门控与尝试/退避状态的时间门控必须分开，不能用较早成功覆盖较新失败状态。原测试覆盖迟到 payload 不覆盖新成功，但没有覆盖迟到成功与新失败交错。
2. `runDoubanDataRefresh` 在 `ErrDoubanTemporarilyUnavailable` 分支调用 `finish(nil, ...)`。实测 HTTP 503 后返回 nil，任务状态与阶段均为 `completed`，只有 `requested=1, scanned=1`，错误没有进入失败状态。应保留停止本批的行为，但传递错误并记录暂停/失败指标；补充执行历史状态的回归断言。原刷新测试只覆盖成功任务和缓存复用。

执行 `go test -overlay <临时映射> -race ./internal/service -run '^TestDoubanReview' -count=1 -timeout=3m`。两条用于验证上述契约的探测断言均失败，分别直接输出退避丢失和 `return=nil/status=completed`；这证明发现可复现，不表示本轮回归通过。测试只使用模拟豆瓣响应，没有真实远端请求或生产数据库操作。overlay、临时数据库和测试服务器已清理。

管理页面保留带原条目 ID 的操作错误，并未把该错误关联到新列表条目，未将这一展示选择列为 P2。此前记录的针对性通过结果仍有效，但未覆盖这两个新发现的场景；当时验收待修复。两项问题的修复验收见下文。

## 追加问题修复验收（2026-10-11）

- 正式新增 `TestDoubanSubjectSuccessPreservesNewerFailure`：分别验证失败占位和已有旧资料两种状态，迟到有效资料可入库但保留较新的尝试/重试时间，更新的成功能清除旧退避。
- 正式新增 `TestDoubanSeriesLatePersistencePreservesNewerFailure`：通过真实 Provider 获取、TV 请求 503、`persistProviderMetadata` 补录的调用链，验证获取时间与新失败状态同时保留。
- 正式新增 `TestDoubanDataRefreshUpstreamFailureIsFailed`：两个已绑定候选遇到第一个 503 即停止，返回临时错误，历史状态/阶段为 failed，记录 failed/upstream_paused/requested/scanned，旧评分/资料/获取时间与五分钟退避均保留。
- 三项测试在修复前执行均复现预期失败；最小修复后在真实 PostgreSQL 16 下执行 `go test -race ./internal/repository ./internal/service -run '^(TestDoubanSubjectSuccessPreservesNewerFailure|TestDoubanSeriesLatePersistencePreservesNewerFailure|TestDoubanDataRefreshUpstreamFailureIsFailed)$' -count=1 -timeout=90s`，两个包通过。
- 同一隔离 PostgreSQL 下执行 `go test -race ./internal/model ./internal/database ./internal/repository ./internal/service ./internal/handler -run 'Douban|TaskTracker|TaskDefinition|ScheduledTaskDefinitions|Scheduler' -count=1 -timeout=3m`，五个包通过，service 29.306 秒。覆盖既有质量保护、缓存去重/退避、管理刷新、旧补齐任务和新任务历史；数据库测试未跳过。
- `go vet ./internal/model ./internal/database ./internal/repository ./internal/service ./internal/handler` 与 `git diff --check` 通过。
- 两个独立只读核验分别复核存储 CASE 时间门控、真实补录路径，以及错误传播/任务定义/历史状态/批次停止，无新增缺陷。主代理点验关键门控和错误返回。
- 本轮未修改前端、未重复 Web 验收；未重跑存在已确认基线失败/超时的完整 service 包。真实豆瓣、Cookie/代理/风控和生产迁移仍未验证。
- 临时测试数据库已关闭，测试 fixture 服务均已关闭，未产生需保留的临时文件。原有 core 未改动。代码保持未提交、未部署。
