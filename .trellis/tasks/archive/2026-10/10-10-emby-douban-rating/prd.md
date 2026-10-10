# Emby 返回独立豆瓣评分

本记录补录已完成的直接修改，用户已授权提交归档。

## 需求与验收

- 含 CommunityRating 的条目有自身独立豆瓣评分时，同级返回 DoubanRating；无有效评分时省略。
- 电影、整剧、季、单集的列表和详情一致；不改变 CommunityRating。
- 不继承父级评分，不依赖 Fields=ProviderIds；列表按页批量加载。
- NFO、红果、黄果 AI 当前无独立豆瓣数据，保持省略。
- 同步共享 Emby 类型和接口目录说明。

## 完成与验证

- 已完成实现及独立只读复核，未发现问题。
- 在临时隔离 PostgreSQL 中通过评分回归、查询次数、容器详情及既有评分解析测试。
- Go 命令：go test ./internal/service ./internal/repository -run 'TestEmby.*(Douban|PayloadQueries|FolderPayload|ContainerDetail)|TestDoubanSnapshotRating|TestMediaViewDoubanRatings' -count=1。
- 前端 npm run lint、npm run build 通过；web/scripts/check-nextup.mjs 的浏览器检查通过。
- git diff --check 通过；临时数据库、预览服务和测试临时文件已关闭并清理。
- 未执行全量 Go 测试或实际播放器验证；客户端展示仍取决于其扩展字段支持。
- 原有未跟踪文件 core 与本任务无关，保留且不提交；未推送或部署。
