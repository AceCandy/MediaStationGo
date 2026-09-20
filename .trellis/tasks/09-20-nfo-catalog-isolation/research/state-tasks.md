# 状态、任务分流与 scrape 候选研究

## 已确认事实

### 1. 普通用户状态不能直接复用

- 普通播放进度写入 `PlaybackHistory`，强制要求 `MetadataID` 非空，并有 `Metadata *MetadataItem` 外键：`internal/model/playback_collection.go:5-16`。
- 普通播放事件同样强制 `MetadataID`，唯一键为 `(user_id, session_id, metadata_id)`，并保存 `MediaID`、`LibraryID` 快照：`internal/model/playback_collection.go:18-27`。
- 普通收藏 `Favorite` 强制 `MetadataID`，并外键关联 `metadata_items`：`internal/model/playback_collection.go:29-36`。
- 播放列表及条目也强制 `MetadataID`，条目以 `MediaID` 作为首选版本：`internal/model/playback_collection.go:38-53`。
- `PlaybackService.RecordProgress` 先通过 `MediaView.FindByIDs` 判定来源；红果来源走 `repo.HongGuo.RecordProgress`，普通来源随后拒绝 `MetadataID == ""`：`internal/service/playback.go:76-129`。因此 NFO 独立体系不能把 `metadata_id` 填成代理值来复用普通表。
- 普通历史列表、收藏、播放列表均通过 `MediaView` / `metadata_id` 查询：`internal/service/playback.go:156-231`、`:249-256`、`:284-316`；播放列表增删也按 `metadata_id` 去重：`:319-379`。

### 2. 红果是可复用的隔离模式

- 现有模型明确把红果用户状态和播放事件放在独立表（规范名 `hongguo_user_states`、`hongguo_playback_events`），且 `.trellis/spec/backend/hongguo-catalog.md` 明确写有“用户表 excluded from `HongGuoModels()`”。
- `RecordProgress` 已按 `CatalogSource == model.TaskSystemHongGuo` 分流到红果仓储，并以 `LookupCatalogID` / `EpisodeNum` 维护来源坐标：`internal/service/playback.go:92-114`。
- 最小复用方向是复制“来源分流 + 独立 user-state/event repository + 独立统计投影”的边界，不复用普通 `PlaybackHistory` / `Favorite` / `PlaylistItem` 外键模型。NFO 的身份应使用内部 NFO work/season/episode UUID 或路径绑定，而不是外部 provider ID。

### 3. `media` 已有隔离约束，但普通状态仍会漏入旧路径

- `Media.CatalogSource` 的数据库约束要求：非空来源时 `metadata_id IS NULL`；`LookupCatalogID` 保存来源字符串：`internal/model/library_media.go:32-40`。
- 普通媒体刮削字段仍默认为 `ScrapeStatus=pending`：`internal/model/library_media.go:70-75`。仅依靠 media CHECK 不能阻止后续普通刮削、收藏或播放列表代码尝试关联 metadata。
- 红果 upsert 已在仓储层保留来源绑定并拒绝普通 metadata 关联，可作为 NFO upsert 的结构参考：`internal/repository/hongguo_media.go:50-74`（当前实现只覆盖 `hongguo`，NFO 需独立分支）。

### 4. 任务中心 system 分流

- 任务定义根据 task kind 计算 system，并可按 system 过滤定义：`internal/service/task_definitions.go:118-145`。
- 执行记录筛选对显式 `system` 优先；旧记录 `system=''` 用 kind 回退：common 使用 `CommonTaskKinds`，hongguo 使用 `LEFT(kind,8)='hongguo_'`，catalog 使用“非 common 且非 hongguo_”：`internal/repository/task_execution_repository.go:69-84`。
- 内存任务同样在缺省 system 时调用 `TaskSystemForKind`：`internal/service/task_definitions.go:255-267`；`ListSystem` 直接传入 `TaskExecutionFilter{System: system}`：`:270-272`。
- 因此 NFO 独立后台任务的最小接入点是：新增稳定的 NFO task kind → `TaskSystemForKind` / 定义 registry 映射为独立 `nfo` system → handler/UI 的 system allowlist 与 URL；不能让它落入 catalog 默认分支，否则任务中心会与普通资料混列。旧空 system 记录若有迁移，必须保留 kind 回退兼容。

### 5. 普通 scrape worker 的候选入口与过滤风险

- 扫描后统一唤醒 scrape worker：`internal/service/scanner_post_scan.go:14-17`；watcher 也会唤醒：`internal/service/watcher.go:366-367`。
- 普通 library enrichment 的入口是 `ScraperService.EnrichLibraryDetailedWithOptions`：`internal/service/scraper_library.go:30-72`；该链处理库中待刮削媒体并最终更新 `scrape_status`：`:167-169`、`:198`。
- 普通媒体可被标为 `pending` 的通用路径很多（例如 `internal/repository/media_repository_upsert.go:173-175,229-287`），因此仅在某一个 provider 入口加判断不完整。
- 规范要求独立 NFO “不触发远程资料请求”，且 NFO media 必须 `metadata_id=NULL`；最小可靠过滤点应在普通 enrichment 的候选 SQL/批次生成处统一排除 `catalog_source='nfo'`（以及未来的 NFO source 常量），并在手动 scrape/recheck/manual match 入口再次拒绝。否则 worker 会把 NFO 文件送入 TMDB/豆瓣/Bangumi 等 provider 链。
- `internal/service/media_listing.go:53-76` 的 scrape issues 列表目前按 library、keyword、status 查询，研究中未发现来源过滤；若 NFO 状态要展示，应单独 system/source 过滤，避免普通任务中心把 NFO 误当旧刮削问题。

## 最小完整接入点（供主代理决策）

1. 数据/状态：NFO 独立 user-state、playback-event、favorite、playlist（或明确不支持 playlist）；所有表不得有 `metadata_items` 外键，状态键使用 NFO 内部作品/集身份并保存媒体绑定快照。
2. 服务分流：在 `PlaybackService.RecordProgress` 现有红果分支旁增加 NFO 分支；历史/收藏/播放列表/统计 API 也必须按 source 分流，不能只改播放写入。
3. 普通 worker：在 `EnrichLibraryDetailedWithOptions` 的候选生成/库扫描边界统一排除 NFO source；手动 scrape、recheck、metadata edit 入口加同一 source guard，防止绕过 worker。
4. 任务中心：新增 NFO task kind、system 映射、定义过滤、执行记录过滤和前端 system URL；兼容旧 `system=''` 回退。
5. 迁移：现有 NFO media、普通 `playback_histories` / `favorites` / `playlists` 中若存在关联 metadata，不能直接删除或改写普通行；只能按可证明的路径/媒体绑定迁入独立 NFO 状态，迁移应幂等，并保留无法判定的行供人工处理。

## 迁移风险

- 普通状态表的 `MetadataID NOT NULL` 与 RESTRICT 外键使“把 metadata_id 清空后继续使用原表”不可行。
- 一条 NFO 路径可能历史上被普通刮削写入 metadata；直接按标题/provider ID 迁移会误合并，应按路径/媒体 ID 和 NFO 扫描身份迁移。
- 播放事件的普通唯一键以 metadata_id 为中心，而 NFO 事件应以 NFO source/work/episode 身份为中心；重复 session、同路径多版本、文件删除后的事件保留都需单独处理。
- 普通播放列表条目按 metadata 去重，NFO 不应直接复用，否则不同路径重复作品会被错误合并。
- 任务历史旧记录可能 `system=''`；批量回填 system 若不按既有 kind 规则，会改变历史过滤结果。
- 新增 NFO source guard 若只加在 scanner，不会阻止手动匹配、重刮、recheck 或 API 直接调用。

## PostgreSQL 测试可用方式

- 项目规范明确 PostgreSQL 是唯一运行/测试数据库；测试通过环境变量 `MEDIASTATION_TEST_POSTGRES_DSN` 提供 DSN，并为每个测试使用隔离 schema；缺失时应显式 skip（`.trellis/spec/backend/database-guidelines.md` 的“PostgreSQL-Only Database Runtime”）。
- 不应启动服务或输出 DSN/密钥。可先检查环境变量是否存在（只输出存在/缺失），再按现有测试 helper 运行指定 PostgreSQL 测试；本次未启动服务、未连接数据库、未暴露任何凭据。

## 未覆盖/存疑

- 当前仓库尚未找到 NFO 专属 user-state/playback-event/playlist 模型；`internal/service/nfo_only_test.go` 只覆盖 NFO 资料写入、缺失/损坏 NFO 和 `metadata_id` 不绑定，不覆盖用户状态迁移。
- 本轮未完整追踪所有 HTTP handler 的普通历史/收藏/播放列表/统计路由；上述服务层是确定接入点，handler 需按这些服务调用方再做一次定向核验。
