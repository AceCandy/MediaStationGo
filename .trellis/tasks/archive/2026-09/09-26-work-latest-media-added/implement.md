# 执行计划

## 作品候选统一追加（实现与隔离验证完成，待实机验收）

1. 按 R9–R13 完成入口清单及最终范围确认；保留此前全部 57 项工作树变更。正式改代码前主线程完整读取所需规范与确切待改代码，沿调用方核对差异，不重复部署/补算。
2. 保存当前查询作测试 oracle，先补全局 Movie/Series、无库 Latest 和普通计数页/混合电影库的行为与实际计划检查；Web 普通库覆盖原代表版本/分集排序，已优化三来源入口作为对照。候选测试不能只断言 SQL 文本或最终页长。
3. 先接入三来源全局作品候选，复用库内资格/状态及页内加载；再去掉无总数 Latest 的额外计数及红果 PersonIds 完整节点资格投影。对同类普通库/Web 冗余汇总依次实施，逐个比较完整旧结果；不得顺带统一原本不同的状态/排序行为。
4. 使用独立临时 PostgreSQL 17 和 MEDIASTATION_TEST_POSTGRES_DSN，真实运行 TestEmbyGlobalBrowse、TestEmbyLatest、TestEmbySeriesPagination、TestHongGuoLibrary、TestNFOLibrary、TestEmbyResume、来源搜索/人物、TestLibraryMetadataPaginationBoundsFileReads、TestLibrarySeriesPage、TestLibraryFilteredSeriesPageWithStaleStatistics、TestRecent 及对应 handler 回归。核对真实测试名称、非 skip；扩展已有十万/六十万 fixture 而非另建框架。
5. 记录新旧候选/完整页执行计划和必要剩余成本，验证空页准确总数、统计滞后和稀疏库不退化；运行 go vet 受影响包、git diff --check。代码冻结后单独只读审查，主线程复核发现并重跑测试；子代理不可用时明确记录，不假称独立代理审查通过。
6. 同步规范中确被替代的条款并核对 API catalog，无对外行为变化不改前端。结束前关闭测试服务、删除临时产物；区分代码回归与真实 APP/生产并发未验证部分，不自动重启、提交或归档。

## Hills 补充执行

1. 保存原全局候选作为测试对照，验证多来源/播放/权限/排序/空页；加入 CollectionFolder 字段断言。
2. 轻量红果候选、计数分页单次物化、批量页内范围；复用旧节点投影验证未看数和代表规则，不修改权限/状态语义。
3. 隔离 PostgreSQL 大样本执行实际查询计划，检查候选只执行一次、页内文件和层级访问有界；回归原详情/Latest/Resume/NextUp 和 API 路由。
4. 独立只读审查后由主线程修复并复验，同步契约/API catalog、Web lint/build 和界面检查；关闭所有临时服务，不重启业务或提交归档。

1. 最终确认后 start；完整读取相关规范与待改真实代码，列出最近添加消费者清单。
2. 增加三来源字段、索引和事务维护；隔离 PostgreSQL 回归新增/删除/重绑/层级/并发、派生字段防覆盖和重复迁移。
3. 修改 Web/Emby 最近添加与 DateLastContentAdded 候选，保留权限/状态、DateCreated、标题和上映时间；重点核验红果跨季跨库及三来源全局排序。
4. 执行真实 EXPLAIN 与写入成本检查。独立只读审查，主线程修复和复验；同步数据库、三来源与 API catalog 规范，前端相关检查及 diff 检查。
5. 维护机制可用后执行一次性历史补算；确认并发写入协调方式，必要停写/重启另请授权，核对结果并删除临时操作材料。
6. 关闭调试服务，分别报告代码/历史补算/实机验证状态，不自动提交归档。

## 代码位置

- 模型：internal/model/{metadata,nfo,hongguo}.go。
- 数据库：internal/database/schema_migration.go；参考 tmdb_recheck.go 的触发器模式。
- 写入复核：media_repository_upsert.go、media_repository.go、metadata_repository_merge.go、nfo_repository.go、hongguo_media.go、hongguo_groups.go、service/media_metadata.go。
- 读取：repository/media_view_repository.go、nfo_media_view.go、nfo_library_candidates.go、hongguo_series.go；service/emby_hongguo_library.go、emby_nfo.go、emby_items_detail.go、emby_metadata_scope.go 及混合近期查询。

实施与一次性补算已完成，验证记录见 progress.md。上线补充先优化红果作品候选与页内详情，再按用户追加要求检查普通/NFO 同类路径：普通 Latest 保留短路存在性探测，NFO 非 DateCreated 状态候选避免全文件聚合与重复分集目录扫描。沿用现有字段/权限/状态投影，不增加历史补算、缓存或服务重启。代码验证通过后仍需用户重启新版并进行客户端验收。

本轮追加：先修复红果 ImageURL 全层级查询与失败占位缓存，补真实图片和计划回归；再实测实际首页 Latest/Resume，针对确定慢点优化并独立审查。生产仅只读诊断，隔离 PostgreSQL 验证后关闭，不提交归档。

详情点击补充：详情、季列表和集列表共用身份文件范围；小样本对照原层级输出，大样本检查节点与分集版本加载计划，再以最近失败身份只读采样完整链路。补回归后独立审查，不重启用户服务。

Web/NFO 补充：Web 红果季集及逻辑版本按原生作品限定绑定；NFO 详情、季集和两端版本共用去重后的目标条目范围，保持既有投影。Web 无季号链接先读卡片可见季，再请求默认季，管理员全剧 API 不变。增加十万/六十万文件计划、旧输出权限状态对照及前端加载回归，生产只读抽样，不重启或提交归档。

各库 Latest 补充：按已验证的“轻量候选先排序，资格过滤后 LIMIT”改普通/NFO/红果路径；保留全局 Latest、精确计数列表和 DateCreated。扩展原结果对照与执行计划检查（含前排不合格、空权限、跨库、部分已看、无文件首季），隔离 PostgreSQL 回归后只读抽样最新业务方法，独立审查并关闭测试数据库。

## 海报未看集数追加计划

1. 扩展既有普通、红果、NFO 容器聚合与 payload，不新增查询、schema 或写入。
2. 隔离 PostgreSQL 回归去重、部分/全部已看、新集、取消已看、用户隔离与权限；断言首页、库页、详情、季列表字段及 HTTP JSON，运行既有大样本计划。
3. 同步播放契约及 API catalog，独立只读复核，关闭测试数据库；不重启、提交或归档。

## 持久库归属追加计划（代码与生产补算完成，待实机验收）

1. 最新规划摘要获明确批准后实施；保留此前 51 项工作树修改。主线程完整读取待改文件与所需规范，更新调用方清单；普通/红果字段、数据库维护、Emby Latest、Web 最近添加及相应测试是本次代码边界。
2. 模型新增 nullable 只读 JSONB 集合及 GIN 索引，扩展现有 latest_media_refresh 与纯换库事件识别。复用 latest_media_added_test.go，补多库、最后文件、换库、重绑、移季、批量、级联、并发、回滚、stale Save/UpdateAll 和重复 AutoMigrate。
3. 替换普通 Latest 的成员枚举和对应前缀/回退，接入红果 Latest 与 Web 最近添加候选；原权限、有效状态、页内详情保持不变。未知集合只跳过前置成员筛选、仍执行原资格校验；NFO 复用既有字段。移除仅因本次替换变成未使用的常量/逻辑，不改相邻无关代码。
4. 隔离 PostgreSQL 比较新旧 IDs、顺序与完整 payload，覆盖三来源、大小/新旧库、空/部分命中、跨库全局时间、红果首季无文件及移组、普通部分已看、受限空权限、隐藏库、混合 NULL/数组。执行实际绑定 SQL 的 EXPLAIN ANALYZE，检查访问行数乘 loops 和每次写入成本；GIN 与时间索引不能只做静态断言。
5. 定向命令：配置仅指向隔离 PostgreSQL 的 MEDIASTATION_TEST_POSTGRES_DSN 后，执行 go test ./internal/database -run 'TestLatestMediaAdded' -count=1；再跑 service 的 TestEmbyLatest/TestEmbyLatestCandidatePlans/TestNFOLibraryPagePlans/TestHongGuoLibraryPagePlan 及 repository 的 TestRecentWorkTimeSharedAcrossLibraries/相关最近添加回归。扩大测试前核对真实名称，未匹配/skip 不算通过。随后 go vet ./internal/database ./internal/repository ./internal/service 与 git diff --check。
6. 实现后单独只读复核；同步数据库/共享媒体/红果规范中被本次正式取代的候选契约，接口不变不改 API catalog。对前轮 12 库差异重新只读采样必须先核对运行环境、使用只读事务与超时，不写生产结构或历史数据。
7. 收尾关闭隔离服务、清理临时产物；报告代码、隔离验证、真实客户端、生产补算各自状态。生产补算/结构操作待另行授权，不自动重启、提交或归档。

风险点与回滚：schema_migration/只读字段的重复迁移、latest_media_refresh 的并发快照、emby_metadata_scope 的完整候选、hongguo 的全局合集与库内成员分界。性能不达标不得通过删权限/状态校验或提前 LIMIT 达成；保留旧查询对照直至验收。本次追加已在最终摘要后获得用户“ok”批准；实际生产操作仍遵守单独确认边界。

## 提交归档计划（2026-09-27，用户已确认）

用户已要求提交归档，并回复“ok”确认本节分组。当前改动与本任务历次批准的实施记录一致，没有识别出无关文件；按以下顺序执行，不推送、不重启、不改生产数据。此前定向回归、独立审查及格式检查通过；实际客户端、生产并发和全项目测试的未验收边界继续保留，不因归档改为通过。

1. 业务提交：`perf(library): unify work-level browsing and media state`。时间/库归属维护、三来源列表与详情优化、未看角标及其回归共享修改，合并为一个可独立回滚的提交；包含以下 68 个代码、测试及规范文件。
2. 任务归档：使用 `task.py archive 09-26-work-latest-media-added`，提交本任务的需求、设计、执行计划、进度、上下文清单及归档状态。
3. 会话记录：使用 `add_session.py`，引用第 1 步业务提交，单独记录验证与剩余风险。无其他任务需要同时归档。

### 业务提交文件清单

```text
.trellis/spec/backend/database-guidelines.md
.trellis/spec/backend/hongguo-catalog.md
.trellis/spec/backend/image-variants.md
.trellis/spec/backend/index.md
.trellis/spec/backend/nfo-catalog.md
.trellis/spec/backend/playback-contracts.md
.trellis/spec/backend/shared-media-metadata.md
.trellis/spec/backend/work-level-queries.md
.trellis/spec/frontend/routing-and-loading-contracts.md
internal/database/schema_migration.go
internal/database/latest_media_added.go
internal/database/latest_media_added_test.go
internal/handler/emby_images.go
internal/handler/emby_items_test.go
internal/handler/emby_people_images_test.go
internal/handler/stats.go
internal/handler/stats_test.go
internal/model/hongguo.go
internal/model/media_view.go
internal/model/metadata.go
internal/model/nfo.go
internal/repository/continuation.go
internal/repository/continuation_test.go
internal/repository/hongguo_groups.go
internal/repository/hongguo_media_view.go
internal/repository/hongguo_series.go
internal/repository/library_metadata_repository.go
internal/repository/media_view_logical_test.go
internal/repository/media_view_repository.go
internal/repository/media_view_recent.go
internal/repository/nfo_library_candidates.go
internal/repository/nfo_media_view.go
internal/repository/playback_state.go
internal/repository/search_loading_test.go
internal/repository/work_libraries.go
internal/service/emby_artwork.go
internal/service/emby_global_browse_test.go
internal/service/emby_hongguo.go
internal/service/emby_hongguo_browse.go
internal/service/emby_hongguo_library.go
internal/service/emby_hongguo_library_test.go
internal/service/emby_items_detail.go
internal/service/emby_items_helpers.go
internal/service/emby_items_list.go
internal/service/emby_latest_played_test.go
internal/service/emby_metadata_scope.go
internal/service/emby_movie_items.go
internal/service/emby_movie_library_test.go
internal/service/emby_nfo.go
internal/service/emby_nfo_library_test.go
internal/service/emby_played_hierarchy.go
internal/service/emby_resume_test.go
internal/service/emby_series_pagination_test.go
internal/service/emby_series_payload.go
internal/service/emby_series_played_test.go
internal/service/emby_system.go
internal/service/emby_work_candidates.go
internal/service/emby_work_pagination.go
internal/service/emby_work_pagination_test.go
internal/service/hongguo_playback_test.go
internal/service/library_pagination_test.go
internal/service/media_series.go
internal/service/playback_state_test.go
internal/service/stats.go
internal/service/test_db_test.go
web/scripts/check-series-loading.mjs
web/src/pages/embyApiCatalog.ts
web/src/pages/useLibraryData.ts
```
