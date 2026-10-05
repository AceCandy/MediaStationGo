# 完成核验与提交计划

## 验证

- 新增 PostgreSQL 并发回归在旧代码上全部失败；修复后 ordinary / explicit_merge / mixed 全部通过。规范化标识、逆序输入、真实锁等待验证连续三轮通过。
- 晚到身份冲突拒绝且整个新建事务回滚。
- 既有身份替换、显式递归合并、TMDB 身份失效测试通过。
- 路径提示、版本季目录、共享电影/分集资料、整季入库相关 service 测试通过。
- `go vet ./internal/repository ./internal/service` 和 `git diff --check` 通过。
- 独立审查完成。层级更新疑虑经原文核查：metadataItemUpdates 明确更新 parent_id/season_num/episode_num，并非未更新；该行为未改变。并发测试采纳实际锁等待核验建议。
- 精确数据归并先在事务中演练并回滚，再正式提交。使用 repository.Merge，提交前校验两根及资料树、外部身份与快照、14 个文件、用户状态。提交后只读查询证实 1 作品、7 集、14 文件，每集 2 版本，原三条源资料已移除。
- 已定向刷新搜索索引并限制两个候选 ID 验证，目标保留、源移除。
- 临时归并工具、二进制、备份和 PostgreSQL 数据目录已删除，临时 PostgreSQL 已停止。

## 未验证及运行边界

- 未运行全项目测试和浏览器交互测试；实际媒体库仓储查询及搜索索引已验证。
- 未部署或重启用户现有服务。当前重复数据已经修复，防复发代码需运行新版本后生效。

## 已批准的提交计划

单个提交：`fix(metadata): prevent duplicate works during concurrent ingestion`

只包含：
- `internal/repository/metadata_repository.go`
- `internal/repository/metadata_concurrency_test.go`
- `internal/service/episode_metadata_cleanup.go`
- `internal/service/episode_metadata_cleanup_test.go`
- `.trellis/spec/backend/database-guidelines.md`
- `.trellis/spec/backend/shared-media-metadata.md`
- `.trellis/tasks/10-05-canonical-work-dedup/`

用户已确认提交以上范围，按工作流 3.4 执行。

## 其他工作区修改（排除，不处理）

- `.trellis/spec/backend/emby-api-catalog-sync.md`
- `internal/service/emby_compat.go`
- `internal/service/emby_playback_test.go`
- `internal/service/emby_system.go`
- `internal/service/hongguo_favorite_test.go`
- `web/src/pages/embyApiCatalog.ts`
- `.trellis/tasks/10-05-emby-favorite-library-ids/`
- `.trellis/tasks/10-05-hongguo-arm-app/`
- `.trellis/tasks/archive/2026-10/10-05-android-arm-bytevc2/`
- `internal/service/emby_favorite_libraries.go`
- `internal/service/emby_favorite_libraries_test.go`
