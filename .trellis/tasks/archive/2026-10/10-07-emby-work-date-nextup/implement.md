# 实施与验证记录

## 修改

1. 在作品列表入口使用 workDateSortParams，将主排序 DateCreated 映射到 DateLastContentAdded；共享排序解析器及季集查询保留原规则。
2. 红果复用 hongGuoLibraryLatestWorks，删除原库内排序所需的 MIN 文件日期分支。NFO 复用最新时间候选，移除仅服务于旧排序的 created_at 投影。
3. NextUp HTTP handler 固定空列表，内部下一集服务、Resume 和 Web continuation 保留。
4. 普通电影库混合类型按目标库分流；更新响应目录、来源规范和回归测试。

## 验证

测试均通过 MEDIASTATION_TEST_POSTGRES_DSN 指向独立测试数据库，并由 OpenPostgres 创建隔离 schema；未对生产执行迁移。

- 核心排序：TestHongGuoLibraryDateCreatedPlan、TestHongGuoLibraryPagingAndLatest、TestHongGuoLibraryPageMatchesHierarchy、TestNFOLibraryCreatedAtSortUsesWorkLatestTime、TestEmbyLibraryWorkTimeSort、TestHuangGuoAIEmbyPlaybackHierarchyAndPermissions、TestEmbyGlobalWorkDirectBindingsPreserveGroups 通过。
- 回归：TestNFOLibraryPagingMatchesHierarchy、TestNFOLibraryPagePlans、TestEmbyMetadataWorkPageMatchesFileGrouping、TestEmbyFileDateSortQualifiesOnce、TestEmbyItemsCountModes、TestEmbyItemsExposeSeriesSeasonEpisodeHierarchy、TestEmbyItemsKeepSpecialsInSeasonZero、TestEmbyLibraryMixedTypesRespectLibraryType、TestEmbyRandomSortUsesSeed 通过。
- HTTP：TestNextUpRoutesAndWebContinuation、TestNextUpEmptyWithoutService、TestEmbyBrowseRequestErrors、TestEmbyTargetUserRequired 通过。
- 大数据：TestHongGuoLibraryPagePlan 的 60 万文件执行计划检查通过；DateCreated 为首个支持字段，验证库内候选不为日期排序展开文件。
- Web：npm run lint、npm run build、node scripts/check-nextup.mjs 通过，覆盖目录搜索/筛选/复制、三种屏幕宽度、双主题可访问性及管理员访问限制。
- 独立只读复核后核对具体入口；人物 Movie/Series 作品页统一排序，混合季集页仍按原规则处理。
- git diff --check 通过；本轮临时日志已删除，浏览器与本地预览服务已关闭；原有未跟踪 core 文件不纳入提交。

## 经验与回滚

多字段排序按首个支持字段选择路径。DateLastContentAdded,DateCreated 不能验证 DateCreated 分支；本轮已将真实 DateCreated,SortName 参数纳入大数据测试并记录规范。

本轮无 schema 改动，回滚业务提交可恢复原排序、栏目和分流行为。服务端测试不代表 SenPlayer 实机兼容或生产时延。
