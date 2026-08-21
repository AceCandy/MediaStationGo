# 播放器请求日志记录 Body

## Goal

管理员可在播放器请求日志详情中查看请求 Body，以直接诊断播放器兼容 API 的参数错误。

## Background

- 当前播放器请求日志仅保存 Path 参数、Header、Query、状态码、耗时和 IP。
- `/emby/Sessions/Playing/*` 等请求的关键参数通常位于 JSON Body，现有日志无法解释 400 的具体输入。
- 播放器日志页面仅供管理员访问，但持久化内容仍需复用现有敏感字段脱敏规则并限制大小。

## Requirements

- 仅为现有播放器兼容 API 请求捕获 Body，不扩大到普通 API。
- 保存处理后的 Body 文本，JSON 与非 JSON 请求均可表示；空 Body 保存为空字符串。
- Body 最大保存 64 KiB，超限时必须有明确的截断标记，且不得影响下游 handler 读取完整请求体。
- JSON Body 中命中现有敏感字段规则的值必须脱敏；Header 与 Query 的现有脱敏行为保持不变。
- 既有数据库通过幂等迁移增加 Body 字段，旧日志保持可读。
- 管理员播放器日志详情新增 Body 展示；列表与筛选行为不变。

## Out of Scope

- 不记录响应 Body。
- 不增加新的日志筛选条件。
- 不为普通 API 保存请求 Body。
- 不调整播放器进度校验或 400 响应行为。

## Acceptance Criteria

- [ ] 播放器兼容 API 的 JSON Body 被保存，并通过管理员日志查询接口返回。
- [ ] Body 中的敏感 JSON 字段按现有规则显示为 `[redacted]`。
- [ ] 超过 64 KiB 的 Body 被有界记录并带截断标记，业务 handler 仍能读取完整 Body。
- [ ] 空 Body、非 JSON Body 和迁移前的旧日志可正常查询与展示。
- [ ] 管理员日志详情显示 Body，现有 Path 参数、Header、Query 展示不变。
- [ ] 最小后端测试覆盖 Body 透传、脱敏和大小限制；现有相关测试通过。

## Technical Notes

- 捕获位置复用 `MarkPlayerAPIRequest`，避免全局请求日志中间件预读所有请求。
- 数据库存储使用文本列，避免对非 JSON Body 引入额外结构约束。
- repository 使用现有 GORM model，无需新增专用查询逻辑或依赖。
