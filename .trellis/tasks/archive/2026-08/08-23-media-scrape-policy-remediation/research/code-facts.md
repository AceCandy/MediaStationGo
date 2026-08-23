# 代码事实：媒体入库刮削策略与失败处置

## 任务与状态

- “媒体入库刮削”定义为事件任务且没有手动 action：`internal/service/task_definitions.go:51-60`。
- worker 只领取空状态或 `pending`，不会自动领取 `error`/`no_match`：`internal/service/scrape_worker.go:119-140`。
- `ResetLibraryScrape` 已能将一个库的 `error`/`no_match` 清错并重新置为 `pending`，随后唤醒现有 worker：`internal/service/scrape_worker.go:192-204`。
- 现有并发边界为三个媒体 worker 与一个串行 catalog worker；任务历史不是恢复依据：`.trellis/spec/backend/background-task-execution.md`。

## 自动触发与日志

- 扫描完成当前同时受 `autoScrape`、常规 provider 可用性和 `scrape.auto_on_scan` 设置控制：`internal/service/scanner_scan.go:211-230`、`internal/service/scanner_prune.go:133-147`。
- watcher 入库后也读取 `scrape.auto_on_scan` 才唤醒 worker：`internal/service/watcher.go:328-371`。
- scanner 的精确 canonical 命中发生在 `MediaRepository.Upsert -> ResolveMetadata`，会直接写 `matched`，但没有 scrape task：`internal/repository/media_repository_upsert.go:26-61`。
- scraper worker 为每个候选组创建 `TaskKindScrape`，并写入统一的 `media_scrape` 定义日志：`internal/service/scrape_worker.go:18-68`、`internal/service/task_definitions.go:51-60`。
- scraper 内的 canonical 快速命中、provider 成功和 NFO 成功最终都落入同一 worker task，但当前 task detail 无法辨别来源：`internal/service/scraper.go:60-70`、`internal/service/scrape_worker.go:84-113`。
- `Match.Source` 已携带 provider 名称，NFO hub 事件已有 `source=local_nfo`；来源可作为一次执行的临时结果写入 `TaskUpdate.Details`，无需增加数据库字段：`internal/service/tmdb_types.go:6-34`、`internal/service/scraper_local_metadata.go:185-225`。
- scanner 必须继续只绑定已存在的精确 canonical metadata，不能提前制造本地 canonical：`.trellis/spec/backend/shared-media-metadata.md`。

## NFO 与新库类型

- `Library.Type` 是最长 16 字符的字符串；现有 UI 类型为 `movie/tv/variety/anime/music`，所以新增内部值必须保持在 16 字符内：`internal/model/library_media.go:4-10`、`web/src/pages/AdminLibraryPanelSections.tsx:67-74`。
- NFO 读取已区分电影与系列：电影支持同名、`movie.nfo`、目录同名等候选；系列合并 show 层和单集同名 NFO：`internal/service/local_metadata_read.go:13-120`。
- NFO XML 损坏会返回错误；缺失 NFO 当前退化为图片或空 metadata：`internal/service/local_metadata_read.go:13-27,84-120`。
- 系列判断散落于 scraper、organizer、Emby 和 Web，新增 `nfo_tv` 必须同步所有真实 `Library.Type` 分支：`internal/service/scraper_query.go:108-130`、`internal/service/scraper_service.go:145-164`、`internal/service/manual_scrape_providers.go:62-142`、`internal/service/organizer_paths.go:247-254`、`internal/service/emby_movie_items.go:175-208`、`web/src/pages/librariesPageModel.ts:11-13`、`web/src/pages/useLibraryData.ts:163-166`。
- `inferLibraryKind` 会优先按名称/路径推断且其 normalizer 当前不认识新类型；显式选择的 `nfo_movie/nfo_tv` 必须在后端服务边界被识别并保留：`internal/service/media_paths.go:11-30`、`internal/service/organizer_directory_source.go:120-136`。
- canonical 层无需新 kind：非常规电影继续用 Movie，非常规剧集继续用 Series/Season/Episode。

## Web 失败处置

- `Media` 已持久化 `scrape_status` 和 `scrape_error`，但 Web 类型尚未暴露错误文本：`internal/model/library_media.go:60-62`、`web/src/types/media.ts:1-40`。
- `MediaView` 使用 metadata inner join，未匹配媒体不应通过普通媒体列表展示；失败列表需要专用安全 DTO/query，不能放宽共享 join：`.trellis/spec/backend/shared-media-metadata.md`。
- 现有手动搜索/应用接口与 `ManualScrapeDialog` 可供普通库 `no_match` 复用：`internal/handler/manual_scrape.go:33-113`、`web/src/components/ManualScrapeDialog.tsx:11-65`。
- 当前任务页按定义打开每日文本日志；`TaskUpdate.Details` 是统一 operator-facing 明细入口：`web/src/pages/TasksPage.tsx:262-310`、`.trellis/spec/backend/background-task-execution.md`。
