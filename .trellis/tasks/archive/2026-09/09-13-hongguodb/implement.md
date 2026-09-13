# 实施与验证

## 实施顺序

- [x] 1. 独立数据模型、来源解析与事务入库：检查 ID 精度、完结判断、集数、快照白名单、幂等与失败回滚。
- [x] 2. 分类发现、详情刷新、图片本地化与来源任务：检查取消、检查点、重试、任务互斥及旧任务隔离。
- [x] 3. 公共 Media 的来源归属与独立绑定：检查文件 ID 匹配、重复扫描、旧刮削排除、探测及本地/STRM 播放。
- [x] 4. 人工聚合与稳定用户状态：检查季号冲突、解聚、收藏和观看进度、权限及用户隔离。
- [x] 5. 独立资料 Web 与任务切换、Emby 查询和播放适配：检查分页、搜索、详情、季集与状态一致性。
- [x] 6. 完成实现集成与独立复核，修复本任务发现的问题，补齐逐项人工验收指引；实际环境验收仍待用户执行。

## 自动检查

- 来源解析及匹配纯单元测试，不依赖公开站点在线可用性。
- PostgreSQL 集成测试使用 MEDIASTATION_TEST_POSTGRES_DSN 和独立 schema；未配置则明确记录跳过，不冒充验证通过。
- Go 针对性测试，最终 go test ./...；Web npm run lint、npm run build；git diff --check。
- 实现后独立审阅 diff，复核权限、旧体系查询污染、迁移幂等和状态身份不变性。

## 进度记录

- 2026-09-13：用户确认完整推进，最终集中人工验收。当前已确认旧 Media.MetadataID 与用户状态外键绑定旧表，不能直接写入红果 ID；后续按独立绑定适配。
- 2026-09-13：已新增独立模型、资料解析/事务入库、来源任务、任务中心体系切换、Web 来源资料页、源 ID 文件匹配、人工聚合编辑、来源用户状态。已接 Emby 库内目录/季集/电影及单集 PlaybackInfo/进度/收藏/已看，Web 来源媒体播放入口和“我的”体系切换。整体仍未交付。
- 新增 PG 检查 `TestHongGuoBindingGroupingAndStableProgress`：未知资料待匹配、导入重绑定、聚合季号与源坐标分离、重复会话去重、单集电影转类保持进度、无效重扫清除绑定、反向旧体系扫描拒绝。
- 新增 PG 检查 `TestHongGuoEmbyPlayableIdentityAndUserState`：逻辑 ID 到 MediaSource、电影/单集状态、错误 MediaSource 不改变其他作品、锁定 profile 拒绝回写、聚合季集和容器收藏/已看。容器批量标记最初发现事务内使用外部连接导致单连接池等待，已改为事务连接执行子查询并复测通过。
- Web lint/build 已多次通过；新增“我的”用户记录查询专项检查仍随末轮运行确认。

## 全量回归基线对照

`go test ./...` 在临时 PostgreSQL 中未全绿。对未修改 HEAD 的临时源码副本执行同名测试，以下失败均可复现，不能写成新功能验证通过，也暂不扩大范围修复：

- Repository：`TestMetadataSearchCountsPlayableTopLevelWorks`、`TestUpsertCanonicalExplicitMergeMovesMovieReferences`、`TestMediaSearchUsesExternalBackendAndFallsBack`、`TestMediaSearchFilteredSupportsChineseFuzzyTerms`。
- Handler：`TestEmbyItemImageServesWithoutAPIAuth`、`TestEmbyItemImageServesPersistentArtworkWithoutResolve`、`TestEmbyProgressRoutesUseProbeDurationAndIgnoreUnknownDuration`、`TestEmbyAdditionalPartsRoutesAttachTokenAndKeepProgressMediaID`、`TestEmbySearchHintsReturnsSharedMetadata`、`TestRetainedPlaybackWorkflowsNeverStartFFmpeg`、`TestProbeLibraryRouteRequiresAdminAndReturnsAccepted`。
- Service：`TestKnownTMDbIDReusesLoadedDetails` 在旧 scraper.go 的缺失 library 路径发生空指针。
- 末轮再次执行完整 service 基线，当前工作树与未修改 HEAD `598a2d0` 返回相同的失败测试清单并在上述测试 panic；还包括旧元数据/探测/整理/扫描夹具等失败，而不是只有这一个 panic。例：`TestUpdateMediaMetadataMarksManualMatch` 缺少 media_probe_metadata 表，`TestEmbyItemsFilterByPerson` 旧分集夹具违反 season-zero 约束，`TestEmbyLatestItemsOrderByReleaseDate` 旧日期排序断言不符。未将这些测试改绿，也未把 panic 后未执行的测试当成通过。
- 本次可归因的任务定义数量断言由 16 更新为 19；新增来源任务定义已单独核验隔离。

## 补充实施与复核记录

- 目录尾页 404：用户日志中先保存320项后404，重试立即0项404；对照更新后的 juku 确认其 Web 分类以原始列表少于24项结束，而本项目此前只认空页，320=13×24+8 与“短尾页后多请求一页”吻合。现按原始 recommendList 数量判断，短页摘要与检查点归一同事务保存；兼容已前移的旧检查点时，page N 404 必须成功回查 page N-1 且仍为短页才能重置。首页404、前页完整时的中途404、403/429/5xx和解析错误仍失败并保留检查点。错误含分类、页码和相对路径，不含响应体或绝对URL；任务级失败文案不再误写“作品失败0项”。
- juku 新增的 App API 目录/详情、签名、has_more/next_offset 游标和分集 vid_index 校验有参考价值，但引入固定设备参数、签名协议和额外来源契约，当前 Web 资料链路未需要，未直接移植；第三方播放回退与本项目“只处理本地下载媒体”边界冲突，不采用。当前仅复用已能用真实 Web 行为和测试证明的24项尾页规则。
- 临时 PostgreSQL 中完整红果 repository/service/handler 专项 -race 通过；来源客户端全部 -race 通过。回归覆盖23/24原始条目边界、投影去重不影响尾页判断、8项短页、旧检查点404回查恢复、首页404/前页完整时中途404失败、503错误上下文、25页以上持续扫描、重复页和10000页保护。第二轮独立复核无阻断项；日志中的页条目数改用原始列表数量。未重跑全量Go或生产任务，真实检查点恢复留待用户部署后重试确认。
- 发现入口合并：用户已确认统一发现权限。Web 可见名称改为“红果短剧”，观看空间只保留发现入口，system=catalog|hongguo 并列切换且按需挂载；旧 /hongguo 保留 query 重定向。列表一次 JOIN 返回本地 artwork_id 与真实 tags，圆角竖海报展示集数/评分/标题，缺图回退；管理操作折叠保留，不混合资料或用户记录。独立复核核对旧链接参数、来源隔离、无逐卡详情请求及权限边界，未发现本轮新增问题。
- 末轮 Web lint/build、红果发现浏览器脚本与 git diff --check 通过。测试脚本显式模拟正确的权限、档案和推荐栏目响应，避免通配响应覆盖具体接口；图片故障占位单独断言，不把模拟图片当真实下载验证。临时预览、浏览器和测试容器均关闭，截图与临时日志已删除；工作树保留未提交，未归档。
- 本轮临时 PostgreSQL 中 repository/service/handler 红果专项 -race、完整来源解析 -race 通过；任务日志和旧剧集加载脚本通过。浏览器模拟接口验证旧链接/参数、按需请求、体系切换、权限、图片失败占位及明暗主题390/768/1024/1280/1440无横溢，亲自查看暗色手机及亮色桌面截图。模拟接口不代表真实海报加载或生产端到端；部署后按 A02/A04/A12 验证真实海报、权限和交互。未重跑全量 Go，旧基线失败仍保留。
- 持续发现：取消每分类每轮20页限制，按持久化检查点持续读取至有效空页；保留串行请求、分类顺序及逐页原子保存。整页没有本轮新ID时明确报分页未推进；非空第10000页保存摘要但不把游标归一到首页，明确报安全上限。独立复核确认只有空页重置游标，异常/取消不冒充完成。新增 `TestHongGuoDiscoveryScansUntilCategoryEnd` 验证四分类各25页及第26页空页、重复页和非空上限的失败状态/检查点；临时PG中红果来源、service/repository/handler专项 `-race` 与 diff 检查通过。未跑真实站点全量采集、全量Go或本轮Web构建（只改后端和后端下发的任务说明）；实际分类覆盖范围和出口限流仍待验收。临时测试容器与日志已清理。
- 发现提速：新增 `hongguo_discoveries` 保存分类摘要，待补齐由不存在正式作品且无失败记录推导；分类页不再逐部取详情，摘要批量写入与页码原子提交，日志按页更新。刷新按固定时间边界和源ID游标分页补齐全部已发现新作品，再执行50条到期重试、100条超过24小时旧作品维护；指定ID仍立即刷新。发现、刷新、图片三个任务槽可同时运行，统一取消并等待退出。没有自动联动、额外常驻任务或占位媒体，完整详情成功后才进入已有 Web/Emby 目录。
- 本轮验证：`TestHongGuoDiscoveryDefersDetailsAndResumes` 证明24条目录只请求分类页、失败页续跑、105部跨批补齐、成功同轮不重复、摘要不覆盖详情和指定ID绕过冷却；`TestHongGuoDiscoveryCheckpointAndRetryIsolation` 证明页码保存失败时摘要回滚、固定截止点和重复发现不重置失败冷却。三任务同时请求/去重/取消/停用/关闭测试通过。临时PG下 repository/service/handler 红果专项与完整来源解析 `-race` 通过，Web lint/build 和 diff 检查通过。独立复核重点确认三组取消句柄、摘要与详情隔离、游标查询不反复从头扫描已处理记录、旧数据无需迁移。未执行全量Go或真实上游速度对比，生产迁移和 A03/A10 留待用户验收；本轮临时容器与测试日志已清理。
- 用户确认放开图片并行：资料发现/刷新保留共享互斥，图片使用独立互斥和取消句柄；取消、停用同时取消两组，关闭等待两组退出。Web 取消文案明确为所有红果任务。根因是此前全来源共享锁把图片一并拒绝，且调度异步返回后拒绝只记服务日志；本次仅拆开不必要互斥，不重构通用调度器。独立复核确认取消句柄只在生命周期锁内操作，任务清理仅清自己的句柄且早于释放运行锁，图片原有来源地址条件写回和固定批次截止点保持不变。
- 新增 `TestHongGuoArtworkRunsAlongsideCollection`，真实临时 PostgreSQL + 阻塞 HTTP 验证两组同时进入请求、三种重复任务拒绝、取消/停用/关闭同时中断和任务槽释放。repository/service/handler 全部 `TestHongGuo` 的 `-race` 检查通过；Web lint/build 与 `git diff --check` 通过。本轮未执行全量 Go、浏览器端到端或生产环境联网采集，仍需部署后验证 A10。临时测试库和日志已清理；任务保持未提交、未归档。
- 用户实测发现首次采集301：已修正分类第1页不带page参数，第2页起保留分页。真实请求还发现category_layout与category_$并存，解析器现优先明确的动态资料节点，不把布局当资料。根因是此前模拟测试只覆盖category_page，没有覆盖真实请求规范化及布局节点；新增四分类第1/2/10000页地址断言和分类/详情动态节点回归。实际Go客户端验证四分类各前两页均返回24个ID，并各抽取1个作品成功解析详情；只读验证，未操作生产库。临时联网检查文件已删除，保留离线回归测试；完整发现入库任务仍需部署后重试。
- 已完成 Emby 全局浏览、搜索、Latest、Resume、Counts、人物查询及图片；混合列表先 SQL 合并分页，再批量加载当前页载荷。旧电影、整剧、季、单集复用原批量投影；明确 Fields 时不加载未请求的 People/ProviderIds/MediaSources，省略 Fields 保持旧默认。
- 已完成来源库 Web 目录、播放文件版本、我的收藏/历史/继续观看、已看/清除进度和独立播放统计。统计复用现有 DTO/页面，只查 hongguo_playback_events；URL system=catalog|hongguo。
- 已完成失败队列冷却、同轮成功重试去重、图片本地化/缺图修复、停用和关闭取消。取消保留 context.Canceled，任务为 interrupted，不写失败重试。
- 已完成管理员待匹配文件分页诊断，入口 `/hongguo?pending=1`；任务日志记录成功/失败作品 ID 和 processed/failed/succeeded。业务重试不依赖任务日志。
- 独立复核发现旧元数据编辑可能创建孤立旧资料，已与旧搜索、手动匹配、自动刮削一起在写入前拒绝独立来源；专项测试断言旧资料数保持 0。
- 停用时的新扫描绑定尝试报错并回滚整个文件 upsert，旧绑定/资料仍可读；不提供破坏性卸载按钮。
- HTTP 使用真实 JWT 与临时 PG：匿名401、非管理员写403、锁定档案不返回文件/库且不可修改进度、跨用户字段不能覆盖认证用户、公开 DTO 不含来源签名地址。
- HTTP 本地临时文件 Range 返回206及正确字节；测试 STRM 返回302及原目标，不请求目标站点。图片 GET/HEAD 已通过，缺文件会排队修复。这不是视频解码或真实设备播放验收。
- 新增独立统计 PG 检查：总数、趋势、分页、账户/库/类型过滤、排名日期交集、聚合后展示季号、文件删除仍计数、旧统计不受影响。
- 浏览器使用本地构建和测试响应：资料页390宽无溢出，统计页390/768/1024/1440宽无溢出，切换与非法/重复system归一化通过，检查了暗色资料页与亮色统计页。不是完整真实后端端到端或全部主题/视口组合验收。
- 本轮 Web lint/build、任务日志脚本、旧剧集加载脚本均通过。最新全量 Go 的 database 包通过；整体仍被旧测试夹具/旧行为断言失败阻挡，不能写成全量绿。
- 末轮所有 TestHongGuo 在临时 PG 中通过；来源解析和模型测试通过。来源解析、repository/service/handler 红果专项 `go test -race` 通过，包括8并发同会话去重、事件插入失败时进度事务回滚、混合页旧整剧/季/集摘要。
- Emby 静态接口目录已执行 ID 唯一性和非空响应描述检查。真实接口目录页面的完整可访问性/交互组合仍列入 A12。

## 交付限制与人工检查

- 真实上游可用性、真实下载文件的编解码/seek/字幕、具体 Emby 客户端缓存刷新行为，需使用用户实际环境验收。
- 用户确认发现合并后，作品/详情/分集/聚合目录查询统一受 can_view_discover 控制；本地文件/媒体库、图片与用户状态仍沿用既有账户和播放档案限制，没有新增来源专属权限配置。
- “我的”来源卡片只展示有可见绑定文件的资料；无文件收藏仍保存在来源用户表，绑定文件后出现。删除文件不删除事件，管理员统计仍保留不可用记录。
- 红果发现、资料刷新和图片各最多一个任务，可同时运行；各任务内部串行请求，图片和媒体探测复用现有公共能力。详情/图片任务开始后新增的待办留待下一轮。本次不引入跨所有来源的新通用网络调度框架，超大资料量吞吐未压测。
- 人工验收 A01–A12 仍保持待验证，详见 acceptance.md；不把自动接口检查冒充真实客户端验证。
