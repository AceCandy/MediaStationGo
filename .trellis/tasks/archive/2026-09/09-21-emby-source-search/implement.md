# 执行与验证

1. 复用索引协调器并新增红果逻辑文档、检索、变更同步和启动预热。
2. 调整 Emby 全局作品搜索入口及候选/最终页边界，保留特殊筛选。
3. PostgreSQL 隔离 schema 测试同步、权限、三来源分页；HTTP 模拟测试 alias 隔离与故障。
4. 运行相关 Go 测试、race、git diff --check；独立只读复核。
5. 更新搜索契约。保留既有工作树修改，不重启用户服务。

## 实现结果

- 普通资料复用原 OpenSearch；红果使用独立 alias，共用连接配置，覆盖资料、合集变更与删除同步。
- NFO 继续数据库检索。各来源候选合并后统一排序分页，当前页才补全详情；有 NFO 不再绕过普通搜索后端。
- 红果候选在索引截断前限制可见身份，数据库复核文件、库权限与标题；索引不可用或已知同步失败时回退数据库。
- 保留层级和复杂播放状态的既有数据库路径，两个来源并行启动预热。
- 独立复核发现并修复无 NFO 时红果播放状态筛选遗漏，以及索引写锁阻塞搜索的问题；最终复核无新增阻塞项。

## 已完成验证

使用现有 PostgreSQL 测试辅助创建隔离 schema，结束自动清理；未输出连接凭据。

```sh
MEDIASTATION_TEST_OPENSEARCH_LIVE=1 go test ./internal/repository ./internal/service \
  -run 'Test(OpenSearch|MetadataSearch|RankMetadataSearch|PageMetadataSearch|HongGuoSearchIndex|HongGuoOfficialAlbums|HongGuoAlbum|EmbySourceSearch|EmbySearch|EmbyHongGuoSearch|NFOSeriesHierarchy|NFOFreshStartup|HongGuoEmby)' \
  -count=1

go test -race ./internal/repository ./internal/service \
  -run 'Test(OpenSearchHongGuoAlias|HongGuoSearchIndex|MetadataSearchBackfill|EmbySourceSearch|EmbyHongGuoSearch)' \
  -count=1

go vet ./internal/repository ./internal/service
git diff --check
```

- 相关回归通过：repository 8.372s，service 10.908s。
- race 通过：repository 4.567s，service 2.910s；vet 与 diff 检查通过。
- 真实 OpenSearch 测试通过：唯一临时索引的创建、写入、alias 激活、中文搜索与可见身份过滤；测试自动删除临时索引。
- 使用当前数据库与配置对新代码执行只读事务探测：搜索“航海王”耗时约 274ms，返回 1 条。临时探测测试已删除，永久回归测试保留。

## 未验证与剩余风险

- 未重启或部署现有后端，未完成播放器端实际请求验收；只读探测不等同于生产请求已修复。
- 生产红果 alias 需随启动预热；未就绪时自动回退数据库，复杂播放状态筛选仍走数据库。
- 索引存在正常近实时延迟；红果同步失败标记为进程内状态，成功重建后恢复索引搜索。
- 未运行全项目测试，未修改生产配置，未提交或归档工作树中的其他任务。
