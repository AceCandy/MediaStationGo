# 修复红果合并状态筛选加载缓慢

## 问题与证据

红果下载空间默认“下载中”长期停在加载中。实际库约 2.9 万作品、219 万分集；合并的 active 查询在逐作品 EXISTS 内使用多状态 IN，优化器选择状态/租约索引后再过滤来源，重复扫描。只读对照中原查询超过 3 秒被本连接超时取消；一次性活动作品集合约 4ms，四个单状态相关探测约 859ms。

## 范围

仅修正红果 ListWorks 的 active 筛选查询，保留四种处理阶段、总数、完整分集汇总、搜索、排序、分页及既有单状态筛选。增加实际 PostgreSQL 大数据量/空状态/统计过时执行计划与语义回归；更新查询规范，避免只测小样本结果。

## 验收

- [x] active 先取得去重作品集合，不随作品总数重复枚举活动分集。
- [x] 有活动任务、无活动任务、状态统计过时、关键词与越界分页均正确。
- [x] 30 万分集上自定义及通用执行计划通过，复核返回统计与既有语义一致。
- [x] 实际库只读 EXPLAIN 验证优化效果，不改生产数据或索引。
- [x] 独立复核；明确服务源码生效条件。

## 验证计划

扩展 TestHongGuoDownloadWorkPagePlan 和 TestHongGuoDownloadWorkCandidatesPreserveResults，先复现失败，再修正查询。使用隔离临时 PostgreSQL，结束后关闭并删除；实际库仅只读查询且限时。

## 已执行验证

- 修复前 `active-stale`、`active-empty` 大库回归均复现活动索引随作品重复扫描。
- 修复后：TestHongGuoDownloadWorkPagePlan、TestHongGuoDownloadWorkCandidatesPreserveResults、TestDownloadWorkActiveFilter、TestDownloadWorkSearchBeforePagination 全部通过，使用隔离 PostgreSQL，非跳过。
- 30 万分集：active 普通/通用计划约 19ms，统计过时约 23ms，无活动任务约 0.1ms。
- 实际库只读 EXPLAIN：原查询超过 3 秒主动超时；最终代码对应 SQL 普通计划约 53ms、通用计划约 0.9ms，活动索引仅执行一次。
- 未修改生产数据或索引；临时测试数据库已关闭并删除。
- 当前后端由用户通过 go run 启动，源码修复需重启后端后生效；尚未重启或做真实 HTTP 验证。

## 复盘

根因是合并筛选改变查询计划而缺少大库验证。上轮只验证小样本结果和模拟接口页面，未扩展既有执行计划回归。已将多状态、队列统计过时和空活动状态加入回归，并在下载查询规范中记录禁止逐作品重复枚举活动状态集合的约束。

独立只读复核通过：未发现阻断问题，确认参数绑定、关键词追加、去重、分页及完整统计保持正确。
