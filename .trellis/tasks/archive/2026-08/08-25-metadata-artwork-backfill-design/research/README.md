# Research Index

本目录记录 metadata 图片补齐与硬删除设计的只读事实。产品要求、技术方案和实施清单分别以 `prd.md`、`design.md`、`implement.md` 为准。

## Canonical metadata boundary

- `internal/model/library_media.go`：Media 的 TMDB lookup 字段不是权威 metadata；Media 只保存播放文件事实。
- `internal/model/metadata.go`：权威身份、图片选择、图片资产和 catalog checkpoints 位于共享 metadata 图。
- `internal/repository/media_view_repository.go`：MediaView 的 TMDB ID 与图片均从 metadata identifiers/artworks/assets 投影。
- 核心补图查询无需且不应连接 `media`。

## Existing catalog hydration

- `internal/repository/catalog_repository.go:57-68`：job key 为 `(provider, entity_kind, external_id)`；enqueue 冲突当前 `DoNothing`。
- `internal/repository/catalog_repository.go:71-135`：已有 running 恢复、SKIP LOCKED claim、retry、requeue 和 complete。
- `internal/repository/catalog_repository.go:157-169`：Series 子项当前只按 `catalog_hydrated_at IS NULL` 选择。
- `internal/service/catalog_hydration.go:391-454`：root metadata/artwork checkpoints 独立，TMDB 空图片路径会完成 artwork checkpoint，下载错误会返回失败。
- `internal/service/catalog_hydration.go:483-578`：已有 Season poster 与 Episode still 下载能力。
- `internal/service/catalog_hydration.go:648-663`：catalog artwork 当前直接导入；需要配合 ArtworkRepository 防止覆盖既有 selection。
- `internal/repository/artwork_repository.go:12-43`：SaveSelection 当前在冲突时覆盖 selection 并恢复软删除。

## Scheduler and task center

- `internal/service/scheduler.go:106-145`：SchedulerService 是周期任务唯一 owner，支持默认开关、周期设置和运行互斥。
- `internal/service/scheduler_local_jobs.go:148-153`：people backfill 是“scheduler 薄调用 -> scraper 有界 pass”的现有模式。
- `internal/service/task_definitions.go:51-60`：action=`scheduler` 可复用手动运行、周期配置、历史与日志。
- `web/src/pages/TasksPage.tsx:117-187`：scheduler action 和 schedule config 通用渲染；只有 probe/media scrape 等需要专用参数的 action 才有前端特例。

## Delete semantics

- `internal/model/model.go:13-22`：全部模型历史上统一嵌入带 DeletedAt 的 Base，这是字段广泛存在的来源，不代表所有表都有回收站需求。
- `internal/repository/metadata_repository_merge.go:41-65`：metadata merge 已先迁移引用再 `Unscoped().Delete` source。
- `internal/repository/metadata_repository.go:374-457`：identifier upsert/replace 当前依赖 Unscoped 查询和 tombstone restore。
- `internal/repository/artwork_repository.go:12-99`：selection 当前软删除后恢复；asset 为共享内容哈希实体。
- `internal/service/media_delete.go`、`internal/repository/media_repository.go`：media 已使用硬删除路径，但保留兼容列；本任务不改变 media。
- `internal/database/schema_migration.go`：已有事务删列、幂等 `IF EXISTS`、显式索引和历史软删媒体清理模式。

## PostgreSQL and performance

- `internal/database/database.go`：运行时只接受 PostgreSQL。
- `internal/model/metadata.go`：现有关键索引包括 identifier.metadata_id、artwork `(metadata_id, artwork_type)`、metadata parent_id 和 catalog job 唯一键。
- `internal/testdb/postgres.go`：PostgreSQL tests 使用独立随机 schema，DSN 未配置时 skip。
- 项目没有现成 EXPLAIN 自动化惯例；真实规模 `EXPLAIN (ANALYZE, BUFFERS, VERBOSE)` 应作为上线前人工门禁，而不是推测性创建索引。
