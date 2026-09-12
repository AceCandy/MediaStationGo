# 实施与验证

1. 优化仓库计数，增加 summary/items 请求模式；默认请求保持兼容。
2. 更新任务页数量请求和弹窗独立加载生命周期。
3. 在独立临时 PostgreSQL 中验证既有语义、请求模式和规模化计划；前端生命周期脚本、lint/build。
4. 实际库只读比较同一快照下新旧计数及执行计划；独立复核后记录结果，关闭临时数据库。

## 验证结果

- 实际 PostgreSQL 只读执行计划：旧统计 2656.617–3012.596 ms、约 132 万缓冲页访问；优化统计 1167.730 ms、约 28.7 万缓冲页访问。不同时间窗口，不作严格基准承诺。
- 同一 SQL 快照内双向 EXCEPT 比较新旧各状态计数，差异为零。
- 当前 not_found 精确计数 151.334 ms；第二页列表此前实测 197.282 ms，两项约 0.35 秒，仅为 SQL 执行耗时，不代表部署后的端到端延迟。
- 临时 PostgreSQL 15：repository/handler 的 TestTMDbRecheck* 全部通过；含 30,000 集规模化执行计划、无媒体/错误 kind 排除、实时删除、多版本、搜索、排序、空页及 summary/items 权限和兼容性。
- web/scripts/check-recheck-dialog.mjs、npm run lint、npm run build、go vet ./internal/repository ./internal/handler、git diff --check 通过。
- 独立只读代码复核未发现 P0–P2 问题；复核后主线程补充 summary 分页参数边界并移除无用查询包装，再次运行定向测试。
- 未部署或重启运行实例；未执行实际媒体删除或已部署页面浏览器验证。统计和全部状态列表仍存在随数据规模增长的成本。
