# 设计：红果日志去重与历史分类回填

## Boundaries

- 代码修复仅修改红果任务的 notice 更新参数，并在现有红果增量发现测试中验证日志次数。
- 数据回填通过当前配置的 PostgreSQL 连接直接执行一次事务，不把一次性机制留在应用代码中。

## Log Fix

红果 notice 继续使用 `Message` 保存当前任务摘要，但不再把相同文本重复放入 `Details`。任务跟踪器已有“仅当消息变化时追加进度”的契约，可自然得到一条日志；目录批次仍使用不同的摘要 `Message` 和 ID `Details`，不受影响。

选择修复调用方而不是全局去重，是因为 `Message` 与 `Details` 在通用任务契约中语义不同。全局按文本去重可能吞掉其他任务有意保留的详情。

## Backfill Transaction

事务内先计算候选集，再只更新满足以下条件的行：

1. `hongguo_works.source_category` 去空白后为空；
2. 同 `source_id` 的 `hongguo_discoveries.source_category` 属于三个现行分类之一。

更新后在同一事务内检查：候选数等于影响行数；不存在仍可关联但为空的 work；不存在非法非空分类或 work/discovery 非空冲突。任一检查失败则抛错并回滚，否则提交。

不修改 `updated_at` 等其他业务字段，避免一次性分类补值改变刷新、排序或其他时间语义。

## Full Category Match

首轮数据库关联完成后，临时命令复用 `hongguo.Client.Category`，依次从第 1 页扫描 `real-drama`、`comic-drama`、`ai-drama`。以接口返回的原始条目数判断短页，以每分类已见 ID 集合拒绝重复页，并保留 `MaxCategoryPage` 安全上限。

目标集合是 work 或 discovery 中分类为空的 source ID。一个目标只在唯一分类中出现时进入写入集合；多个分类均出现则标记冲突且不写。三个分类全部成功到尾页后，才在一个事务内分别条件更新仍为空的 work 和 discovery。该操作不读写业务检查点，不保存页面摘要，不触发详情刷新。

## Compatibility and Rollback

- SQL 只从空值变为已有 discovery 的同源合法值，重复执行时影响 0 行，具备幂等性。
- 代码修复可通过恢复 notice 的 `Details` 参数回滚。
- 数据提交前由事务保护；提交后 discovery 仍保留分类来源，可按本次候选 source ID 反向清空，但正常情况下不需要执行破坏性回滚。

## Risks

- 运行库可能在计划与执行之间自然补齐部分记录，因此执行时以事务内实时候选数为准，不硬编码 2285。
- 官网已下架或不再出现在任何分类页的历史作品仍无法分类，会保持为空。
