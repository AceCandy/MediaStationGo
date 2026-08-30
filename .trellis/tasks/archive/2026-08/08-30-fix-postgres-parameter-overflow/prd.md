# 审计并修复 PostgreSQL 参数超限

## Goal

消除生产运行路径中由无界集合展开导致的 PostgreSQL 扩展协议参数超限，确保数据规模增长时相关任务和请求仍可完成。

## Background

- `people_translation_periodic` 已出现 `extended protocol limited to 65535 parameters`。
- 已确认 `ListPersonWorkContexts` 将全部待翻译人物 ID 展开到单条 `IN` 查询；`ListTranslationCaches` 也会把未截断的翻译查找集合展开为多组参数。
- 项目运行期按现有数据库规范使用 PostgreSQL 预编译/扩展协议，不通过切换简单协议规避问题。

## Requirements

- 审计生产 Go 代码中由 GORM 或原生 SQL 展开的集合参数，并沿调用方确认集合是否存在可靠上限。
- 仅修复参数规模可能随数据库数据或外部输入无界增长、能够逼近 PostgreSQL 65,535 参数上限的可达路径。
- 修复 `people_translation_periodic` 已确认的作品上下文查询，并覆盖同一流程中的翻译缓存查询。
- 修复审计确认的 Telegram 批量解绑、媒体抓取分组、刷新令牌回收、目录版本路径检查和 metadata identifier 批量写入风险。
- 修复进入 SQL 过滤条件前没有数量上限的 Emby ID、人物 ID、媒体库 ID 和可见性 ID 集合。
- 对确认有风险的查询采用项目已有模式或 PostgreSQL/GORM 原生能力控制单条语句参数规模。
- 保持查询结果、排序、去重、错误传播、事务边界和调用方业务语义不变。
- 不增加新依赖，不引入面向未来的通用查询框架。

## Acceptance Criteria

- [ ] `people_translation_periodic` 不再因待翻译人物或缓存查找集合超过 65,535 个绑定参数而失败。
- [ ] 审计发现的其他无界生产查询均已修复，或有项目代码证据证明其参数规模受可靠上限约束。
- [ ] 每条修改后的查询在单次执行中具有明确且远低于 65,535 的参数上限。
- [ ] 无界 ID/路径过滤使用固定数量的 PostgreSQL 数组绑定参数；无界批量 INSERT 使用固定批次。
- [ ] 人物作品上下文仍按原规则选择最近作品，翻译缓存命中与去重结果保持一致。
- [ ] 为 PostgreSQL 数组绑定和批量写入留下可运行的聚焦回归检查。
- [ ] 聚焦测试通过，独立复核未发现遗漏的同类可达路径或行为回归。

## Out of Scope

- `docs/cankao` 下的参考项目。
- 测试、迁移或开发工具中不会进入生产运行的 SQL。
- 已有可靠分页、固定批次或严格输入上限，无法接近协议上限的普通 `IN` 查询。
- 改变 PostgreSQL 运行协议、关闭预编译语句或调整数据库部署配置。
- 与参数超限无关的 SQL 性能重构。
- 自由文本搜索变体生成的动态 LIKE/正则谓词；它不属于集合占位符展开，需另行决定搜索输入上限或查询模型。

## Constraints

- 优先采用最小、可回滚、可验证的修改。
- 只改动审计证据确认需要处理的路径及其聚焦测试。
- 不因审计顺手重构相邻 repository 或 service 代码。
