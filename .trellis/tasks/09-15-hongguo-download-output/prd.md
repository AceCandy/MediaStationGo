# 红果发现下载与分层文件输出

## Goal

从红果发现下载分集，可靠发布到按红果上线年月组织的输出目录，交由 CD2 和 Symedia 处理

## Discovery downloaded badge (2026-09-17)

- Discovery posters show 已下载 when any episode of the exact source work has a completed download record. This is historical download evidence, not Media ownership, whole-series completion or current file existence. Missing files do not remove the badge.
- Batch-project a boolean into existing list/rank/search responses. No per-card requests, filesystem checks, extra queues or database writes. Keep the badge clear of rating, group/episode footer and multiselect control; repeated search merges retain completed evidence.
- Verify completed/mixed/failed/queued/absent histories and nonexistent output files against isolated PostgreSQL, plus search/card/multiselect presentation and frontend checks.

## App source and episode diagnostics (2026-09-17, approved)

- Add App resolution to the existing download pipeline. New configurations default to App → fallback → official page; preserve saved first-source preferences and include all three sources. Highest compatible quality remains automatic, not a new setting or cross-source comparison.
- Expose safe per-source failures and selected quality/source plus verified dimensions/codec in the existing Download Space episode details. Preserve unknown values on legacy records. Never expose or persist URLs, signatures, device identifiers or media keys.
- Reuse bounded retries, isolated transfer/verification pools, original-source key recovery and no-overwrite publication. No production queue mutation, automatic retry, permission change or deployment.

## Hardware verification and inline group icon (2026-09-17, approved)

- User requests an on/off setting for the tested VAAPI decoding and a smaller group icon directly before the poster episode label. Keep current device ACL per user instruction; do not change service permissions or deploy.
- Default hardware verification off; optional boolean persists with existing settings, affects subsequent verification and preserves active work. Use fixed renderD128 and hardware frames, fallback once to software on error, never on cancellation. Probe/remux/hash/publication remain unchanged, and logs disclose fallback without raw tool output. Hardware and software corruption detection are not guaranteed equivalent.
- Group icon is transparent and 14px with a 24px interactive target, sibling of the main card button, in the same footer row as the episode label; retain hover/focus/tap portal and no accidental card action. Tests must cover non-overlap/keyboard close at viewport edges, settings compatibility/fallback/cancel, actual GPU opt-in, and existing download/group flows.

## Failed-work filter (2026-09-17, approved)

- Add an unchecked-by-default checkbox to list only works containing failed episodes. Filter before server pagination/counting; preserve complete episode summaries and expanded episodes. URL stores the filter, toggling resets page, stale responses cannot show the wrong result, and polling/retry removes resolved works and clamps pages.
- Scope: works query/handler, Web API/list, focused service/HTTP/browser checks; no download, cancellation or retry behavior changes. PostgreSQL race tests, targeted vet, Web lint/build and whitespace checks passed. Independent review was checked against source and passing mixed-status tests: the source-ID subquery correctly preserves all episodes, unlike an outer status filter. No deployment or production queue mutation.

## Configurable verification concurrency (2026-09-17, approved)

- Add verification concurrency to existing download settings, range 1–5, default 2. Preserve old requests that omit the field. Both pools apply saved limits without restart; increases admit waiting work, decreases let running work finish without cancellation.
- Scope: existing service config/dispatcher, Web config type/modal, HTTP/service/browser regression tests. No change to decoding strength, hardware acceleration, file recovery, download paths or source selection.
- Verification passed: isolated PostgreSQL download tests with race detector (including verification 2→1→3 and existing transfer 3→1→2), targeted vet, Web lint/build, download-space browser checks and diff whitespace checks. Independent read-only review found no issue. No production download, HDD performance benchmark or deployment; higher concurrency may increase disk contention. Temporary test services stopped after verification.

## Group visibility and download pipeline/UI (2026-09-16, approved)

- User requests vertical drag reordering in the group-confirmation modal; keep keyboard up/down controls and explicit confirmation. Discovery posters show 已关联剧集, with related source titles and assigned seasons on hover; provide focus/tap access for keyboard/mobile. Save success must visibly update the affected cards, independent of Media existence, without merging discovery cards or altering files.
- Compact each download episode into one normal row: episode, truncated path, concise progress/status and actions. Use distinct status text/icons/colors. Long path/error remains inspectable without permanently consuming an extra row. Completed work tasks receive an explicit success badge; do not imply upstream series is finished when only current tasks are complete.
- Sort episodes before pagination: downloading, verifying/publishing, failed, queued, cancelled, completed; stable episode/id ordering within each state. Replace native thick progress with a thin bottom gradient line and add work-level progress. Proposed work progress is completed task count / total tasks, clearly labeled, not fabricated byte-weighted or verification-time progress.
- Configured download concurrency should count transfer-stage work only; verification/publication must not consume its slots. Proposed verification concurrency is independently bounded at two, with excess waiting for verification rather than unlimited FFmpeg processes. Keep persisted recovery, cancellation/leases, source fallback, highest-quality choice and no-overwrite publication. Failed verification requiring another download must reacquire a transfer slot.
- Evidence: service/hongguo_download.go Start counts each full run until completion; hongguo_download_worker.go executeDownload switches to verifying only after copy/sync; repository/hongguo_download.go Claim resets unverified expired jobs to downloading. Splitting stages therefore requires explicit recovery handling, not just decrementing a UI counter. ListEpisodes currently orders episode,id; list/search projections omit group membership.
- Acceptance: pointer/touch reordering submits the shown season order; existing/new groups show related members without Media; 3 configured downloads can run while verification is busy; verification stays bounded, restart/cancel/fallback never duplicates or publishes unverified files; sorted pagination prioritizes activity across >50 episodes; compact rows/progress and completed badges work in both themes and narrow widths.
- Out of scope: changing real file hierarchy, uploading/STRM generation, automatic merging, unlimited verification workers or claiming fixed throughput under upstream/disk constraints. Main operational risk is growing temporary-file backlog when transfer is faster than verification.
- User approved the two-worker verification queue, row order and completed-task progress semantics. Waiting verification is shown separately, after active verification/publication and before failed transfers.

## Combined discovery search (2026-09-16, approved)

- Combine the official first search response with paginated, already collected local catalog matches. Deduplicate by source ID, preserving official ordering and preferring hydrated metadata; never claim full upstream coverage.
- Scroll loads local pages of 50 only, retaining selection and existing cards. Each source fails/retries independently, local retry stays on the failed page, and query/account changes abort and discard stale responses. Explicit repeat-search/refresh resets local pagination and reruns official search.
- Scope is frontend composition using existing search/list APIs. No guessed official pagination, new providers, extra automatic import, media creation or download changes.
- Evidence: public search for 万妖 returned 10 entries and totalCount=195; scrolling did not fetch another page, page=2/offset=10 returned identical source IDs, and the current official search bundle only renders loader searchList. Existing repository List already matches titles/source IDs and pages canonical works plus pending summaries.
- Acceptance: overlapping sources render once without replacing hydrated fields with a summary; >50 local results append; empty/failed source retains the other; failed page is retried without skipping or repeating official search; later loads preserve selection; refresh restarts page 1; stale responses do not leak across queries/accounts; scope notice is visible.

### Combined-search verification

- Reused the existing local list and official search endpoints, with independent abort/retry state and source-ID merging. Local next-page requests never call official search, and explicit repeated search restarts local page 1. Category/rank refresh behavior is unchanged.
- `npm --prefix web run lint`, `npm --prefix web run build`, `git diff --check`, `check-hongguo-search.mjs`, `check-hongguo-discover.mjs` and `check-hongguo-batch.mjs` passed. New mocks cover 10 official plus 52 local matches with overlap, hydrated precedence both directions, retrying page 2, retaining selection, source-specific retry/empty/failure, delayed old-query completion and dual-theme/four-width source notices. Screenshots wait for finite theme transitions to complete.
- Independent read-only review and main-thread review completed. Browser tests use mocked responses, not production catalog writes. No backend changes, database integration run, deployment or real full-result coverage claim. The reused live offset pagination is not a frozen snapshot; concurrent catalog changes may alter ordering/membership, requiring a fresh search.

## Discovery multiselect (2026-09-16, approved)

- Administrators can select hydrated discovery cards for download or logical season grouping. Selection order defines initial seasons; a modal shows posters, names and seasons with up/down controls and an editable group title before submission.
- Reuse existing enqueue and create-group APIs. Download results distinguish accepted works/new episode tasks from failures; retain failed selections. Missing download configuration prevents enqueue. Pending summaries remain read-only; movies cannot become seasons and existing group membership is never overwritten.
- Selection persists through incremental loading but resets with account/filter/search changes or exit. No media creation, file movement, automatic title-based season inference, new provider or queue.
- Acceptance: keyboard-operable selection, duplicate toggle prevention, ordered group payload, cancel without writes, partial download failure feedback, missing-config rejection, admin isolation, and responsive modal. Existing discovery detail behavior remains unchanged outside selection mode.

### Verification

- Implemented only discovery selection/card semantics and a batch-action/modal component, reusing existing APIs and ModalShell without backend changes or dependencies.
- Web lint/build, existing `check-hongguo-discover.mjs`, and new `check-hongguo-batch.mjs` passed. Batch checks cover ordered payload, movement, cancel, failed group draft retention, partial enqueue/retry, missing root, disabled pending/movie actions, delayed request completion after route unmount, admin isolation and four viewport widths in both themes. Mobile screenshots were visually reviewed.
- Independent read-only review plus main-thread follow-up completed; corrected successful-save focus to the selection count because the create-group button becomes disabled after clearing selection. Added the delayed-unmount regression identified as missing coverage.
- Browser APIs are mocked; this round does not verify database persistence, real downloads or cloud workflows. Existing backend validation was inspected, not modified. No commits/deployment/archive; preview/browser sessions stopped and temporary screenshots removed during handoff.

## 并发与接口优先级增量（2026-09-16，用户已确认实施）

- 用户要求在现有设置弹窗配置并发下载数量、下载接口优先级；画质默认最高，不增加画质选择项。
- 并发范围 1–5，默认 3；并发槽涵盖单集下载、处理、校验及发布。降低数量不打断正在处理的分集，后续领取遵守新上限；接口顺序变更用于后续取流，已开始传输不重下。
- 当前只有官方播放页和备用解析两个来源，提供官方优先/备用优先两种顺序；不允许填写任意接口 URL，不新增来源。优先来源无法完成有效下载时尝试另一来源，本地磁盘错误、取消不应触发换源。
- 最高画质指当前所用来源返回的最高可用档位，不跨接口比较。官方 parseDownloadPage 仅读取 main_url（规划时 internal/hongguo/download.go:93）；现有解析材料没有可确认的官方多档协议，沿用唯一地址，不声称全站最高。备用解析按档位数字选择最高有效地址与密钥。
- 验收：设置持久化及输入校验；并发不超限、不重复领取，取消/恢复/完整性校验仍有效；两个优先顺序及失败回退可测试；多档响应选择最高有效档位，单档正常下载。
- 风险：并发增加源站限流和 CPU/磁盘压力，第三方接口可用性不受本项目控制。此次不做断点续传、多连接分段、qBittorrent、跨接口画质竞选或外部上传。

## 下载空间按作品管理（2026-09-16，用户已确认实施）

- 删除内容区重复的下载空间标题和说明；红果短剧使用发现页相同的下划线子导航样式。
- 存储设置收进设置按钮打开的弹窗，保留目录校验、临时/完成路径及备份提示；取消不保存。
- 按作品而非分集分页，默认折叠。作品显示总任务数及各状态数量；展开后按分集顺序加载子列表并保留单集操作。
- 作品级“重试失败集”覆盖整部作品所有失败任务，不受分集分页影响，不重试已完成、运行中或已取消任务；不创建重复任务，不更改作品固定路径。
- 本轮仅检查并报告其他子 Tab 差异，不擅自改动其他页面。已发现 TasksPage.tsx:707 与 DiscoverHubPage.tsx:24 的间距/悬停差异，HongGuoPage.tsx:142 的分类/榜单仍为主按钮风格；分类标签属于筛选项，不一概改成 Tab。
- 验收：超过 50 集的一部剧不在作品列表中被拆开；折叠/展开及轮询保持稳定；批量重试只处理该作品失败分集；设置弹窗保存/取消与未配置状态可用，窄屏不溢出。
- 不增加上传、整理、自动追更、删除文件或整剧取消；没有数据库迁移需求。用户已确认此增量，不影响此前已完成改动。

## 已确认范围

- 红果发现提供作品下载入口；文件空间增加下载空间，其中提供红果短剧 Tab。
- 本项目负责取流、持久化分集任务、进度、取消、失败重试及重启恢复，使用 FFmpeg 执行媒体下载，不接 qBittorrent。
- 配置下载存储位置，明确展示临时目录与完成输出目录；CD2 仅备份完成输出目录。
- 临时下载隔离于备份范围；成功校验后发布正式文件，发布失败不能标记完成。不得覆盖已有目标文件。
- 日期完整时输出 年/月/作品；只有年份时输出 年/未知月份/作品；年份未知时输出 未知年份/作品，不再增加未知月份层。
- 作品目录保留 [hongguo-作品ID]；剧集使用 Season 01 与 S01E001 等源季集坐标。同一作品首次确定目录后，重试、补集沿用该目录。
- 不自动将下载视频入库，不上传云盘，不生成 STRM，不因下载完成删除正式文件。CD2 备份和 Symedia 生成 STRM 由用户在外部配置。
- 最终 STRM 交给现有红果媒体库扫描绑定，交接时保留作品 ID 与季集编号。
- 当前不增加外部下载目录轮询整理、硬链/移动选择或自动追更。

## 已核对事实与技术风险

- internal/hongguo/client.go:75 明确区分上线时间与首播时间；Work 只有 FirstVisibleAt，ParseDetail 在 :488 读取 first_visible_time。
- internal/model/hongguo.go:46 保存 FirstVisibleAt，没有已确认的上映日期字段。参考 juku 的红果实现中也未找到可直接使用的上映日期。
- docs/cankao/juku/internal/app/provider_hongguo.go:192 提供第三方、网页、App 取流回退；真实网络可用性、分集正确性尚未验证。
- internal/hongguo/path.go:11 支持明确红果标签；internal/repository/hongguo_media.go:64 使用源作品 ID 和源季集坐标绑定。
- 单凭文件存在、大小阈值或下载进程成功不足以证明完整；需媒体结构检查，并在有可信来源时长/大小时比对。缺少源校验和不能声称字节级完整。
- 临时输出到完成目录的原子发布要求同一文件系统，不能只根据路径层级判断。

## 验收标准

- [x] 发现页可发起对应作品下载，重复发起不重复下载已完成分集。
- [x] 下载空间展示持久化任务状态，支持取消、失败重试和重启恢复。
- [x] 完整日期、仅年份、无年份三种目录输出符合约定，补集不因日期变化换目录。
- [x] 未完成或校验失败文件不会出现在 CD2 备份的完成目录中。
- [x] 已完成文件不会被覆盖或被自动清理，视频下载不会触发本地视频入库。
- [ ] 验证代表性真实分集的取流、下载和媒体检查；若环境无法验证则明确报告，不声称通过。
- [ ] 用符合交接命名的 STRM 样例验证现有红果身份和季集绑定。

## 已确认的日期语义

- 用户已批准使用红果上线年月，按北京时间归档；不宣称为上映或首播日期。
- 当前来源只有完整时间戳或未知，只有年份的规则保留为目录函数的明确契约，不伪造月份。
- 用户已明确批准实施本任务。

## 实施验证记录（2026-09-15）

- 已实现独立持久化队列、管理员下载空间/详情入口、隔离下载、校验与无覆盖发布；代码与下载边界规范均保留未提交。
- 隔离 PostgreSQL 上下载专项测试及竞态检查通过；FFmpeg 本地生成视频的下载/校验/发布通过；HTTP 身份与管理员权限测试通过。
- `node web/scripts/check-download-space.mjs` 通过：设置、重试、未保存输入保持、详情入队、普通用户隔离、两种主题和四档宽度。测试详情接口的前缀匹配需将媒体子路由置于详情路由之前。
- `npm run lint`、`npm run build`、`git diff --check` 通过。独立审查后又复核目录标签冲突与真实测试租约维护，并重跑下载专项和竞态检查。
- 扩展回归的 `TestHongGuoDiscoveryScansUntilCategoryEnd`、`TestHongGuoListOrdersByFirstVisibleBeforePagination`、`TestHongGuoArtworkOwnershipMigration` 存在旧失败，已在干净基线 ce0f3a6 复现，未修改无关逻辑。
- 真实来源测试未通过：环境 DNS 返回 198.18/15 fake-IP 保留地址，公共下载连接拒绝；未降低网络安全限制。当前解析支持网页和 juku 第三方回退的完整 MP4，不含 App 回退/HLS，不能宣称真实分集下载已可用。
- 分层 STRM 已验证来源 ID 解析；完整扫描绑定、CD2/Symedia 联调、真实部署迁移尚未验收。无来源校验和时不能承诺来源字节级完整。
- 测试容器、预览服务和临时基线工作树仅用于本次检查，收尾时清理；真实源验收未完成，任务保持 in_progress，不提交、不归档。

## Fake-IP 下载修复验证（2026-09-16）

- 用户批准修复下载失败。域名解析为 `198.18/15` 时通过固定公网 AliDNS DoH 重新取得真实地址；仍拒绝私网/保留地址，不启用代理、不放开 Fake-IP、不修改全局网络策略。
- 保留脱敏后的 DNS、连接、超时、TLS 与重定向错误类别，不记录签名地址和密钥。
- `go test -race ./internal/hongguo`、`go vet ./internal/hongguo`、下载目录与 HTTP 权限专项测试、`git diff --check` 通过；独立复核确认 IP 字面量不触发回退，DoH 有界、校验证书、禁止重定向且失败关闭。
- 显式开启 `MEDIASTATION_TEST_HONGGUO_DOWNLOAD_LIVE=1` 后，`TestDownloadFakeIPLive` 验证万妖图录传第一季 E001：完整读取 9,429,999 字节，FFmpeg 全量解码通过（8.35 秒）。测试临时视频自动清理，未写实际任务队列或正式输出目录。
- 本轮未提供 PostgreSQL 测试 DSN，队列/发布集成测试跳过；未验证全剧、CD2/Symedia、线上重试或部署。未启动调试服务，不提交、不部署、不归档。

## 按作品管理实施验证（2026-09-16）

- 已实现作品汇总分页、展开分集分页和整部失败重试；复用单集来源校验，保留旧接口、固定目录和租约机制。页面改为下划线子导航、设置弹窗和作品折叠列表。
- 临时隔离 PostgreSQL 上 `go test -race ./internal/service ./internal/handler -run HongGuoDownload -count=1` 通过，覆盖 55 集汇总、52 部作品分页、缺资料跳过、重复重试、状态与来源隔离，以及原有取消/恢复/校验发布行为。
- `go vet ./internal/service ./internal/handler`、前端 lint/build、`git diff --check` 通过。`check-download-space.mjs` 通过管理员/普通用户、折叠懒加载、批量及单集重试、分集分页、设置保存/取消、草稿保持和 390/768/1024/1440 四档双主题检查；人工复核桌面列表和移动端弹窗截图。
- 独立复核确认失败行锁、部分跳过计数、旧 API 兼容、展开轮询稳定及弹窗焦点返回。补全浏览器测试缺失的发现列表模拟响应，未改发现页业务逻辑。
- 其他页面子 Tab 差异保留为上文报告，不修改。未进行线上部署、全剧真实下载或 CD2/Symedia 联调；临时数据库/预览服务/截图在交付前清理，不提交、不归档。

## 并发与接口设置实施验证（2026-09-16）

- 现有弹窗支持并发 1–5（默认 3）和官方/备用优先；单实例有界调度，降低并发排空运行任务，保存后唤醒。配置键批量原子保存，旧 root-only 请求保留新字段，既有目录/完成文件不变。
- 来源回退贯穿解析、HTTP 和校验，最高档保留于当前所用接口；官方没有可验证的多档字段，沿用 main_url。工具错误仅明确媒体损坏诊断换源，未知错误安全停止，避免错误重下；不记录 FFmpeg stderr 或取流密钥。
- 隔离临时 PostgreSQL 上 `go test -race ./internal/hongguo ./internal/service ./internal/handler -run 'HongGuoDownload|TestDownload' -count=1` 通过，数据库测试未跳过；覆盖动态并发 3→1→2、停止恢复、取消后立即重试的新旧租约隔离、两个优先顺序、403/截断/损坏回退、最高有效档位和设置 HTTP 兼容校验。另 `go test -race ./internal/hongguo -count=1` 与定向 go vet 通过。
- Web lint/build 和 `check-download-space.mjs` 通过；校验新字段请求、静态服务端响应回填、无效并发阻止提交、取消与脏表单、下拉菜单 Escape、四档双主题布局。真实持久化由 HTTP/数据库测试覆盖，不通过重复注册静态浏览器路由假装配置保存。截图等待弹窗入场结束后检查，避免把过渡帧当稳定样式。
- 只读独立核验后主线程复核：取消已有 2 秒心跳停止机制，不新增取消注册表；补测立即重试竞争，收紧未知 FFmpeg 错误分类。复用现有 Select/ModalShell，无新增依赖。最终检查含未跟踪文件的空白检查。
- 未进行真实源站并发测速、全剧或 CD2/Symedia 联调；不提交、不部署、不归档。调试 Vite、隔离数据库和浏览器会话关闭，六张临时截图删除，不触碰实际下载队列或现有视频。

## 补充下载迁移任务中心并支持定时（用户已确认）

- 用户纠正入口归属：执行记录仍在下载空间设置旁，补充下载迁至任务中心，并支持定时执行。此前一次性按钮范围由本节新需求取代，不改变候选和下载规则。
- 现有 `SchedulerService.configuredJob` 支持持久化开关/周期与手动运行互斥（`internal/service/scheduler.go:110`、`internal/service/scheduler_runner.go:123`）；`TaskScheduleDialog` 已有周期设置（`web/src/pages/TasksPage.tsx:279`），应复用而非另建调度器。
- 独立“红果补充下载”任务，配置每轮数量 1–100 部和执行间隔；默认关闭，初始数量 10、间隔 24 小时。手动执行可指定本次数量，不覆盖定时数量；每轮最多新增该数量，不代表保持固定下载中数量。
- 保留下载执行记录查看入口；任务中心新任务记录每轮选取/入队汇总，不能把入队完成显示为视频下载完成。未配置目录/红果禁用不能入队；继续排除既有记录、失败/取消、资料不齐，避免跨手动/定时重复入队。
- 用户以“ok”批准默认配置和实施；不增加自动追更、上传、STRM 或运行中队列水位策略。

## 下载记录入口与按数量补充下载（用户已确认）

- 用户确认两个功能都做：设置旁新增执行记录按钮并弹窗展示，不放在分集详情；任务中心不再展示红果下载入口。另新增输入作品数量后挑选未下载作品入队的按钮。
- 复用真实执行历史，不拿分集状态列表替代执行历史。`TaskTrackerService.DefinitionHistory` 依赖任务定义（`internal/service/task_definitions.go:189`），隐藏入口不得破坏历史读取，不删除底层记录。
- 发现目录默认首播时间倒序（`internal/repository/hongguo_repository.go:204`），包括未补齐摘要；`Enqueue` 只接受已有作品/分集资料，来源 ID/集号冲突跳过（`internal/service/hongguo_download.go:211`）。
- 按源作品数量，从本地发现目录按现有默认顺序选择资料可下载且从未有下载记录的作品；失败/取消保留手动重试，不自动重下。仅本次提交，不加周期任务、官网抓取、补资料、上传、STRM 或 Media 入库。
- 验收：执行历史分页查看阶段/时间/结果/错误；任务中心入口消失；数量有界校验、缺目录不入队、重复及并发请求不重复创建分集；候选不足和部分失败如实反馈实际新增数量。
- 用户以“ok”确认上述规则；每次请求上限 100 部，资料齐全指存在 1–10000 条有效来源分集，不等同于上游全剧完结。并发争抢或单部失败可能少于请求数量，按实际结果反馈。

## 执行记录与补充下载验证（2026-09-17）

- 已完成设置旁两个按钮、执行历史分页弹窗、任务中心隐藏下载定义但保留历史/日志、按数量首次入队。无数据库结构变更，无新增依赖。
- 隔离 PostgreSQL 上 `go test -race ./internal/service ./internal/handler -run 'HongGuoDownload|TaskDefinition' -count=1` 通过，下载相关测试未跳过；定向 go vet、Web lint/build、diff 空白检查通过。覆盖所有既有状态排除、数量边界、缺目录、无效分集、顺序、并发首次入队、历史保留、HTTP 权限及请求体边界。
- `check-download-space.mjs` 通过：记录分页/错误内容、补充请求参数、0/101 阻止提交、失败输入保留、候选不足/跳过/部分失败展示、Escape 返回和 Tab 循环，以及原有设置/重试/普通用户隔离/响应式回归。人工查看亮暗主题手机补充弹窗截图。
- 独立后端/前端只读复核未发现阻断问题，主线程复核关键事务和调用链。测试对照任务定义数量减一；记录分页断言等待实际请求完成，避免将状态更新当成请求完成。
- 未验证生产部署、真实全剧批量下载、极大目录查询性能或 CD2/Symedia。逐作品提交不提供整批原子性；请求丢失响应时可能已有部分入队，先查看下载空间再继续。测试数据库、预览服务和截图在交付前关闭/删除，不提交、不部署、不归档。

## 聚合交互与独立校验历史记录（2026-09-16）

- 聚合支持鼠标/触摸拖拽和键盘调序；发现列表批量读取关联 ID，封面按需展示关联成员及季号，不依赖 Media。保存成功的关联覆盖迟到的旧列表结果，显式刷新恢复服务端数据为准。
- 分集改为紧凑单行、彩色状态和底部细渐变进度，作品显示当前任务完成比例与全部完成标记；状态优先排序发生在数据库分页之前。
- 下载与校验独立调度，配置下载数不包含等待/运行校验，校验固定上限 2。持久化 raw 检查点、原来源及必要参数，不持久化 URL/密钥；恢复校验使用新租约独立 stage，保留无覆盖发布及取消后立即重试隔离。
- 临时 PostgreSQL 的下载、搜索关联、HTTP 专项 race 测试通过；覆盖 3 个传输同时运行 2 个校验、等待校验不抢槽、停机恢复不重复传输、租约隔离、分页优先序。定向 go vet 通过。下载测试连接池改为连接参数携带隔离 search_path，避免取消 SQL 后重连丢失测试 schema；重复 5 次竞态测试通过。
- Web lint/build、已跟踪与未跟踪文件空白检查通过；下载空间、发现与搜索脚本通过。多选脚本连续 3 轮及额外截图轮通过，包含原生鼠标/浏览器触摸仿真、键盘进入关联浮层、两主题响应式、旧列表不能抹掉保存关系。修正新增等待断言返回 DOM 而非布尔值导致的浏览器序列化错误。
- 已完成独立只读复核并修正浮层键盘访问；主线程复核最后的保存关系覆盖与焦点逻辑。早期浮层偶发关闭在后续上述重复检查未再复现，不能据此承诺所有真实设备均无问题。
- 未验证生产迁移部署、真实来源全剧并发、断电/强杀注入、真机触摸及 CD2/Symedia 联调。下载快于校验时临时文件可能积压；加密 raw 恢复仍依赖原来源重新提供密钥。测试容器、预览服务及截图在交付前清理，代码保持未提交，按收尾规则不归档任务。
