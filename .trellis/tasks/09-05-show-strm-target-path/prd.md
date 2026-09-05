# 详情页显示 STRM 真实路径

## Goal

在媒体详情页查看当前选中 STRM 文件实时指向的目标路径，同时避免把该读取加入通用媒体详情响应。

## Background

- `Media.path` 是本地 `.strm` 文件路径；扫描时会把当时读取到的目标缓存到 `Media.strm_url`。
- 项目已有 `readLocalSTRMTarget`，负责读取首个有效目标并兼容 BOM、注释、本地绝对路径及 HTTP/HTTPS URL。
- 当前没有按 media ID 单独读取 STRM 文件目标的接口。

## Requirements

- 新增独立的已认证接口，按 media ID 实时读取本地 `.strm` 文件目标。
- 接口必须沿用媒体详情的用户可见性约束，不允许借此读取不可见媒体或任意文件。
- 接口只返回 STRM 目标，不返回完整媒体详情。
- 前端仅在当前选中版本的 `path` 以 `.strm` 结尾时请求该接口。
- 成功读取后，在“本地路径”下方展示“STRM 路径”；切换版本时同步刷新。
- 读取失败或没有有效目标时不影响详情页其他内容，并且不展示过期版本的目标。

## Acceptance Criteria

- [x] 非 STRM 版本不发起 STRM 目标请求，也不显示“STRM 路径”。
- [x] STRM 版本通过独立请求显示文件当前包含的首个有效本地路径或 HTTP/HTTPS URL。
- [x] 切换媒体版本后，请求和展示均对应新的 media ID，旧响应不能覆盖新版本。
- [x] 不可见或不存在的媒体不能通过接口获得目标；响应不包含媒体本地路径等额外字段。
- [x] STRM 文件不可读或没有有效目标时，详情页其余内容保持可用。

## Out of Scope

- 将 STRM 目标加入 `GET /media/:id` 或版本列表响应。
- 编辑、复制、跳转或播放 STRM 目标。
- 解析 STRM 目标之外的文件内容。

## Notes

- 本次为跨前后端的小型功能，需要 `design.md` 与 `implement.md`。
