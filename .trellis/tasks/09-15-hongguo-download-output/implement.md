# 实施与验证

## App source and episode diagnostics (2026-09-17)

- Reused resolver, safe transport, scheduler, key recovery and existing episode details. Added signed App POST and compatible highest-quality parsing; new default App/fallback/official preserves stored priorities. Three-source retry budget is nine across transfer/verification. No production settings/queue mutation or deployment.
- Added safe public metadata/error columns through the existing model migration. Each failed source retains its latest safe reason; manual retry clears history. FFprobe replaces source metadata with actual dimensions/codec/short-side quality on verified publication; old records remain unknown.
- Isolated PostgreSQL targeted race tests, vet, Web lint/build and download-space browser checks passed; browser tests were updated to assert complete three-source labels rather than retired two-source text. Independent read-only review found no blocking issue; primary review additionally bounded upstream dimensions and nonfinite duration.
- Live App episode 23 returned 1080p, 1080×1922 HEVC, 6,329,053 bytes and passed full decode. The separate isolated service live check also passed transfer, original-source key re-resolution, decryption, verification, publication and persisted attributes. Temporary files are test-cleaned; this does not certify all episodes, future upstream availability, production migration or cloud workflows.
- Scope remains uncommitted; finish-work archiving is deferred. Test preview/database are stopped at handoff; no user service shutdown or permissions change.

## Hardware verification and inline group icon

- Implemented existing config/worker/UI paths only, no dependency/schema/permission changes. Added deterministic fake-command checks plus opt-in read-only VAAPI verification of a completed HEVC file. Isolated PostgreSQL download race tests, live GPU verification, Web lint/build, vet and download-space/group/discovery browser checks passed. Mobile and desktop screenshots reviewed. User's existing ACL retained; isolated database, preview and temporary screenshots cleaned at handoff. No deployment, production configuration change or all-codec/driver corruption-equivalence claim. Work remains uncommitted, so finish-work archive is deferred.

### Bug analysis: bottom-edge portal reopening

1. Root cause (implicit assumption/change propagation): old portal top-clamping assumed a top-positioned trigger. Moving it near the viewport bottom made the panel overlap its trigger.
2. Failed attempt: waiting for React state did not solve Escape reopening; measured rectangles showed overlap, distinguishing geometry from timing.
3. Prevention: choose above/below, constrain height and assert panel/trigger non-overlap in browser checks. No arbitrary delay or hover suppression.
4. Scope: keep this fix inside the moved group badge; do not refactor unrelated popovers.
5. Capture: frontend discovery spec updated. Independent review claims about nil TaskHandle and sibling-button event bubbling were checked against the nil-safe Update method, JSX ancestry and passing tests; no unnecessary guards added.

## Failed-work filter verification (2026-09-17)

- Added server-side source-ID filtering before count/pagination and an unchecked-by-default URL-backed checkbox; full episode summaries and expanded lists remain intact. Toggling resets pagination and response identity prevents stale filter results.
- Isolated PostgreSQL service/HTTP race tests, targeted vet, Web lint/build and diff checks passed. Browser regression passed twice consecutively after correcting an unsupported checkbox test command; one intermediate run timed out toggling the filter. Production queue state and deployment were not touched. Independent review's aggregate concern was rejected using the source-ID predicate and passing mixed-status assertions. Preview and isolated database stopped at handoff; no commit/archive.

## Task-center scheduled supplement (approved)

验收记录（2026-09-17）：隔离 PostgreSQL 上下载/补充/Scheduler/任务定义/HTTP 专项 `go test -race` 通过；`go vet ./internal/service ./internal/handler`、Web lint/build、补充任务/下载空间/任务日志三个浏览器脚本及 `git diff --check` 通过。独立前后端复核后修正任务定义数量断言和可选仓储读取保护。周期输入沿用现有 UI，后端仍校验整数和范围；未扩大修改通用周期表单。未执行生产下载、真实 24 小时等待、云盘链路或部署。定时为每轮新增，启用者需关注磁盘和队列积压。测试预览已关闭，隔离数据库关闭清理；未提交或归档。

1. Inject download service, register default-off job, persist quantity atomically with existing schedule fields, pass manual-only quantity via invocation context, and record batch results.
2. Move the quantity dialog to task center; retain history in Download Space and use existing schedule UI with the job-specific count field.
3. PostgreSQL/race tests: defaults, restart persistence, invalid updates without mutation, manual quantity isolation, real timer dispatch using shortened initial test delay, same-job overlap rejection, zero candidates/missing root/disabled source, shutdown cancellation and join. HTTP quantity/config validation and existing task authorization remain required.
4. Browser checks: moved entry, manual cancel/error/range and request body, periodic defaults/quantity and payload, history regression, responsive themes. Independently review; run lint/build/vet; stop test services and remove screenshots. No commit/deploy.

## Execution history and one-shot supplement (approved)

1. Add bounded supplement selection and reuse first-time enqueue protection; hide task-center definition without removing its history registration.
2. Add settings-adjacent history/supplement buttons and accessible dialogs, reusing existing API client and ModalShell.
3. Run PostgreSQL race tests for ordering, every existing status, missing root, invalid candidates/counts, concurrent enqueue and retained history; run HTTP authorization/body validation.
4. Run Web lint/build and download-space browser regression for history paging, actual payloads, errors, insufficient candidates, keyboard and responsive layouts; independent read-only review then primary verification.
5. Record checks/limits; stop isolated database and preview, remove test screenshots. No commit, deployment, cloud integration or production queue mutations.

## Group visibility and split pipeline (approved, implementation)

1. User confirmed verification bound and presentation semantics; stage/recovery contracts and tests were read before implementation.
2. Add drag reordering and bounded membership projection/hover presentation; verify persistence, season payload and account isolation.
3. Separate transfer/verification admission and durable recovery; isolated PostgreSQL/FFmpeg race tests must prove 3 transfers plus bounded verification, cancellation, shutdown/restart and failed-verification source fallback.
4. Add server-side episode priority and whole-work progress; implement compact colored rows and thin progress; test >50 episodes, completion semantics and dual-theme responsive/touch/keyboard interaction.
5. Independently review, update specs, clean private/debug artifacts and stop services. Do not submit/deploy/archive without authorization.

## 发现海报历史下载标识（2026-09-17）

- 分类、榜单、搜索共享批量查询：同 source_id 任意分集存在 completed 记录即返回 downloaded，不读取文件、不依赖 Media、不表示全剧完成。删除完成记录后不再作为下载依据。
- 海报右侧第二行显示绿色「↓ 已下载」，避开评分、待补齐和多选圆圈；本地和官网搜索合并保留任一响应的完成标识。重新加载列表时更新，不额外轮询下载接口。
- 新增数据库回归覆盖各状态、同名不同源、多条完成、混合失败、待补齐摘要及文件不存在；补齐相关测试迁移，排序测试 fixture 补合法 Kind，不改排序逻辑。
- 定向 PostgreSQL race 测试、go vet、Web lint/build、搜索与发现浏览器回归通过；未部署、未重启用户服务，未操作生产下载任务。

## Combined discovery search

1. Compose official and local queries in HongGuoPage without backend/API changes; preserve hydrated precedence, error isolation, selection and modal behavior.
2. Add browser search pagination/failure/cancellation checks and adjust earlier discovery mocks/expectations for the additional local query.
3. Run lint/build, search/discovery/batch regressions and independent review; record scope and remaining live-catalog risks, stop preview and clean screenshots. No commit/deployment.

## Discovery multiselect

1. Add administrator selection state/card semantics and batch actions, keeping ordinary detail opening intact.
2. Reuse enqueue/config/create-group APIs; verify failure retention, explicit ordering and existing group rejection.
3. Extend browser regressions for selection, downloads, grouping, cancellation, permissions and responsive themes; run Web lint/build and diff checks.
4. Independently review the change, update discovery contracts, and stop preview services. Do not deploy or commit.

## 并发与来源顺序增量（用户已确认实施）

1. 用户确认最新方案后，读取相关规范并检查配置调用方、ClaimHongGuoDownload 的锁/租约及来源响应测试材料，确认不破坏原有恢复机制。
2. 扩展配置存取和设置弹窗；实现有界并发调度与来源优先/回退，保留单集校验发布，不引入新下载服务。
3. 补充配置边界、动态并发升降、去重领取、顺序/回退及最高档选择测试；隔离 PostgreSQL 上运行下载专项竞态测试，运行前端 lint/build 和设置弹窗浏览器检查。
4. 独立复核取消、关闭、旧配置兼容、错误分类与脱敏；同步规范并说明真实源验证边界，关闭调试服务。不提交、不部署。

## 按作品管理增量（已确认实施）

1. 补充作品汇总/分集分页/批量失败重试查询与管理员接口；验证跨 50 集分页、来源隔离、状态过滤、部分失败和重复操作。
2. 修改下载空间 UI/API 类型，复用发现子导航、设置弹窗、作品折叠子列表；保留单集操作和账户切换隔离。
3. 更新下载空间浏览器检查，验证弹窗保存/取消、作品展开、批量重试、轮询及移动端；运行定向 Go 测试、前端 lint/build 和 diff 检查。
4. 独立复核并同步规范；缺少测试数据库时明确报告未验收项。不改其他页面，不提交、不部署。

1. 增加源媒体解析与安全下载、分层和发布函数；独立测试身份、年月、完整性及覆盖保护。
2. 增加独立作品下载记录、分集业务队列、租约恢复、取消和失败重试，接入启动/关闭与任务日志。
3. 增加管理员下载 API、下载空间页面及红果详情下载按钮，保留导航与账户权限约束。
4. 运行针对性 Go 测试、Web lint/build、diff 检查；验证实际取流条件，独立复核数据/文件安全及 UI。
5. 更新任务记录与红果隔离规范。不提交用户代码，不部署、不连接云盘，不删除已有输出。

回滚：移除新增入口及服务启动即停用下载；保留独立业务表和已输出文件。旧下载功能退役迁移不能删除新队列表。
