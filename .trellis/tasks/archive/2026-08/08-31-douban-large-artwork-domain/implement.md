# 豆瓣大图与图片域名配置：实施计划

1. 配置与 URL 契约
   - 将 Douban 标记为支持 `base_url`。
   - 在 API 配置更新入口校验 Douban 图片 origin；空值仍合法。
   - 在 `DoubanProvider` 增加动态图片 URL 解析，配置只替换 scheme/host。
   - 验证：聚焦 API config/Douban 单元测试覆盖空值、合法值、非法值和热更新。

2. 新数据优先保存大图
   - 增加豆瓣专用海报选择 helper，不修改通用 `firstStringFromMap` 的递归顺序。
   - enrichment 下载前应用动态图片域名，继续复用现有 `importRemoteCandidate`。
   - 验证：`douban_snapshot_test.go` 覆盖 `cover.image.large.url → pic.large → pic.normal → 兼容字段`。

3. 历史豆瓣候选安全修复
   - repository 增加豆瓣候选 keyset 分页投影，携带资产尺寸/存储键、快照和当前选择快照。
   - repository 增加事务 CAS：先切候选，当前选择仍与旧豆瓣候选一致时再同步切换。
   - ArtworkStore 复用现有 fetch/prepareAsset，仅新增豆瓣候选修复调用。
   - 服务增加扫描循环，识别缺文件和小图，从快照取大图并应用配置域名；失败保留旧引用。
   - 验证：repository 测试覆盖并发变化、其他 provider 当前选择、豆瓣当前选择同步；service 测试覆盖缺文件/小图/健康大图及下载失败。

4. 任务中心入口
   - 注册“豆瓣图片本地化修复”任务定义、scheduler job 和本地 job bridge；默认自动调度关闭。
   - 验证：任务定义和 scheduler 测试确认可手动运行且保存配置不会启动任务。

5. 独立复核与质量门
   - 运行修改包的聚焦 Go 测试，不执行全量编译。
   - 前端无产品代码改动，只检查现有输入/PUT 契约；若实际改动前端才运行其聚焦 lint/build。
   - 运行 `gofmt`、`git diff --check`，检查 `git diff` 只包含本需求。
   - 使用 `trellis-check` 做独立复核；发现问题后修复并重跑相关检查。

## Rollback Points

- 配置/解析改动可独立回退，空配置本身保留原域名行为。
- 历史修复每条只有新图片成功落盘后才进入事务 CAS；任一步失败不切旧引用。
- 本任务不删除旧文件/资产行，不需要数据恢复脚本。

## Expected Product Files

- `internal/model/api_config.go`
- `internal/service/api_config.go`
- `internal/service/douban.go`
- `internal/service/douban_enrichment.go`
- `internal/repository/artwork_repository.go`
- `internal/service/artwork_store.go`
- `internal/service/artwork_backfill.go`
- `internal/service/task_definitions.go`
- `internal/service/scheduler.go`
- `internal/service/scheduler_local_jobs.go`
- 对应聚焦测试文件

实现时若能复用现有文件内 helper，则不新增生产文件、不引入依赖、不做无关重构。
