# 设计：将成人刮削移出常规工作链

## 边界

保留 `AdultProvider`、成人 API 配置、番号解析、本地成人元数据、NSFW、成人库分类和显式手动入口。
只改变服务层何时调用 `AdultProvider.Search`。

## 调用策略

| 场景 | 成人源行为 |
| --- | --- |
| 扫描后自动刮削 | 不调用 |
| 单媒体/媒体库常规重刮 | 不调用 |
| 修复重刮、STRM 刷新刮削 | 不调用 |
| 手动搜索选择 `all` 或非成人 provider | 不调用 |
| 非成人类型的整理/重分类 | 不调用 |
| 手动明确选择 `adult` provider/source | 调用 |
| 整理明确选择 `mediaType=adult` | 调用 |

## 实现

1. 从 `ScraperService.EnrichOneWithOptions` 删除成人 provider 的预检分支。所有常规刮削入口最终都汇入该方法，因此不在每个 handler/scanner 重复加开关。
2. `ScraperService.AnyEnabled` 不再把成人 provider 计作常规 provider，避免它单独触发自动刮削任务。
3. `OrganizerService.lookupOrganizeAdultMetadata` 仅接受规范化后的显式 `adult` 类型；普通类型即使路径看起来像番号也立即返回。
4. 保留手动搜索和手动应用中的成人 provider 分支，不改 API 或前端。

## 兼容性与回滚

- 无数据库、配置或 API 迁移。
- 已匹配的成人元数据保持不变。
- 回滚只需恢复上述两个服务分支及对应测试预期。

## 风险

- 成人库若依赖扫描后自动成人刮削，将不再自动匹配；用户需使用显式手动成人刮削或显式成人整理。这是本任务的预期行为。
