# 季集复查归并耗时修复

## 目标
修复变更归并逐条跳过大量已处理记录的查询退化，澄清日志计数与耗时口径。

## 实测依据与范围
只读线上执行计划：领取候选走主键索引，过滤 89188 条记录，耗时 86.110ms，命中 89879 个缓冲页；待归并约 7 万条且在下降。
改动限于队列索引模型/迁移、归并日志及回归测试。不修改租约、冷却、领取顺序、事务边界和网络并发；不清理队列或重跑任务。

## 验收
- 部分索引以 metadata_id 排序且只含 pending 记录；新建与重复升级均正确，先建立替代索引再移除旧索引。
- 真实 PostgreSQL 大量已处理前缀夹带待处理尾部的执行计划使用部分索引，不过滤已处理前缀；覆盖通用预编译计划与 SKIP LOCKED。
- 现有队列事务、租约、冷却测试通过；日志明确次数不是请求数，区分阶段耗时和累计耗时。
- 验证后为运行数据库并发创建同一索引，记录前后执行计划及积压变化。

## 验证结果
- 独立临时 PostgreSQL 15：`go test ./internal/repository ./internal/service ./internal/database -run 'TestTMDbRecheck|TestTMDbMetadataRecheck|TestTMDbEpisodeRecheck' -count=1 -timeout=180s` 通过（database 在此筛选下无匹配测试）。
- 同一临时库：`go test ./internal/database -run 'TestAutoMigrate|TestEnsurePerformanceIndexes' -count=1 -timeout=180s` 通过。
- 独立代码复核无高/中严重问题；`git diff --check` 通过。
- 运行库已成功 `CREATE INDEX CONCURRENTLY` 新索引，`indisvalid/indisready` 均为 true；未修改业务记录或重启应用。
- 同一候选领取 SQL 从 86.110ms / 89879 个缓冲命中降至 0.094ms / 12 个缓冲命中。
- 15:46:38 至 15:47:00，待归并登记从 67028 降至 64572，约 111 条/秒；用户原日志约 14 条/秒。该比较是不同时间窗口实测，非严格基准。
- 新日志尚未部署；未等待整轮 TMDb 网络补全结束。持续新增变更仍可能延长归并阶段，此次保留现有事务与调度语义。
