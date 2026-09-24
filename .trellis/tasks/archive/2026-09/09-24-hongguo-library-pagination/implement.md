# Implementation

1. 调整红果分页查询，复用可见文件范围和物化分页模式。
2. 扩展 `TestHongGuoLibrarySeriesPresentation` 的空关系、无文件、分页和权限断言，在隔离 PostgreSQL 执行相关测试。
3. 使用当前数据库的只读事务对照新旧结果和执行计划；不重启现有业务进程。
4. 执行相关 Web 加载/展示检查与必要质量检查，独立复核 diff，记录结果。
5. 更新分页约束，清理本次临时测试/性能文件和进程；用户确认后提交并归档。

## 验证结果

- 定向 Go 测试通过：`TestHongGuoLibrarySeriesPresentation`、`TestHongGuoBindingGroupingAndStableProgress`、`TestHongGuoEmbyPlayableIdentityAndUserState`、`TestHongGuoHTTPAccessAndStateIsolation`、`TestLibrarySeriesPagePreservesDirectFilesAndTies`、`TestLibraryFilteredSeriesPageWithStaleStatistics`。测试使用随机隔离 PostgreSQL schema，结束自动清理。
- Web 的 `check-series-loading.mjs`、`check-series-presentation.mjs`、`check-task-log.mjs`、`check-hongguo-discover.mjs`、lint 和 build 通过。发现页浏览器检查使用 mock API，复用现有 Vite，未启动额外业务服务。
- 独立代码复核未发现阻断问题；已检查权限范围复用、当前页文件限制、空页总数、排序和稳定身份。gofmt 与 `git diff --check` 通过。
- 当前真实库通过只读、可重复读事务对照：总数 4,692，首页 50 项及末页 42 项的新旧 summaries 完全一致，包含身份、顺序、代表文件、分集数和版本数。
- 同轮实测旧 count 为 4.019 秒，首页旧 page 为 8.901 秒；新 count/page/代表文件合计 0.823 秒。末页旧 page 为 6.006 秒，新合计 1.104 秒。其他负载下新查询曾为 1.33–3.92 秒，这些数字不是重启后的 HTTP 首屏耗时。
- 最终执行计划：存在性判断针对约 21,506 个来源作品走绑定索引，可见来源作品 5,008 个、合并身份 4,692 个；当页文件聚合仅 4,848 行。分页计划临时写块为 0，不再先排序全库约 50 万文件；未修改数据库 JIT 配置。
- 已移除临时真实库诊断测试，保留正式回归断言。未修改业务数据、数据库结构、配置或依赖。

## 未验证与剩余风险

- 未运行全量 Go 测试；已运行上述受影响路径的定向回归。
- 未重启现有后端，尚未验证新代码生效后的真实 HTTP/浏览器端到端耗时；现有页面仍运行旧版查询。
- 后台扫描与数据库连接池竞争仍可能引起耗时波动，本次未调整并发或连接池。
- 用户已确认提交和归档；部署与重启不在本次收尾范围。
