# 将成人刮削移出常规工作链

## Goal

防止普通电影、剧集在扫描后自动刮削或常规重刮时误触发 JavDB/JavBus，
同时保留成人刮削实现与配置，供未来或显式入口使用。

## Requirements

- 成人刮削 provider、番号解析、API 配置和已有元数据结构继续保留。
- 普通扫描后的自动刮削不得调用成人源。
- 常规单媒体重刮、媒体库重刮和整理后刮削不得隐式调用成人源。
- 手动搜索选择 `all` 或非成人 provider 时不得调用成人源。
- 用户明确选择 `adult` provider 的手动搜索和手动应用继续调用成人源。
- 用户明确以 `mediaType=adult` 执行整理时，继续允许成人元数据参与命名。
- 文件名或路径包含明确外部 ID 时，继续按现有外部 ID 刮削逻辑处理。
- 不迁移或删除既有数据库数据，不改变通用 `NSFW` 字段的 API 兼容性。
- 变更应集中在成人 provider 接入常规工作链的位置，不复制刮削流程或新增抽象。

## Acceptance Criteria

- [x] 常规刮削媒体时，即使路径包含 `STRM-115` 等成人番号形态，也不会请求 JavDB/JavBus。
- [x] 扫描后自动刮削仍能使用 TMDB 等现有非成人 provider。
- [x] 文件名或路径中的明确 TMDB ID 仍能直接按 ID 匹配。
- [x] 成人 provider、配置页和解析代码仍然存在。
- [x] 手动指定 `adult` provider 仍能返回并应用成人匹配。
- [x] 普通手动搜索仍能返回非成人 provider 结果且不会请求成人源。
- [x] 显式选择成人类型的整理流程仍能使用成人元数据。
- [x] 针对常规刮削不再调用成人源的回归测试通过。

## Out of Scope

- 删除成人刮削代码或配置。
- 删除或迁移 `NSFW` 等既有数据字段。
- 修改成人网站抓取实现、站点列表或反爬策略。
- 修改成人媒体库分类、可见性控制或显式成人整理行为。

## Notes

- 当前普通刮削在 `ScraperService.EnrichOneWithOptions` 中先尝试成人源，再尝试明确外部 ID 和普通 provider。
- 现有显式入口是 `ManualSearch(..., provider="adult", mediaType="adult")`、手动应用 `source="adult"`，以及整理参数 `mediaType="adult"`。
