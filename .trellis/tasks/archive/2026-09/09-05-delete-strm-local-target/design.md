# Design

## Boundary

行为缺口位于 STRM 目标检查与文件删除之间：页面现在只显示原始目标，缺少管理员可用的本地删除预览和按同一规则执行删除的能力。

本次只改 STRM 目标解析、管理员路由/API 和详情页确认 UI。不会复用会删除数据库记录的媒体删除接口，也不会让前端提交路径。

## API Contract

- `GET /api/admin/media/:id/strm-delete-target`
  - 服务端实时读取具体 `.strm`，解析本地删除目标。
  - 可删除时返回 `{ "target_path": string, "parent_path": string }`。
  - 目标不可删除时返回明确的 4xx；页面不打开确认弹窗。
- `DELETE /api/admin/media/:id/strm-delete-target`
  - 请求体仅为 `{ "delete_parent": boolean }`。
  - 服务端重新解析并校验，以请求时状态计算最终删除路径。
  - 成功返回最终删除的绝对路径，不触碰 `.strm` 或数据库。

## Resolution and Trust Rules

1. 用 Media ID 查询具体记录，要求 `Media.Path` 是 `.strm`，再直接读取该 sidecar 当前内容。
2. 本地绝对目标以现有文件管理 allowed roots 为可信根。
3. HTTP/HTTPS 目标复用 `mapRemoteProbePath`；成功匹配的映射本地前缀作为可信根，即使它未同时出现在文件管理 roots 中。
4. 目标必须是存在的普通文件。
5. 通过真实路径校验目标仍位于可信根内，拒绝符号链接逃逸。
6. `delete_parent=false` 删除文件；`true` 删除 `filepath.Dir(target)`。
7. 最终路径不得是文件系统根或可信根本身，且必须仍位于可信根内。

## UI Flow

`STRM 原始目标 → 点击删除 → GET 管理员预览 → 专用确认弹窗`

弹窗以预览返回的 `target_path` / `parent_path` 做纯显示切换；确认时只发送 `delete_parent`。服务端是最终真相，因此删除前状态变化会导致拒绝或删除最新重新解析且仍安全的目标，而不是信任旧预览路径。

## Compatibility and Rollback

- 保持现有 `GET /api/media/:id/strm-target` 响应不变，普通详情读取不获得新的本地路径字段。
- 新接口位于既有管理员路由下，不改变其他媒体删除和文件管理行为。
- 回滚只需移除新增管理员路由/方法和详情页按钮弹窗，不涉及迁移或数据回滚。
