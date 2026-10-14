# 执行计划

- [x] 核对既有工作树与读取/任务入口。
- [x] 移除整理入口和调度，调整人物翻译归属；验证路由、任务列表、Web 构建。
- [x] 实现统一来源展示与关联编辑，验证详情和季集边界。
- [x] 迁移搜索/排序/图片/人物读取，替代旧补缺消费者。
- [x] 核实可删除表与字段，按用户要求直接切换并更新规范。
- [x] PostgreSQL 定向回归、性能计划、Web lint/build、独立复核；全量测试限制和独立已知问题见后文。

旧任务的 97 项未提交改动为基线，不能覆盖或回退。

## 当前进度（2026-10-14）
已完成：整理 UI/HTTP/调度入口退休，翻译执行/定义/历史归属 TMDb，保留日志键。
验证：Web npm run lint/build；真实临时 PostgreSQL 下 TaskDefinition 系列、历史分类、路由退休、旧 organize.auto=true 不注册调度的回归通过；git diff --check 通过。
独立审查：怀疑共用 people kind 导致日志串类，经主线程核实 taskMatchesFilter 包含精确 Name 判断且数据库日志回归通过，排除。
一次测试因主线程提前停止临时数据库被打断，已重建容器完整重跑通过。
尚未实施：来源读取、字段编辑限制、搜索更新替代、表/字段精简。等用户回答旧手工值处理策略（异步问题已发）；不能推定允许覆盖既有人工展示。
没有操作生产数据库、提交、部署。完整 200 万文件数据与浏览器交互尚未验证。

## 来源切换实施进度
用户已明确：旧值不用保留、直接切换、尚未上线。上文等待决定已结束。
已实施：metadata_presentations/metadata_artwork_presentations 本地来源视图；基础列退出身份模型迁移和写入；普通编辑只接受 TMDb 关联；旧复制同步替换为持久索引待办；豆瓣绑定/补全海报改存来源中心；播放统计、图片读取和复查剩余基础列读取切换。
整剧/季换绑在同一事务解除旧子级 TMDb 来源并重置补全检查点，保留本地 ID；冲突整体回滚，同 ID 不清子级。新来源关联设置身份状态 source=tmdb。
已验证：四包无数据库测试通过；真实 PostgreSQL 来源读取/即时更新/搜索/解绑、重复 AutoMigrate、父换绑冲突回滚、普通编辑拒绝/来源更新、索引事务回滚/revision CAS/来源删除测试通过。
进行中：真实数据库四包全量回归（临时库）以及旧夹具适配。测试不带 DSN 的成功不能视为 SQL 回归通过。
已知尚未完成：全部旧夹具/旧 SQL 性能测试迁移、图片维护旧 selection/candidate 写读责任核对、翻译/人物来源实时边界核对、200 万规模查询计划、Web 最新检查、最终独立复核。不得声称完整切换已验收。

### 扩大回归发现与处理
四包完整 PostgreSQL 回归已执行且失败，主要是精简测试schema缺view、旧fixture直接写metadata_items.title，以及旧图片选择契约；不是全量验证通过。完整临时日志在 /tmp/mediastation-source-regression.log，收尾必须删除。
已补：handler migrateMediaHandlerTestDB、service migrateScraperTestModels 的来源schema；repository多个精简schema增加ensureSourcePresentationTestSchema；testdb.SaveMetadataPresentationFixture显式建立来源（含父层级），不是runtime回退。部分旧数据库身份测试移除raw INSERT title。
额外修复：普通图片读取FindSelection/ListSelections直接读来源view；活跃ImportRemote经SaveSourceArtwork写tmdb_artworks；普通本地图片持久化不再写旧选图（独立NFO未改）。旧候选创建/修复端静态发现已退休/孤儿，尚未清理全部实现和对应旧测试。
只读独立审查确认父子并发改绑竞态，增加lockMetadataHierarchy父到子锁、RebindTMDb观察版本校验、父换绑重置后代updated_at；HTTP stale返回409。SaveCatalogArtworkAsset也核对请求图片路径。
本轮Web lint/build通过。尚有大量旧测试需要逐项适配并重跑，不能交付为完成。


### 直接切换补充进度
- 已删除旧图片选择/候选/无图巡检写读链、旧媒体演职员写入，以及剧集本地资料纠正实现与任务历史兼容定义。当前 schema 不再创建 metadata_artworks、metadata_artwork_candidates、metadata_artwork_rechecks、metadata_credits。
- metadata_credit_presentations 读取唯一 TMDb 绑定的当前关系；Episode 读 Season。person_presentations 读取 TMDb 当前姓名/简介，译名按当前原文匹配缓存；公共人物 ID、本地头像及独立本地人物保持稳定。
- 人物翻译直接分页读取 tmdb_credits + tmdb_subjects，包含尚未绑定本地媒体的来源；角色缓存按来源作品 ID 隔离。补充来源角色候选部分索引及 30,010 关系的真实 PostgreSQL验证。
- TMDb 人物资料与人物/角色翻译缓存变化纳入持久索引队列。旧补缺任务不再复制展示字段。
- 身份资格和按需展示分离：Emby 列表排序需要正文时才读来源；继续观看先聚合有进度身份再按候选读取来源；Web 普通作品分页的身份/层级查询移出来源视图。
- 已通过：演员刷新、同一人物来源改名/简介和旧译名隔离、来源角色候选、图片资产去重/NFO隔离、来源与翻译索引登记、父子并发绑定、任务分类与退休、Emby图片/演员接口及若干分页/排序真实数据库专项。
- repository+database 完整真实回归最近一次仅人物候选精简schema缺view失败；该schema已补，针对性通过。service/handler完整回归发现旧夹具与部分真实查询退化，仍在修复和复跑，尚不能宣称全部验收。
- 既有开发数据不做保留或兼容迁移；本次仅维护现行 schema。使用旧开发 schema 的环境需重建测试/开发库，本次未连接或清理用户真实库。
- 性能样本为 4,000 剧、4,000 季、80,000 集与 80,000 文件，非 200 万实库。完整浏览器/真实媒体库压测未完成。

### 本轮后续清理与验证
- 删除未注册的媒体季集复查调度包装、旧队列执行器及本地资料/图片复制提交分支；共用季批量请求、租约、取消、404 与冷却逻辑保留，当前入口只提交来源。
- 普通媒体不再回退旁挂 NFO 建立本地资料，NFO 仍可提供外部 ID/季集定位提示；独立 NFO 库的入库与展示链未改。
- 作品层级资格查询改用身份表，实际展示按文件/作品 ID 限定来源投影，避免限定单剧时读取其他作品来源；未知库归属回退保留逐级主键查询边界。
- 复查日志改为来源即时可读并报告剩余缺项冷却，不再提示等待本地同步。独立复核确认当前 catalog maintenance 入口仍调用来源复查，无旧入口调用残留。
- 修正旧测试直接写只读视图/基础字段、手改基础资料预期，以及因移除关系模型自动建表而缺失 people 的精简测试 schema。
- 正在运行新一轮临时 PostgreSQL 回归；尚不能标记全部验收完成。

### 收尾验证结果
- 来源切换实现已落地；不保留旧正文、图片选择、演职员副本回退，不提供旧开发 schema 兼容迁移。未提交、部署或修改真实数据库。
- repository 完整真实 PostgreSQL 回归通过（261 项通过，live OpenSearch 1 项因无外部服务跳过）；database/handler 完整回归发现的精简 schema 与退休路由测试已修正，相关专项重跑通过。
- service 来源更新/换绑/图片/人物翻译/普通 NFO 回退拒绝/季集复查/分页与查询计划专项通过。末轮 14 项中两项旧 fixture 已修正并复跑通过；批量回滚 fixture 只建身份及文件，搜索 fixture 建真实来源。独立复核未发现测试绕过行为。
- 新性能断言曾捕获人物作品查询展开 20,200 次来源标识查询；改为身份查询后专项通过。相关搜索 repository 补测通过。Web lint/build 前轮通过，此轮未改 Web；git diff --check 通过。
- 验证限制：service 全量未取得全绿，早期完整运行曾超时；已知 TestHongGuoEmbyPlayableIdentityAndUserState 的红果人物名称搜索仍失败，有 SearchTerm 时 Persons 仅查询普通人物，这是当前未修改的独立分流问题，未扩展本任务修复。
- 未验证 200 万文件真实库、完整浏览器交互、真实 TMDb/豆瓣上游和 live OpenSearch；合成性能样本只有 4,000 剧、4,000 季、80,000 集与 80,000 文件。因此最终整体质量门仍不标记全部通过。
- 收尾停止临时 PostgreSQL 容器并删除本任务临时测试日志/导出；保留用户既有工作树及 core 文件。

### 索引批次重复刷新修复
- worker 收集本批最多 100 个 metadata ID，一次调用 RefreshMetadataIDs，复用仓储的顶层作品去重。成功仍按原 revision 逐条完成；部分失败保留整批并延迟重试，接受幂等重复写入以避免丢待办。
- 新 PostgreSQL 回归先复现同剧 3 个分集刷新 3 次的失败；修改后 TestMetadataIndexWorker 两项通过（5.118 秒），覆盖整剧只写一次、整批失败保留和延迟、刷新中新 revision 保留、缓存失效。
- 已独立复核；批次失败重试已成功写入属于预期至少一次语义，不是任务丢失缺陷。git diff --check 通过。
- 本次只改 worker 和对应测试，未重跑全量服务/200 万实库压测，前述全量验收限制仍保留。临时 PostgreSQL 与测试日志收尾删除；未提交或操作真实库。

### 用户授权的现有开发库定向切换
- 用户启动新代码后，新来源视图/索引待办/触发器已建立，但旧 title 非空约束仍阻挡新身份插入。用户明确授权由本次处理现有库，服务已停止、无其他应用连接后执行。
- 在单个事务中删除 metadata_items 的 10 个旧展示列（title/original_name/overview/rating/runtime_sec/year/release_date/languages/countries/genres）及 metadata_artworks、metadata_artwork_candidates、metadata_artwork_rechecks、metadata_credits 四张旧副本表。未使用 CASCADE，保留 source、身份、层级、检查点及关联。
- 提交前保存结构定义供核对；查明无外部表外键引用，当前 ORM 只读字段不写入，来源视图不依赖旧列。独立复核提出的 ORM 写入疑点已通过当前依赖源码和此前新 schema 回归排除。
- 同一事务内 savepoint 验证身份 INSERT/UPDATE/DELETE 和来源视图读取，回滚测试记录及触发副作用后提交。提交后旧表/旧列/测试记录/测试索引待办均为 0，索引触发器仍为 8 个。
- 核对 media、metadata_items、metadata_identifiers、users、api_configs、playback_histories、tmdb_subjects、douban_subjects 行数前后不变；只执行定向 DDL，未改保留表业务记录。未进行重新扫描或自动重启应用，完整启动/页面/实际扫描由后续运行验证。
- 清理本次临时数据库连接脚本和结构导出；没有保留旧正文备份或新增兼容层。


### 补全任务归属收拢（本轮）
- 退休 catalog_scrape、people_backfill 的定义、路由、调度、队列模型及工作者；作品补缺复用现有 TMDb 电影/剧/季/集来源维护。
- 新增 tmdb_data_people（TMDb 演职员与人物资料补全），直接查询来源 payload 完整证据、来源人物，不依赖媒体身份或文件。空 cast/crew 数组表示已获取，缺失/null 不计完成；独立 next_people_at 退避、Fetch 预算/租约和日志沿用来源中心。先失败后耗尽预算仍报告失败，不误计成功。
- 删除旧 catalog 图片工作者和六个 metadata 检查点，图片/头像统一走现有来源图片工作者。保留文件入库三工作者、季集占位和绑定。修复未到齐来源资料时身份 source 标记丢失导致占位集无法补绑定的问题，不回退旧正文。
- 验证：临时 PostgreSQL 下新人物补全、空列表、缺失/null、失败重试/预算/取消、季集入库、任务分类/日志、旧入口退休、来源图片、schema/来源展示等四包定向回归通过；Web lint/build 与 go test ./... -run '^$' 全包编译通过。独立只读审查未确认新缺陷；预算误推进猜测经代码确认已被提前返回排除。
- 准备 scripts/migrations/20261014_retire_media_completion.sql，停服后定向删除旧队列和六列，再由新版本启动重建视图。真实 PostgreSQL 模拟旧视图依赖、重复执行与身份保留回归通过。未对实际开发库执行，等待用户确认停服窗口。
- 未进行真实 TMDb/豆瓣请求、完整浏览器和 200 万文件实库压测，未宣称 service 全量全绿；此前独立红果人物搜索问题仍不属于本轮。

- 本轮临时 PostgreSQL 容器和临时测试日志/编辑脚本已清理，未停止用户服务，未提交。

### 本轮实际开发库清理已执行
- 用户确认“已经停了”后核对数据库无其他连接，在单事务执行 20261014_retire_media_completion.sql 的定向 DDL。已提交：catalog_hydration_jobs 删除、六个旧 metadata 检查点删除；两个相关只读视图暂时删除，等待用户启动新代码自动重建。
- 提交前逐表核对行数不变：media 2,314,469、metadata_items 465,214、metadata_identifiers 615,950、tmdb_subjects 458,357、tmdb_person_profiles 559,590、tmdb_credits 4,018,278、douban_subjects 8,673、users 1、api_configs 8、playback_histories 1,346。提交后旧队列表不存在、旧六列为 0。
- 未启动应用，启动后视图重建/新任务运行尚待验证。没有生成连接脚本、凭据导出或其他临时文件。

### 索引人物查询性能修复
- 实际运行查询确认：metadataSearchPeople 的两个展示视图联接使通用计划将约 56 万人物及译名反复作为内侧扫描；来源 ID 转文本也导致每次按类型扫描来源。当前 22 条索引待办被该查询拖住。
- 仅修改该装载函数：当前批次电影/整剧及季先取唯一有效来源关系，沿用演员/导演/编剧和 subject-origin 规则；用现有数字唯一索引定位来源，并保留文本严格匹配。关系去重后单独按人物主键取当前展示名，保留译名、软删除及错误传播。
- 真实开发库只读 EXPLAIN ANALYZE：21 个顶层作品及季，关系查询约 323.8 ms、297 个人物名称约 147.1 ms；无新增索引或数据库数据修改。这是该待办范围的样本，不承诺全库恒定耗时。
- 独立 PostgreSQL 2 万无关人物/来源回归通过；旧实现对照稳定因扫描 20,000 people 失败。覆盖实际扫描量、generic plan、演员/导演/编剧、履历排除、软删、歧义/无效绑定、译名、改名和解绑后的索引文档写入。搜索文档/绑定与索引 worker 定向回归通过。
- 独立复核关于职务大小写漏选的疑点已核对：旧视图和新查询使用完全相同的 cast/Director/Writing 条件，无此新增回归；补充实际文档 upsert 检查，避免只测内部返回值。
- 新代码尚需用户重启才能替换正在运行的旧二进制；未删除/手工结算实际库待办，未擅自重启服务。无需 SQL 迁移。

### 人物索引查询修复后的启动复核
- 用户再次重启后 health 返回 ok；四个展示视图存在，旧 catalog_hydration_jobs 与六个检查点仍不存在。
- 原持续数分钟的双展示视图查询不再出现，复核时超过 30 秒的活动查询为 0。原 22 条积压缩减到 3–4 条近期待办，来源图片任务继续成功，媒体刮削仍在运行。
- 15 秒只读抽样：4 条中 1 条完成删除、2 条因新来源变更推进 revision、1 条 revision 不变；未观察到同 revision 延后重试。说明待办在消费且持续有新变更，不能把动态队列尚未归零表述为全部空闲，也不宣称全库无其他问题。
- 本轮只读检查，无服务启动/停止、数据修改或临时文件。

- 补充核对发现两个已无运行读者的 people.backfill_* 设置，按既定清理授权精确删除 2 条并提交，复查剩余为 0；同步一次性切换 SQL。旧任务执行历史/日志仍保留，仅用于查看过往记录，不参与运行。

### 提交归档边界
本任务按最终直接来源展示方案交付，旧媒体同步方案已被取代。用户授权的开发库定向清理和重启验证已完成。归档不表示全项目无缺陷：红果人物搜索独立问题、完整 service 全量非全绿及 200 万文件完整扫描压测限制仍保留。
