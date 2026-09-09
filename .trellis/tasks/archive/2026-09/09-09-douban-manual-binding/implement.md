# Implementation

1. Provider 类型确认与有界搜索；覆盖错误的联想类型。
2. 人工绑定事务与后台例外；真实 PostgreSQL 测试。
3. 管理员 API 与权限/错误校验。
4. 独立匹配弹窗、跨类型确认、即时刷新；Web lint/build/交互检查。
5. 独立复核并同步规范，保留其他未提交修改。

## 完整搜索修正

- 手动绑定改为解析完整搜索页面的数据对象，最多 5 个作品；保留自动刮削联想接口。支持官方豆瓣链接及 ID，固定主机读取详情，拒绝任意 URL。
- 受限或无法解析页面返回明确错误；详情类型未确认保留候选但禁用并显示重试提示。绑定事务不变。
- 实测新 provider 路径搜索“卑微”找到 `4049664`，subject 确认 `series`；新增显式 opt-in 的真实上游测试和页面/链接/受限回归。
- 独立复核未发现安全或数据一致性问题；根据复核补充详情获取失败的前端提示。

## 验证记录

- 已完成后端管理员搜索/绑定、类型确认、原子即时补齐、后台人工例外与旧响应保护；前端独立入口、结果类型、跨类型确认和刷新。
- 独立只读审查未发现中高严重性问题；主线程复核关键事务、类型和旧编辑入口。
- 真实 PostgreSQL 测试库：service/handler/repository 的豆瓣绑定、后台、补齐与候选定向测试通过（非跳过）；网络使用模拟响应。
- `go vet ./internal/service ./internal/handler ./internal/repository` 通过。
- Web lint/build、`node web/scripts/check-douban-binding.mjs`、`git diff --check` 通过。
- 本地真实浏览器使用真实组件和模拟接口：390/768/1440 宽度无横向溢出；深浅主题截图检查；未知类型禁用，取消零请求，强制只提交一次并刷新。临时页面、截图已删除，服务和浏览器已关闭。
- 未部署、未提交、未改生产数据，未运行全仓回归。新列由 AutoMigrate 添加；真实豆瓣网络限流/权限限制仍可能导致绑定失败并保留旧数据。
- 扩展执行旧 `TestUpdate.*Metadata` 时，两项既有测试因 fixture 未迁移 `media_probe_metadata` 表失败（`TestUpdateMediaMetadataMarksManualMatch`、`TestUpdateEpisodeMetadataDoesNotModifyParentIdentityOrArtwork`）；没有修改无关 fixture，也不将扩展检查报告为全通过。
- `TestMediaSeriesDetailOwnsMetadataAndUserScope` 单独真实 PostgreSQL 通过：旧编辑入口拒绝新豆瓣 ID 且不修改标题；相同 ID 兼容保存并保留人工类型例外。
- 按用户要求保持未提交状态，不运行会自动提交的任务归档或会话记录脚本。
