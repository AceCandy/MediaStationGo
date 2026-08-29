# 当前实现与运行数据证据

## 运行数据

- 运行库中有 4,325 个带 TMDB Movie identifier 的 metadata，4,314 个已有 TMDB 图片，345 个已有 TMDB snapshot。
- 当前共有 3,981 条缺 TMDB snapshot，其中 3,980 条关联本地媒体，1 条未关联本地媒体；3,971 条同时已有 TMDB 图片，是全部缺快照记录的子集。
- “有 TMDB 数据/图片”与“有 TMDB 原始详情快照”是不同状态；现有投影字段和图片不能还原当次完整 provider 响应。

## 前向写入链路

- `internal/service/scraper_metadata_persistence.go:71`：普通 canonical 持久化当前只对豆瓣 `RawJSON` 写 provider snapshot，TMDB match 的原始 JSON 未写入。
- `internal/service/scraper_tmdb_details.go:14`：搜索候选接受后会补 TMDB 详情，但当前详情结果不保留原始 JSON。
- `internal/service/tmdb_match.go:12`：`GetMovieMatch` / `GetTVMatch` 返回的完整 match 已携带 `RawJSON`，可直接复用。
- `internal/service/tmdb_catalog.go`、`internal/service/tmdb_episode.go`：Season/Episode 详情结果已携带 `RawJSON`。
- `internal/service/catalog_hydration.go:453,526,584`：发现目录的 Movie/Series、Season、Episode 已按现有 repository 幂等写 TMDB snapshot。
- `internal/repository/catalog_repository.go:175-194`：`UpsertProviderSnapshot` 与 `FindProviderSnapshot` 已提供合法 JSON 校验、按 metadata/provider upsert 和读取能力，不需要新表。
- `internal/service/media_metadata.go:31-100,190-211`：手工元数据编辑通过 `replaceManualIdentifiers` 直接替换 TMDB identifier，不请求 provider 详情，是当前会继续制造无快照记录的旁路。

## 一次性回填与任务中心

- `internal/service/service.go:81-105`：`Container.Boot()` 是服务恢复任务状态、启动后台 worker 和 scheduler 的统一入口。
- `internal/repository/setting_repository.go:15-29`：现有 settings repository 可持久化内部完成标记；最小方案不新增 schema。
- `internal/service/task_tracker.go:39-66,124-133,316-356,454-484`：TaskExecution 已持久化 metrics，启动会把遗留 running 标为 interrupted，完成时保留最终 metrics；任务历史不是业务断点。
- `internal/service/task_definitions.go:29-171`：稳定任务定义已支持当前状态、最近执行、历史和 server-owned action。
- `internal/handler/tasks.go:156-233`：媒体轨道回填提供了手动启动、同类互斥、进度更新和完成收口的可复用模式。
- `web/src/api/tasks.ts:3-18`：前端类型已包含 `metrics`。
- `web/src/pages/TasksPage.tsx:539-580`：任务页每 3 秒刷新并支持手动运行；当前定义行尚未通用展示 metrics 数字。

## 约束与结论

- 历史回填是一次性迁移，不注册周期 scheduler，也不在完成后的每次启动重新扫库。
- 内部 setting 只记录“一轮候选枚举已经完整结束”；进程中断或致命查询错误不写完成标记，重启后继续处理仍缺快照的数据。
- 单项 provider 失败不阻断整轮枚举；整轮结束后写完成标记，失败项保留无 snapshot 状态并只由任务中心手动重试。
- 单条成功的唯一检查点仍是 snapshot 是否存在；任务执行和日志只负责观察，不作为候选来源或恢复游标。
- 新产生的 TMDB 数据在正常刮削/匹配链路中立即写快照，因此一次性迁移完成后不需要周期补漏。
- `internal/service/media_delete.go:9-20`、`internal/service/media_library.go:15-35`、`internal/service/scanner_prune.go:12-39,126-147`：单媒体删除、媒体库删除和扫描 prune 均只删除 media，不删除 metadata。
- `internal/model/metadata.go:109-117`：provider snapshot 只在显式物理删除所属 metadata 时级联；普通 media 删除不存在反向级联。
- `internal/repository/metadata_repository_merge.go:11-80,112-133`：metadata merge 是常规生产路径中显式删除 source metadata 的例外；snapshot 会改绑 target，或在 target 已有同 provider 快照时删除重复项。
