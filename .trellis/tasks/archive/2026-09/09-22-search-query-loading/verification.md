# 搜索优化验收（2026-09-22）

## 已完成

- NFO 搜索代表及展示由逐候选两次 SQL 改为单次批量 SQL；复用资料投影与文件可见性。
- 普通代表文件候选展开 + LATERAL 索引读取，保留 movie / series episode-only、最新文件排序。
- NFO 关键词先物化匹配作品，再检查候选的文件可见性；不改变匹配字段、排序或候选上限。
- 网页首屏 30、显式加载更多，失败重试同页；依据后端 total 判断后续页，短页/空页不阻断后续候选。
- 搜索页、AI 搜索和顶栏联想传递 AbortSignal，保留序号防旧响应。
- SearchHints 复用 BasicSyncInfo，提示字段及三源顺序不变。

## 实测

真实数据库只读事务，现有 OpenSearch，三次中位数；不包括 HTTP、JSON 传输、图片及浏览器渲染。测试负载会影响时延，因此列出两轮区间。

| 场景 | 改前 | 改后 |
| --- | --- | --- |
| 航海王，全量 14 条 | 486–589 ms | 231–299 ms |
| 海贼王，全量 14 条 | 511–574 ms | 226–290 ms |
| 爱情，全量 200 条 | 8837–9153 ms | 361–413 ms |
| 爱情，网页首屏 30 条 | 旧网页一次请求全部结果 | 302–357 ms |
| 爱情，全量 SQL | 211 | 12 |

同版本旁路 overlay 对照：对三个词分别请求 20、30、2000 条，9 组响应的 JSON 摘要、数量、total 全部一致（摘要涵盖列表顺序与字段）。没有保存响应、真实媒体路径或数据库凭据。

EXPLAIN ANALYZE：普通代表文件使用 idx_media_metadata_id 按候选读取，航海王仅从 2549 个候选层级节点读取 3811 条版本记录，不再扫描整个约 38 万行 media。NFO 候选先扫描匹配资料；无命中时文件子计划不执行。关键词匹配本身仍需扫描 NFO 资料文本，是现有范围下保留的耗时。

## 验证

- PostgreSQL repository/service/handler 的 Search、NFO、MediaViewDouban 相关回归通过（排除下述已复现的既有失败）。
- `go test -race`：NFO 批量展示及权限、Emby 三源搜索/提示字段、网页代表排序通过。
- `go vet ./internal/repository ./internal/service ./internal/handler` 通过。
- `npm run lint`、`npm run build` 通过。
- `web/scripts/check-search-loading.mjs`：StrictMode、30/30/5 分页、失败同页重试、短/空页继续、加载更多取消、关键词切换、AI 切换、顶栏与卸载取消、旧响应、深浅主题和 390/768/1440 宽度通过。
- Emby 混合三源 fixture：提示 18 次实际查询，完整 Items 22 次；字段、顺序、总数一致，包含非零时长。
- 两个独立只读复核（后端/前端）无明确语义回归；主线程另完成最终 diff 复核。

## 既有失败与未验证

以下 6 项均使用 HEAD 原始搜索文件的 Go overlay 再现，没有回滚或覆盖工作树：

- TestSearchMediaVisibleHonorsLargePosterWallLimit：fixture 缺 media_probe_metadata。
- TestSearchMediaVisibleCanReturnHugeLibraryResultsWhenRequested：同上。
- TestOrganizeMediaUsesEpisodeNFOSeason：同上。
- TestSearchTMDbMatchStillLoadsExtendedDetails：刮削 mock 调用计数不符。
- TestProviderMatchDoesNotMergeLocalNFOFields：既有本地 NFO/刮削资料行为断言失败。
- TestProviderErrorDoesNotFallBackToLocalNFO：未返回测试预期的 provider 错误。

未执行全项目测试、真实 Emby/Jellyfin 客户端端到端与线上发布验证；未更改生产数据、重启生产服务、提交或归档。数据库动态变化下的分页仍沿用现有 offset/total 契约，不提供跨请求快照。

临时只读探针与 baseline overlay 已删除，专用 Vite 服务与浏览器会话已关闭。用户已有其他 dirty 文件未修改。

## 提交归档

验收后用户授权提交归档，代码提交为 `28b9493`。任务直接在 main 完成，无独立 PR 分支；归档使用对应的分支校验豁免。未推送或部署，其他任务的未提交修改保留。
