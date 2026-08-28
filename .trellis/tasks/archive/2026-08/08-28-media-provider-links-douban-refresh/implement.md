# 实施计划

1. 扩展单媒体详情 provider 状态
   - 在 `MediaView` 增加只读快照标记和 Series TMDb ID。
   - 在 `MediaService.GetMedia` 复用 metadata snapshot/identifier repository 查询，仅为详情响应附加数据。
   - 增加服务或 handler 测试，验证 ID 来源、快照真值及列表不受影响。

2. 增加详情页外部链接
   - 在 `MediaDetailMetadata` 的现有徽章区域展示非空豆瓣/TMDb ID。
   - 按 metadata kind 生成 Movie、Series、Season、Episode 正确链接；快照存在时显示 `✅`。
   - 验证缺失 ID 不渲染、外链安全属性、移动端换行及明暗主题。

3. 扩展豆瓣补齐候选查询
   - 为候选查询传入 24 小时截止时间。
   - 覆盖无快照、冷却中、过期但完整、过期且缺海报/简介/中文标题，以及非 Movie/歧义 ID。
   - 保持 keyset、batch 20、唯一标识和不 JOIN media 的约束。

4. 刷新过期不完整快照
   - 区分首次补齐与强制刷新；刷新必须请求 provider，并在字段与图片持久化成功后替换快照、推进冷却。
   - 保持 fill-only 字段和图片候选/选择并发语义。
   - 失败不推进冷却；成功但仍缺字段也更新 `fetched_at`。

5. 修正任务指标
   - 分开 `updated` 与 `unchanged`，保留失败、跳过和细分指标。
   - 测试零变化不再显示为已补齐，并验证单项失败不阻止后续候选。

6. 独立质量复核
   - 运行相关 Go repository/service/handler 测试、前端 lint/build、`git diff --check`。
   - 使用 `trellis-check` 核对共享元数据规范、API 数据流和 24 小时冷却。

## 预计修改范围

- `internal/model/media_view.go`
- `internal/service/media*.go` 及相关测试
- `internal/repository/metadata_repository.go` 及 PostgreSQL 测试
- `internal/service/douban_enrichment.go` 及测试
- `web/src/types.ts`
- `web/src/pages/MediaDetailMetadata.tsx`
- `.trellis/spec/backend/shared-media-metadata.md`

不修改数据库 schema，不新增依赖，不启动或改动现有服务实例。
