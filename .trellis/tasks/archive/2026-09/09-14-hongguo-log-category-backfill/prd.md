# 修复红果重复日志并回填来源分类

## Goal

消除红果发现任务的重复分类通知，并在不猜测、不覆盖已有分类的前提下，一次性补齐当前数据库中可由现有摘要或官网分类页确定的历史 `source_category`。

## Background

- 红果发现任务把同一分类通知同时作为 `TaskUpdate.Message` 和唯一一条 `TaskUpdate.Details` 传入；任务跟踪器分别追加进度和详情，因此同一通知落盘两次。
- `source_category` 在历史作品入库后才引入，旧数据没有显式迁移；原约定依赖作品被重新发现并再次刷新后自然补齐。
- 2026-09-14 只读盘点结果：`hongguo_works` 共 2815 条，其中 2428 条为空；2285 条能与非空且合法的 `hongguo_discoveries.source_category` 精确关联，143 条缺少可靠分类证据。
- `hongguo_discoveries` 另有 67 条分类为空；当前作品表和 typed rank 均不能为它们提供可靠分类。
- 首轮关联回填后剩余 143 条空 work：77 条没有 discovery，66 条的 discovery 也为空；它们均不在当前任一官方榜单中。
- 官网全量匹配结果：三个分类各扫描 34 页并在 8 项短页结束；144 个唯一目标中仅 1 个 work 在 `real-drama` 唯一匹配，更新后剩余 142 个空 work、67 个空 discovery，冲突为 0。

## Requirements

- 红果分类通知每次只写入一条任务日志，不改变任务摘要、指标、目录明细、检查点或扫描行为。
- 修复应局限于红果通知生产者，不改变任务跟踪器对其他任务的通用 `Message` / `Details` 契约。
- 一次性回填只更新 `hongguo_works.source_category` 为空且存在同 `source_id`、分类属于 `real-drama|comic-drama|ai-drama` 的 discovery 记录。
- 已有非空分类不得覆盖；不得从标题、标签、集数、详情快照或总榜推断分类。
- 回填必须在单个事务中执行，并在提交前核对候选数、更新数、冲突数和剩余空值。
- 不新增永久迁移、后台任务、管理接口或一次性脚本文件。
- 对三个现行官网分类从第 1 页扫描至短页/空页，只匹配当前 work 或 discovery 分类为空的 source ID。
- 同一 ID 仅在唯一分类中出现时才可回填；跨分类冲突、请求失败、重复页或未完整到达尾页时不得写库。
- 官网匹配结果在单个事务中写入仍为空的 work/discovery；运行后删除临时工具。

## Out of Scope

- 不补猜测性分类；官网完整分类页仍找不到的记录保持为空。
- 不删除旧 `comic` 数据，不改变分类筛选或发现抓取逻辑。
- 不清理已经写入的历史重复日志。

## Acceptance Criteria

- [x] 增量发现追平检查点时，对应分类通知在新任务日志中只出现一次。
- [x] 同一发现任务的目录 ID 明细、摘要消息和任务指标保持现有行为。
- [x] 针对性 Go 回归测试在配置测试 PostgreSQL 时通过；未配置时明确报告跳过。
- [x] 回填前再次统计候选数据，事务实际更新数与事务内候选数一致。
- [x] 回填后不存在“空 work 分类但同 source_id discovery 分类合法”的记录。
- [x] 回填后所有非空分类仍属于既有合法值集合，且 work/discovery 的非空分类冲突数为 0。
- [x] 报告实际更新数、剩余空 work 数和空 discovery 数，不输出数据库凭据。
- [x] 三个官网分类均从首页完整扫描到短页或空页，且无重复页、请求或解析错误。
- [x] 仅回填唯一分类匹配，冲突和未匹配 ID 保持为空。
- [x] 官网匹配回填后重新报告 work/discovery 更新数、剩余空值和冲突数。
- [x] 临时匹配工具已删除，未进入最终工作树。

## Technical Notes

- 代码证据：`HongGuoService.Run` 的 notice 回调当前把相同文本放入 `Message` 与 `Details`；`TaskTrackerService.update` 会分别落盘。
- 数据权威来源遵循 `hongguo-catalog.md`：分类来自分类发现链路，不能从详情字段猜测。
