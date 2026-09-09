# 验证记录

## 初始化进度与批量核对追加修正

- 现场只读核验：初始化游标持续推进，35,200 / 78,091 文件；无锁等待。无后续日志来自重复文案去重，不代表任务停止。
- 初始化每批最多 200 文件，共用一次批量状态查询及一次批量登记，保留事务游标、版本、租约和冷却。不改变先核对、再归并、再消费的执行顺序。
- 资产归并、文件核对、季集变更归并、到期待办处理分别记录阶段起止；批次/结果边界约每五秒输出本次累计数量和总耗时，不新增全库计数或心跳线程。单个 SQL/网络请求未返回时不会额外输出心跳。
- PostgreSQL 17 追加回归通过：200 文件（多版本/未绑定）、80 集（半数完整、半数缺图）只执行一次状态 SQL 和一次登记 SQL，登记 40 集与父季；保持已有 revision=9 与 future due/lease。
- 原空批断点、多连接、合并、资产展开、冷却与重试回归通过；任务日志测试断言各阶段及耗时出现。
- 最终 repository 3.714s、service 0.715s；go vet 与 git diff --check 通过。写完后主线程二次复核查询边界、调用方、计数与日志去重行为。
- 未部署重启、未修改实际数据库，未测生产数据上的具体提速倍数；本轮未改 Web。隔离验证容器结束时关闭。

## 已实现

- 季/集专用持久队列、业务事务内触发器登记、每批 200 文件/后代/资产影响展开及持久游标。
- 到期索引、三 worker 单条领取、五分钟租约及续租、过期 token 拒绝、结果事务并发快照校验。
- 保留 72 小时成功冷却、失败退避和现有任务开关/手动入口；图片与头像准备放在结果事务外。
- Web 任务中心只读折叠面板及管理员列表 API；筛选、数量、分页、固定安全原因，不暴露领取凭据。

## 实际验证

隔离 PostgreSQL 17，设置 MEDIASTATION_TEST_POSTGRES_DSN，以下测试未跳过：

- repository/service：TestTMDbRecheck*、TestTMDbMetadataRecheck*、TestTMDbEpisodeRecheck*、TestListTMDb*MetadataRecheck。
- handler/database：TestTMDbRecheckListAccessAndValidation、TestAdminRouteSurfacesAreRegistered、TestAutoMigrate*。
- 双专用连接持锁验证 SKIP LOCKED 与 NOWAIT 提交退让、同季兄弟集不误伤快照、变更后拒绝旧结果。
- 真正调用 mergeMetadataGraph，验证整剧/季/集递归合并及多版本重绑与登记共同回滚，提交后源待办清理且目标登记。
- 401 个共享图片引用按 200/200/1 续扫、资产删除登记、整剧后代单批 200；201 条无元数据文件跨空候选批次续扫。
- 30,000 条未来待办下 EXPLAIN (ANALYZE, BUFFERS) 有 Index Cond，不全扫未来待办。
- go vet：repository/service/database/handler；Web npm run lint、npm run build；git diff --check。
- 最终修正后联合回归通过：repository 4.670s、service 1.289s、handler 0.171s、database 13.376s；随后 go vet 和 diff 检查成功。保存阶段碰到业务行锁的 100ms 退让也由双连接测试验证。
- 隔离浏览器组件测试（模拟 API）：关闭时零请求、展开、分页、重复选择当前筛选、空结果、失败后刷新恢复；390/768/1024/1440 宽无水平溢出，明暗截图复核，无运行时异常。

## 独立复核与修正

- 两轮只读独立复核，主线程点验具体来源；资产游标误报经磁盘核验排除，并补 401 引用回归防止实际退化。
- 主线程确认父到子展开与提交字典序锁可能反向等待，提交改用 NOWAIT 后条件重排，并用双连接持锁测试验证。
- 只给文件/层级影响登记父季，避免普通集资料写入使兄弟集快照失效。
- 快照存在性触发器不转换大 payload；不相关字段更新在函数内返回、不登记。
- 重复迁移回归发现 UPDATE OF 字段触发器阻止现有 GORM 类型兼容迁移，已恢复普通行触发器，在函数内过滤字段；不为此改动旧迁移流程。
- 重复选择当前状态不再清空列表造成持续加载。

## 尚未验证与风险

- 未对实际业务数据库运行迁移或写入，未重启用户服务；实际启动、生产规模初始化及真实 TMDb/图片网络联调需要部署后观察。
- 浏览器是独立组件与模拟 API，不是实际登录后端到端联调。
- 不声称所有竞争组合和长时间续租故障注入已穷尽；崩溃可能重复外部读取，但条件保存不得迟到覆盖。
- 初始化/每七天核对总工作量仍随文件数增长；有界的是批事务和内存，不是整轮总耗时。
- 触发器增加相关业务写入成本；首次建表/索引和安装触发器会持有数据库锁，建议无写入的部署窗口。
- 非本任务的旧 TestTMDbArtworkMissingRecheckHonorsTaskHandoffCooldown 曾因 fixture 缺少 media 表失败，不包含在通过项；未运行全仓全量测试。
- 临时浏览器页面、截图、Web 服务和隔离 PostgreSQL 验证容器均在验证后清理/停止。不自动提交/归档。

## 404 分类与人工清理扩展验证（2026-09-08）

### 待办入口与统一样式追加

- 季/集复查与媒体入库刮削待处理入口均移到对应任务名称后（桌面/手机一致），操作区不再显示待办入口；点击后才挂载对应 ModalShell，初始任务页不再查询刮削待处理记录，无后端修改。
- 媒体入库刮削待处理保留原筛选、刷新、分页、重试、手动匹配和 STRM 安全删除逻辑；弹窗限制为 `86vh` 并在内部滚动。手动匹配或删除确认打开时禁用父弹窗 Escape/遮罩关闭，避免父子层同时关闭。
- `node web/scripts/check-recheck-dialog.mjs`、`npm run lint`、`npm run build` 与 `git diff --check` 通过；脚本追加检查桌面/手机任务名入口、操作区移除入口、按需挂载及嵌套弹窗关闭保护。
- `node web/scripts/check-recheck-dialog.mjs` 验证真实组件的请求分类、换标签回第一页、同标签不重置、分页、刷新、取消与过期响应隔离。
- 隔离 TasksPage 模拟接口浏览器验证操作区入口、404 请求参数、文件弹窗返回原分类；390 手机浅色与 1440 桌面深色截图复核，无横向溢出或浏览器异常。临时页面、截图、浏览器和开发服务已清理。
- 仅模拟接口交互，未操作真实删除、未验证生产登录态。三天复核与原删除契约不变。
- 弹窗高度回归修正：主待办和关联文件弹窗限制为 `86vh`，滚动主体同时设置 `min-h-0` 与 `overflow-y-auto`，避免 Flex 子项按内容最小高度继续撑出屏幕；标题区保持在主弹窗滚动区外。组件脚本分别检查两个滚动主体，Web lint/build 与 diff 检查通过。
- 404 列表操作改为与媒体入库刮削待处理一致的 `btn-danger`、垃圾桶图标和“删除”文案；仍按具体 media ID 进入多版本选择及最终路径确认。组件断言、lint/build、diff 检查及独立只读复核通过。

### 关键字搜索与任务提示追加（2026-09-09）

- 季/集复查支持按单项/整剧标题、metadata ID 和 S/E 坐标做服务端关键字搜索；媒体入库刮削待处理支持按扫描标题、路径、媒体库名称和已存错误搜索。搜索仅在回车或点击按钮后执行，状态/媒体库筛选、分页总数和过期响应保护保持一致。
- 任务名后的查看按钮在非结束季/集待办或媒体刮削待处理数量大于零时使用金色边框、背景、文字和光晕，并显示数量，超过 999 显示 `999+`。进入任务页及关闭弹窗时各刷新一次；每个接口只取 1 行，不加入三秒任务快照轮询。
- 隔离 PostgreSQL 17 未跳过执行 `TestTMDbRecheckQueueTransactionsAndClaims`（0.284s）与 `TestTMDbRecheckListAccessAndValidation`（0.106s），覆盖标题、S/E 与无结果搜索；临时容器由退出清理删除。SQLite `TestListScrapeIssuesFiltersAndSanitizesReasons` 覆盖媒体库、路径与无结果搜索。
- `node web/scripts/check-recheck-dialog.mjs`、Web lint/build、`go vet ./internal/repository ./internal/service ./internal/handler`、`git diff --check` 通过。独立只读复核确认 count/分页使用相同过滤条件、空关键字保留先分页后联表路径、数量提示不进入高频轮询。
- 未在真实登录环境做浏览器端到端交互或截图，不声称 `%keyword%` 搜索在未来超大数据量下仍无需索引；仅用户提交搜索时执行联表模糊匹配，后续应以生产执行计划决定是否增加 trigram 索引。

- 用户批准三天复核：详情 HTTP 404 写入 not_found、独立计数；请求身份保存在隐藏字段，普通资料事件保留 due_at，身份变化唤醒本地核对。历史 retry 等下次真实请求归类。
- 新增管理员分页关联文件接口与 Web 版本选择，复用既有 STRMDeleteDialog/删除接口；不新增普通视频删除、自动清理或编号推断。
- 隔离 PostgreSQL 17 联合定向测试未跳过：repository 4.091s、service 2.881s、handler 0.381s；覆盖原队列/季集回归、404 的 72 小时到期与再领取、身份变化/普通变更、过期 404 拒绝、非详情失败重试、文件版本分页/种类过滤/URL 隐藏、管理员鉴权、STRM 安全删除与预览后越界重解析。go vet 三包通过。
- 新增 404 服务用例 race 检查通过（2.108s）；database TestAutoMigrate* 通过（11.701s）。新字段使用现有迁移，不在实际库手动执行 DDL。
- Web lint/build 与 diff 检查通过；隔离组件浏览器使用模拟 API，验证第二版本的精确 media ID、取消零 DELETE、父目录默认 false、勾选后的最终绝对路径，以及只发送一次 delete_parent=true。390/768/1440 宽无横向溢出，手机浅色与桌面深色截图已复核，无浏览器异常。
- 独立只读复核提出 INSERT 位于 kind 条件外的疑点；主线程点验磁盘 114–137 行，确认插入仍在季/集条件内，为误报，无需改动。
- 新 handler 测试首次 fixture 缺少有效父级/集号，触发模型约束；已修正 fixture 后重跑通过，未放宽产品约束。
- 不声称生产部署或真实 TMDb/图片/文件端到端联调已验证；浏览器删除为模拟请求，真正文件删除回归仅用 t.TempDir。保留现有重新解析当前路径的语义，不新增预览锁；预览期间不应修改 STRM/映射，父目录删除不可恢复。
- 临时页面、截图、浏览器与 Web 服务已清理；隔离数据库测试容器在最终回归后停止。工作树未提交，按 finish-work 的未提交检查停止归档，不影响本次产品实现完成。
# 2026-09-09 搜索与媒体库聚合复用

- 管理员接口 `TestTMDbRecheckListAccessAndValidation`、Web lint/build、series loading/presentation/detail 和 recheck-dialog 四个契约脚本通过。临时 PostgreSQL 容器已停止并自动删除。
- 隔离 PostgreSQL 17：`go test ./internal/service ./internal/repository -run 'TestLibrary|TestTMDbRecheckQueueTransactionsAndClaims' -count=1` 通过；覆盖筛选、分页、空库/越界总数、整剧/季直接文件、季零、版本/日期平局、字面通配符搜索。`go vet ./internal/repository ./internal/service` 通过。
- 真实库只读 EXPLAIN 三轮：媒体库原 count+page 为 798.714/808.786/835.027ms，新组合查询 252.589/254.152/250.087ms。全库 898 个作品代表文件/数量 EXCEPT ALL 双向差异 0，首页顺序相同。
- 示例关键字 test、pending 状态：原 search count+page 106.864/109.587/107.714ms，新组合查询 41.234/44.110/44.177ms。日志原关键字和状态未提供，不能当作原 257ms 的同参数复现。
- 独立只读复核未发现指定范围实际问题。未验证重启后的登录浏览器请求、生产并发压力；无新增索引/数据迁移。匹配范围扫描和物化临时文件仍随数据量增长。
