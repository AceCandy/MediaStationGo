# 红果下载设计

## App source and episode diagnostics (approved, supersedes two-source rules)

Extend the existing resolver/worker and priority enum, default App → fallback → official page while preserving stored first choices. Other sources follow App/fallback/official excluding the selected first source. Advance after the last attempt, bounded at nine across transfer/verification. Original-source key recovery stays unchanged. Signed fixed-endpoint App requests reuse safe network transport and reject redirects; no credential persistence. Highest compatible quality is selected within one source, not across sources.

Add nullable safe metadata fields and JSON-serialized per-source failures to the existing download model using AutoMigrate. Transfer resets stale fields; successful verification replaces dimensions/codec/short-side quality using the existing FFprobe invocation. Manual retry clears failure history. Reuse the existing episode details, no new screen. Rollback leaves completed files untouched; no deployment or production configuration migration is performed during development.

## Task-center scheduled supplement (approved, supersedes one-shot UI placement)

Register a distinct hongguo_download_supplement definition/job on the existing SchedulerService. Default off, 24 hours, 10 works; persist count with interval/enabled atomically and return it only on that schedule config. Manual execution passes count in invocation context, never saved settings. Both paths use the scheduler running slot and the existing Supplement method; retain a service mutex for compatibility with the legacy direct endpoint. Keep per-episode hongguo_download histories hidden from task-center definitions.

The new batch execution records requested/candidate/added/failed/skipped counters and an enqueue-only summary, with manual/scheduled trigger. Link even detached manual work to scheduler lifecycle cancellation; Stop rejects new supplement runs, cancels and joins admitted supplement work before return. Other scheduler jobs keep existing behavior.

Remove Download Space's supplement UI and unused client wrapper, retain its execution history dialog. Task center opens a quantity dialog for manual run; its existing schedule dialog gets a count field only for this job. Legacy direct HTTP endpoint remains compatible and does not configure periodic behavior. No schema migration or automatic schedule enablement.

## Execution history and one-shot supplement (approved)

Keep the task definition registered for history/log lookup, but omit it from task-center definition enumeration. Download Space loads its existing paginated execution API in a ModalShell; histories remain observational and never determine download candidates.

POST downloads/supplement accepts 1–100 source works. Filter local canonical works before LIMIT by existing discovery ordering, excluding comic, invalid identities, absent/invalid episode rows, and any placement/episode download history. Reuse Enqueue through a private only-new mode: placement INSERT ON CONFLICT inside the existing per-work transaction elects one first-time enqueue; manual enqueue keeps supplement-episode compatibility. Return selected candidates, actually created works/episodes, skips and failures. No scheduler, migration or new dependencies; already committed works survive later per-work failures or request interruption.

Rollback removes the new endpoint/buttons and restores task-center visibility; existing downloads and execution records remain untouched.

## Group visibility and split pipeline (approved)

Reuse the installed motion package for vertical drag where supported; keep explicit handles, scrolling and keyboard reorder. Add minimal group membership to list/search projections in bounded queries, use the existing group-detail read for related members on demand, and refresh changed memberships after creation. Discovery retains source-work identity and requires no media rows.

Separate transfer admission from bounded verification/publication admission. `waiting_verify` and private raw-size/source/duration/encryption-flag/attempt fields persist the handoff; only two verification workers are claimed, with no goroutine per waiting file. Raw files, stage and parent directory entries are synced before handoff. Encrypted files re-resolve the original source's key when verification starts: signed URLs/keys remain ephemeral, so source unavailability or a changed key may require fallback/redownload. Each verification lease hardlinks immutable raw bytes into its own stage before updating the checkpoint, avoiding old-worker cleanup races. Re-download after source validation failure reacquires transfer admission; six alternating source attempts remain bounded across stages. The temporary-file backlog can grow if verification is slower.

Service summary owns whole-work completion counts/progress; episode priority ordering is server-side before pagination. Web renders compact rows, status colors/icons and accessible thin progress indicators, with paths/errors available on demand. Rollback must retain existing completed files and staged-task data rather than discard work.

## Combined discovery search

Reuse `hongguoAPI.search(keyword)` once per explicit search and `hongguoAPI.list(keyword,'','','',page)` for local pages. Independent effects/controllers and retry counters prevent one failure or pagination from repeating the other source. Merge source-ID keyed cards with hydrated precedence and first-seen positions. Keep existing category/rank pagination; block the observer during errors/in-flight loads and disconnect before advancing. Local results retain existing live offset pagination semantics, not a frozen database snapshot; concurrent catalog mutations can change matches, and explicit refresh restarts the result set.

## Discovery multiselect

Keep selected work snapshots in the existing query/account-keyed discovery content. A small batch-actions component owns submission and the season-order modal; reuse ModalShell and existing authenticated APIs. Submit downloads sequentially (enqueue only, actual downloads retain configured concurrency), stop issuing further writes after unmount, and retain unsuccessful selections. Create groups in one existing request with contiguous season numbers from confirmed order. No backend contract or database migration changes.

## 并发与来源顺序增量（已确认）

复用下载配置接口、现有设置存储和弹窗，新增有界并发数与两种来源顺序枚举。调度器在空闲槽内领取任务，沿用数据库租约、单集 run、关闭等待和发布保护；降低上限仅限制后续领取，不中断现有任务。保存配置后唤醒调度，后续取流读取新顺序。

下载执行器通过 ResolveDownloadSource 按配置选择已有两条解析路径，旧 ResolveDownload 保留官方优先兼容入口。来源解析、网络、长度/时长不符及明确的媒体损坏诊断允许下一个来源接替；取消、本地写入和未知工具错误不换源，以免把环境故障误判为源故障。最多三轮，每轮每个来源至多一次；完成校验后发布失败不重下。保留安全 URL/DNS 校验和密钥脱敏。各来源只在自身返回的有效媒体中选择最高档，不为比画质额外请求低优先来源。官方现有解析材料只有 main_url，沿用唯一地址，不宣称全站最高。

旧配置缺失新增字段时采用默认值，旧配置请求省略字段不意外覆盖已保存的新字段；无需修改已有下载路径或完成文件。可将并发调回 1、顺序调回官方优先恢复旧调度策略。

## 按作品管理增量（已确认）

当前 `HongGuoDownloadService.List` 按分集每页 50 项返回，前端不可只对当前页分组。增加按 source_id 汇总和分页的作品查询、按作品分页的分集查询、管理员作品级失败重试入口；保留既有分集 API 兼容性。统计来自全部任务，作品顺序稳定，分集按 episode/id 排序。

批量重试复用单集重试的来源视频 ID 校验和状态清理规则，仅条件更新 failed 任务；并发状态变化不得重置非失败任务。返回实际入队数及无法重试数量，避免部分成功被当作全部成功。保留现有唤醒和租约机制，不新增队列表。

前端复用发现页的下划线导航与现有弹窗基础设施；作品列表和展开分集独立读取，轮询不覆盖设置草稿或折叠状态。其他页面样式差异只报告，待单独确认统一范围。

## 边界

独立下载业务表保存作品固定输出位置、分集状态及发布校验和；原有媒体、元数据、云盘、STRM 生成均不写入。红果资料继续由现有目录服务维护。页面和接口仅管理员可用。

## 数据流

作品详情下载按钮 → 根据已有源分集 ID 创建去重任务 → 后台有界并发消费者 → 按接口优先级取得临时媒体地址 → 隔离目录下载 → FFmpeg 处理及媒体校验 → 保存发布校验和 → 同文件系统无覆盖原子发布 → 标记完成。

取流地址及密钥只在内存中存在；客户端传作品 ID，不传任意下载 URL。出站下载连接阻止内网/本机地址及不安全重定向。只接受本实现支持的完整文件媒体；不把网页试看重定向当作正确分集。

## 目录与恢复

管理员设置绝对存储根路径，服务推导 downloading 与 completed 子目录。作品记录首次固定根路径、标题与年月路径；新增分集沿用。校验产物先持久化散列和大小，再发布；恢复时只认与发布记录一致的完整目标，不把普通同名文件当成功。租约隔离进程恢复；取消或租约失效后不得发布。

## 验证与限制

目录、错误身份、冲突文件、取消、恢复和密钥隐藏均需测试。PostgreSQL 测试使用项目隔离测试 schema。实际源接口和外部 CD2/Symedia 联调独立记录。现有红果目录隔离规范中的禁止取流条款因用户明确批准下载而新增管理员下载例外，播放仍只通过本地/STRM。
