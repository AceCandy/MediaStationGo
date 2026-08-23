# 本任务必须遵守的现有契约

以下为已由主会话完整阅读后筛出的实施/审查约束，原文归属见对应 spec。

## Background task execution

来源：`.trellis/spec/backend/background-task-execution.md`

- 任务历史只做可观测性，`media.scrape_status` 是重试和恢复依据。
- 同一稳定 task definition 的执行写入同一每日任务日志；`TaskUpdate.Details` 只写本次新增明细。
- task execution 必须在业务工作开始前持久化；创建失败不得启动对应工作。
- provider 错误进入 task log 前必须清洗 URL/query/密钥。
- 媒体状态为 `pending/running/matched/no_match/error`；启动恢复 `running -> pending`。
- 保留三个媒体 worker、一个串行 catalog worker和完整 group 原子领取。
- 显式重刮只把业务对象置为 `pending` 并唤醒 worker，不从任务日志恢复。

## Shared media metadata

来源：`.trellis/spec/backend/shared-media-metadata.md`

- scanner 可按精确 provider identity 绑定已经存在的 canonical metadata，但不能由扫描提示制造或覆盖 canonical metadata。
- 无精确 identity 时，扫描媒体保持 unresolved/pending，后续 provider 或合格本地 metadata 才建立链接。
- `MediaView` 使用 metadata inner join；unresolved raw media 不得通过放宽共享 join 进入普通列表。
- provider no-match 才可走普通库的只读本地 fallback；provider error 不得 fallback。
- scanner/scraper 只能读取 NFO/图片边车，不能创建、覆盖、移动或删除它们。
- 扫描和刮削不得移动、重命名、删除、去重或重新分类可播放文件，也不得改变 library/path placement。
- Movie/Series/Season/Episode canonical 层级、父子关系和 identity 规则保持不变。

## Database and Web

来源：`.trellis/spec/backend/database-guidelines.md`、`.trellis/spec/frontend/design-system.md`、`.trellis/spec/guides/cross-layer-thinking-guide.md`

- 删除退休 setting 时只按精确 key 幂等清理，保留所有无关 setting；数据库错误中止迁移。
- 新 Web UI 使用现有 ModalShell、按钮/输入/徽章 primitive 与主题变量，不新增硬编码颜色。
- API 边界统一验证 payload；DTO/TypeScript 类型逐层一致，组件不解析数据库形态。
