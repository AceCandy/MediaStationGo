# 实施与验证记录

## 变更

- libraryAsView 补红果 tvshows 映射，Views 与库详情共享。
- 新增作品层候选分页：可见文件日期/有效播放状态先聚合到源作品，再按官方合集分页；页内 work IDs 限定原节点投影。
- 指定红果库 Latest 在 NFO 路由分支前统一处理，最近可见文件时间降序，不统计作品总数。
- DateLastContentAdded 使用 MAX，DateCreated 保留 MIN；特殊筛选和季集继续原路径。
- 同步接口目录和红果规范，没有 schema、索引、部署或服务重启操作。

## 验证

- 隔离 PostgreSQL 17：TestHongGuoLibraryCollectionType、TestHongGuoLibraryPagingAndLatest、TestHongGuoLibraryPageBoundary、TestHongGuoLibraryPagePlan 全部通过，未跳过。
- 相关 TestHongGuoEmby、TestEmbySourceSearch、TestEmbyHongGuo、TestEmbyItemsExposeSeries、TestEmbySeriesAndSeasonPlayed 回归通过。
- 真实 EXPLAIN ANALYZE：10,000 作品中 6,000 有文件，共 600,000 绑定。作品候选约 1,860 ms，未看 Latest 候选约 2,384 ms；3 项页内详情分别约 5.2/7.3 ms。断言页内 media/binding 扫描不超过 1,000 行次；Latest 不执行作品总数查询。
- 这些是合成数据单次查询计划耗时，不是生产环境、冷缓存、多用户并发或完整 HTTP 延迟；候选仍需汇总可见文件日期/状态。
- Web lint/build、git diff --check 通过。
- 本地 preview + 模拟管理员接口：接口目录搜索、空结果、分类、展开、复制成功/失败反馈通过；390x844、768x1024、1440x900 无文档级横向溢出；非管理员直接访问重定向离开。
- 等待主题动画结束后，main 内容浅色/深色 axe 审查均为 0 violations。首次未等待动画时的瞬态对比度告警未作为持久缺陷。
- 只读独立复核未发现明确实现错误；提出回退边界覆盖缺口，已补 TestHongGuoLibraryPageBoundary，并由主线程复核最终 diff。

## 根因与预防

- 跨层映射漏项：存储支持红果，但共享 CollectionType switch 仍默认电影。
- 入口覆盖缺口：此前搜索优化没有覆盖库浏览与 Latest；NFO 存在性还选择了不同的日期语义。
- 性能边界错误：作品页从全文件展开三层节点后才统计/分页。
- 通过共享映射测试、NFO 有无对照、MIN/MAX 排序测试及真实查询计划约束防止重复；合同已沉淀到 hongguo-catalog.md。不扩展到其它来源重构。

## 未验证与剩余风险

### 2026-09-26 普通标题浏览补充优化

- 用户确认后，将无日期排序、无播放状态筛选的候选改成与 Web 相同的 EXISTS 可见文件判断，不汇总全部文件日期。当前页详情仍读取完整状态和日期。
- 隔离 PostgreSQL 回归与 60 万绑定计划测试通过：标题候选查询实测约 77.2 ms（合成数据单次 EXPLAIN，非生产延迟），文件探测行次限制断言通过；日期与 Latest 路径保持原有汇总。
- 独立只读审查未发现权限、状态丢失或 GORM 语句污染问题。接口契约未变化，因此本轮不重复前端构建与浏览器检查。

- 未部署、未重启现有服务、未操作真实播放器；客户端可能需要刷新缓存的媒体库类型。
- 未执行全部 Go 测试或生产并发压测；特殊过滤回退路径仍可能较慢。
- 未提交或归档，本任务保留待用户验收。
