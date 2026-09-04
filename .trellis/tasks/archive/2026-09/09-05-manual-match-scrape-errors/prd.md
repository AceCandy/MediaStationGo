# 为刮削失败媒体增加手动匹配

## Goal

让管理员可以直接从“媒体入库刮削待处理”中的失败记录发起手动匹配，选择正确的 TMDb、豆瓣等候选并完成 canonical metadata 绑定，而不必反复自动重试。

## Background

- `ScrapeIssuesPanel` 已挂载公共 `ManualScrapeDialog`，但入口仅在 `scrape_status === 'no_match' && !nfoOnly` 时显示；`error` 记录只有“重试”。证据：`web/src/pages/TasksPage.tsx:483-504`。
- 当前仍存在三个手动刮削入口：管理员媒体详情页、电视剧详情页，以及任务中心的非 NFO `no_match` 记录。
- 媒体详情页和电视剧详情页在业务上是 metadata 维度；旧入口只是借 `MediaView` 中的代表 `Media.ID` 调用 media 手动应用接口，入口语义与页面维度不一致。
- 手动应用 API 已通过 `ApplyManualMatch` 和 `UpsertCanonicalWithMerge` 实现 canonical metadata 语义：已有相同 provider/entity kind/external ID 时复用或显式合并，不存在时创建 metadata，随后更新 `Media.MetadataID`。证据：`internal/service/manual_scrape.go:45-65`、`internal/service/scraper_metadata_persistence.go:53-68`、`internal/service/scraper.go:249-277`。

## Requirements

- 非 NFO 媒体的 `error` 与 `no_match` 待处理记录都显示“手动匹配”。
- 点击后复用现有 `ManualScrapeDialog`、搜索 API 和应用 API，不新增弹窗、路由或后端接口。
- 保留现有 provider 选择能力，包括 TMDb 和豆瓣；本任务不收窄其他已支持 provider。
- 保留 NFO-only 行为：继续提示修复本地 NFO，仅提供重试，不开放网络手动匹配。
- 保留“重试”按钮及现有自动刮削行为。
- 手动应用继续遵守既有 canonical metadata 合约：匹配已存在的 metadata 时绑定/合并；不存在时创建并绑定。
- 移除管理员媒体详情页和电视剧详情页的旧手动刮削入口，将手动修复统一收敛到任务中心的具体待处理 Media。

## Acceptance Criteria

- [x] 非 NFO `scrape_status=error` 记录同时显示“手动匹配”和“重试”。
- [x] 非 NFO `scrape_status=no_match` 记录继续显示“手动匹配”和“重试”。
- [x] NFO-only 的 `error` 和 `no_match` 记录均不显示“手动匹配”。
- [x] 从错误记录打开弹窗后，可选择 TMDb 或豆瓣进行搜索并应用候选。
- [x] 应用候选沿用现有 API：已有 canonical metadata 时复用/合并后绑定，不存在时创建后绑定。
- [x] 管理员媒体详情页不再显示“手动匹配刮削”，电视剧详情页不再显示“手动匹配整剧”。
- [x] Web lint、Web build 与 `git diff --check` 通过。

## Out of Scope

- 改变 provider 优先级、TMDb 404 自动回退或错误状态语义。
- 新增 metadata 独立管理页面。
- 修改手动匹配的合并方向、字段覆盖规则或 provider 列表。
- 为项目引入前端测试框架；当前 Web 包没有测试脚本或既有页面测试。

## Risks

- 手动应用是用户确认的显式合并操作，可能替换当前 metadata 的同 provider 标识；本任务沿用现有行为，不扩大后端变更面。

## Notes

- 本任务为轻量前端可达性修复，PRD-only。
