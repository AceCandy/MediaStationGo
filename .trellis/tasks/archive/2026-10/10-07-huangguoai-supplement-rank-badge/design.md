# 设计

黄果 Supplement 候选从独立 work/episode/download 表查询，发现摘要仅提供首次发现排序。入队复用现有 Enqueue 事务，增加内部 onlyNew 模式：持有 work 行锁后检查历史，下载位置冲突则跳过；公开 Enqueue 行为保持。

调度复用红果补充任务的数量上下文、周期计数持久化与汇总日志逻辑，增加独立黄果任务及配置键 huangguoai.download_supplement.*。容器在 Start 前注入下载服务，数量与 enabled/interval 同事务保存。

任务中心现有数量弹窗扩展为两来源共用，提交定义的 key/name；榜单共用 CatalogRankingRow 在相对定位的海报上显示现有绿色样式，保留收起时可读的文本状态。

无需迁移；回滚代码后新增 settings/执行记录可保留，下载队列和已发布文件不被删除。后台错误和日志仅包含安全的汇总数字，不记录黄果标题或路径。
