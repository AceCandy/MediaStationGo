# 完成与验证记录

- [x] 核对实际超时日志及旧查询计划，确认全库节点聚合和逐集写入路径。
- [x] 实现目标范围读取及批量状态写入，保留权限、去重、事务和缓存契约。
- [x] 独立只读复核，无确认缺陷；补齐未归组、无分集绑定和新增集回归。
- [x] 更新 backend/playback-contracts.md，记录写状态禁止全库展示聚合的约束。
- [x] 所有数据库测试使用隔离 PostgreSQL 17，无跳过；测试容器已关闭并自动移除。

已通过命令（运行时设置 MEDIASTATION_TEST_POSTGRES_DSN 指向隔离测试库）：

```sh
go test ./internal/service -run '^TestHongGuoLibraryPagePlan$' -count=1 -v
go test ./internal/service ./internal/repository ./internal/handler -run 'Test(HongGuoContainerPlayed|HongGuoEmbyPlayable|HongGuoLibraryPagingAndLatest|HongGuoLibraryPageMatchesHierarchy|HongGuoBindingGrouping|HongGuoConcurrentProgress|HongGuoPlaybackStatistics|HongGuoHTTPAccess|EmbyHongGuoDetailClick|EmbyMarkPlayed|EmbyPlayedHierarchy|EmbySeriesAndSeasonPlayed|PlaybackStateReplayAndDeletedVersion|ContinuationCrossSeason)' -count=1
go vet ./internal/service ./internal/repository ./internal/handler
git diff --check
```

采样结果：60 万文件中，100 集季写入约 11.8 ms，仅访问 100 个媒体行；300 集合集约 18.5 ms，仅访问 300 个媒体行，目标读取无 JIT。528 集 HTTP 标记约 89.7 ms、取消约 71.7 ms，含成功后的详情状态查询。

未验证：部署环境、YAMBy 实机、并发压力及全仓测试；未修改前端，无前端构建。线上响应仍需部署后验收。
