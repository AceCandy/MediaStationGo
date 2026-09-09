# Implementation

用户已批准 Web 只读待办及数据库触发器登记，开始实施。变更入口覆盖以 PostgreSQL 触发器集成回归验证。

1. 用户确认最终摘要后启动任务；阅读 database-guidelines、shared-media-metadata、background-task-execution。保留全部其它未提交改动。
2. 完成直接写入口覆盖表与事务核对，特别是批量删除、merge、标识失效、图片资产删除。需要扩大范围时重新确认。
3. 实现模型、幂等迁移、到期索引、登记/领取/续租/条件完成；隔离 PostgreSQL 先验证 token、版本和并发。
4. 接入业务事务登记与有界后代展开；测试回滚、批量变化、自身补全写入。
5. 实现持久游标初始化和低频补偿；覆盖空批、删除重绑、重启和未来冷却保留。
6. 替换季/集 worker 的候选扫描，复用缺失规则和 provider 调用；整合结果持久化与确认保护，保留调度/日志行为。
7. 真实 PostgreSQL 回归及规模计划检查；go vet ./internal/repository ./internal/service ./internal/database；skipped 不计通过。
   同步实现管理员只读待办 API 和 TasksPage 列表，验证鉴权、状态筛选、分页及错误展示，执行 Web lint/build。Web 不增加重试或删除操作。
8. 独立复核，更新规范与验证记录，停止临时服务，报告实际部署未验证项；不提交、不直接写实际库。

## 404 与人工清理扩展（已批准，实施中）

1. 用户已确认 STRM 删除范围与三天复核方案；原只读约束仅放宽此人工清理入口。
2. 实现详情 404 类型识别、受快照/token 保护的独立分类及复核；补状态/日志/冷却 PostgreSQL 与 mock provider 测试。
3. 管理员待办 API 增加类别筛选与按需分页关联文件读取，复用现有目标预览/删除接口；不开放任意路径删除。
4. Web 复用 STRMDeleteDialog，明确版本选择、最终路径和父目录内容警告；补取消、越界、映射变更与权限回归。
5. 独立复核、Go 定向测试/vet、Web lint/build 和隔离浏览器交互；关闭临时服务，不删除实际文件，不重启或提交。
