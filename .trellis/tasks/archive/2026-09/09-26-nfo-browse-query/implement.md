# 执行计划

1. 最终确认后 start，刷新 NFO、数据库、播放状态规范，核对当前红果改动。
2. 复用 TestNFOSeriesHierarchyAndStateIsolation 和 repository 测试基础，隔离 PostgreSQL 增加结果等价、权限、空页与查询范围回归。
3. 修改电影库 EXISTS；拆分 Web 候选/详情统计；优化 Emby 作品层及指定库 Latest。保持普通、特殊、全局路由契约。
4. gofmt，运行相关 NFO/Emby/movie library/红果测试，不接受数据库测试跳过。EXPLAIN ANALYZE 检查真实扫描范围，不能只以 SQL 文本或微型 fixture 证明性能。
5. 独立只读复核后由主线程修复、复验；git diff --check，更新规范与验证记录。若接口契约不变，仅记录 catalog 对比，不无故修改前端。
6. 关闭自建测试服务，报告已验证/未验证和剩余风险，不自动部署或提交。
