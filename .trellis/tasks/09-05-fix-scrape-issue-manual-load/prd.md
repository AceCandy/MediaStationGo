# 修复待处理媒体手动匹配加载失败

## Goal

让尚未绑定 metadata 的刮削失败或未匹配 Media 也能从任务中心直接打开手动匹配弹窗。

## Background

- `ScrapeIssuesPanel.openManualMatch` 当前先调用 `mediaAPI.get(issue.id)`，失败时显示“媒体信息加载失败”。
- `GET /media/:id` 读取 `MediaView`；`MediaViewRepository.query` 使用 `JOIN metadata_items AS mi ON mi.id = m.metadata_id`，因此没有 metadata 的待处理 Media 不会返回。
- `MediaScrapeIssue` 已包含弹窗实际需要的 `id` 和 `title`；`ManualScrapeDialog` 及其状态逻辑只读取这两个字段。
- 手动搜索和应用接口直接按真实 `Media.ID` 加载原始 Media，不要求调用前端详情接口预取 `MediaView`。

## Requirements

- 任务中心点击“手动匹配”时不得调用 metadata 依赖的媒体详情接口。
- 直接使用待处理记录的 `id/title` 作为弹窗目标。
- 将手动匹配弹窗的目标类型收窄为实际所需的 `id/title`，不伪造完整 `Media`，不使用类型断言绕过检查。
- 保持现有搜索、provider 选择、应用、成功刷新和错误提示行为不变。
- 不修改后端接口、数据库查询或 metadata 合并逻辑。

## Acceptance Criteria

- [x] 无 metadata 的非 NFO `error/no_match` 记录点击“手动匹配”后直接打开弹窗，不显示“媒体信息加载失败”。
- [x] 弹窗默认搜索词仍为待处理记录标题，搜索与应用仍提交该记录的真实 `Media.ID`。
- [x] `ScrapeIssuesPanel` 不再调用 `mediaAPI.get`，也不再维护“加载媒体信息”的异步状态。
- [x] 手动匹配弹窗的目标类型仅要求 `id/title`，所有调用方通过 TypeScript 检查。
- [x] Web lint、Web build 与 `git diff --check` 通过。

## Out of Scope

- 修改 `MediaView` 使其返回未绑定 metadata 的 Media。
- 新增 raw Media 详情接口。
- 修改手动匹配搜索、应用或 canonical metadata 复用/创建语义。
- 引入前端测试框架；当前 Web 包没有测试脚本。

## Risks

- 本修复依赖 `MediaScrapeIssue.id/title` 合约；这两个字段已由现有列表接口定义并用于页面展示。

## Notes

- 轻量前端修复，PRD-only。
