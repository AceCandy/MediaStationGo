# 实施计划

1. 统一豆瓣大图与 WebP URL 规则
   - 在 `internal/service/douban.go` 集中大图路径派生和当前配置投影。
   - 详情仅保留 `cover.image.large.url → pic.large`，删除小图及兼容字段回退。
   - 将历史修复的重复大图派生逻辑迁入统一位置，并更新调用方。
   - 验证：表驱动测试覆盖字段优先级、无效回退、标准/非标准路径、空配置、配置域名、WebP query 及幂等。

2. 接入搜索、发现和下载链路
   - 搜索 `img` 严格派生大图并应用当前配置。
   - 发现 provider 将 `cover` 严格派生为配置无关大图 URL。
   - `discoverFeedHandler` 在响应、预热和 catalog hydration 共用结果前，只对 `source=douban` 应用当前配置投影。
   - enrichment 和历史修复改用统一公开方法。
   - 验证：搜索/发现 provider 测试、发现缓存配置热更新测试，以及 enrichment/历史修复实际下载请求 URL 断言。

3. 独立复核和质量门
   - 对照 PRD 检查所有豆瓣图片调用方，确认没有残留 `pic.normal`/小图展示或重复 URL 变换。
   - 运行 `go test ./internal/service ./internal/handler -count=1`。
   - 运行 `go vet ./internal/service ./internal/handler` 与 `git diff --check`。
   - 不启动服务、不写生产数据库；当前网络 Fake-IP 环境不作为镜像可达性结论。

4. 完成阶段
   - 更新 `.trellis/spec/backend/douban-cookie-config.md` 中已被本任务替代的图片契约。
   - 如共享图片契约需要同步，仅修改与 WebP 落盘和豆瓣候选直接相关的段落。
   - 对照最终实现和聚焦测试逐项复核更新后的 spec，不保留 `pic.normal` 或“仅替换域名、保留格式”等旧契约。
   - 展示 diff 和验证结果后按 Trellis 流程提交、归档。

## 风险文件与回滚点

- `internal/service/douban.go`：所有豆瓣详情/搜索共用，先用聚焦测试锁定行为。
- `internal/handler/discover_extra.go`：必须限制为 `source=douban`，避免改写其他 provider。
- `internal/service/artwork_backfill.go`：只替换 URL helper 调用，不改变任务扫描、CAS 或旧文件保留逻辑。
- 无 schema/数据迁移；每一步均可按文件撤回。
