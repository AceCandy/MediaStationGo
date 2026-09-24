# 验证记录

## 已完成

- 原实现由 TestWebSourceSearch 复现普通/NFO 有结果而红果缺失；修复后三来源统一排序分页通过。
- 测试使用临时 PostgreSQL 17 容器与现有随机 schema 隔离辅助，未访问生产数据。
- 定向回归：WebSourceSearch、SearchPreservesMetadataRankAcrossWebAndEmby、EmbySourceSearch、EmbyHongGuoSearch、EmbySearch、HongGuoLibrarySeriesPresentation、HongGuoSearchIndex、NFOSearchBatch、MetadataSearch、RankMetadataSearch、PageMetadataSearch 通过。
- race：WebSourceSearch、RankWebMetadataSearch、EmbySourceSearch、SearchPreservesMetadataRankAcrossWebAndEmby、NFOSearchBatch 通过。
- go vet ./internal/repository ./internal/service 通过。
- node web/tests/search-source-cards.mjs、网页 lint/build、git diff --check 通过。
- 现有 check-search-loading.mjs 在独立 Vite 服务通过：首屏30、翻页失败重试、完整加载、请求取消、顶栏联想、AI模式、StrictMode和明暗主题响应式布局。接口由浏览器模拟。
- 后端和前端分别独立只读复核，无阻塞项；主线程复核关键差异。红果 metadata_id 保持空，媒体和来源 ID 由真实代表文件保留。

## 限制

- 未部署、未重启线上服务；用户已在验证完成后确认提交归档。
- 未对真实线上库进行浏览器联调或生产规模性能测量；真实 OpenSearch 服务未在本轮验证，独立后端调用、可见性过滤和故障回退由测试桩加真实 PostgreSQL 验证。
- 保留现有最多100个搜索候选与红果标题匹配范围。
