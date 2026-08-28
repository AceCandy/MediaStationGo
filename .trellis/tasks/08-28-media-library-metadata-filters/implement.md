# 实施计划

1. 清理媒体库批量入口
   - 删除单库页批量操作 UI、状态、处理函数及无调用前端 API。
   - 删除媒体库总览的 retired 修复重刮入口。
   - 删除整库强制重刮后端路由/handler；保留轨道与人物回填后端兼容路由，并更新路由测试。
   - 验证：`rg` 确认 retired/危险入口和对应前端调用不存在，任务中心定义及兼容路由仍存在。

2. 统一每集图片默认行为
   - 删除单库、媒体详情和手动刮削对话框中的 `EpisodeArtworkToggle` 使用及由此产生的无调用代码。
   - 移除异步刮削 worker 显式跳过每集图片的选项，保留服务层默认开启语义。
   - 增加或调整最小服务测试，验证队列刮削不再关闭 episode artwork。

3. 实现电影卡片筛选
   - 扩展前端请求选项、handler 参数、服务筛选与仓储共享查询。
   - 将筛选值加入请求去重键和后端缓存键。
   - 仓储测试覆盖无海报、含/不含汉字标题、组合条件及 total。

4. 实现剧集卡片筛选
   - 为剧集列表传递同一查询参数。
   - 在剧集聚合后、分页前按最终展示卡片筛选。
   - 服务/handler 测试验证整部剧维度、组合条件和分页数量。

5. 接入单库页面筛选控件
   - 管理员可见两个切换项，状态传入 `useLibraryData`。
   - 筛选变化时重置加载版本、列表和页码，继续加载沿用当前条件。
   - 人工检查非管理员不显示控件、空结果和取消筛选行为。

6. 独立质量复核
   - 使用 `trellis-check` 做 spec、diff、调用链、lint/type-check/相关测试审查。
   - 默认只运行前端定向检查和 Go 相关包测试，不执行全量构建；若验证需要扩大范围，先向用户说明并取得批准。

## 预计修改范围

- `web/src/pages/LibraryPage*.tsx`、`useLibraryData.ts`、`useLibraryAdminActions.tsx`：页面入口、筛选状态和数据加载。
- `web/src/pages/MediaDetail*.tsx`、`useMediaDetailPageState.ts`、`web/src/components/ManualScrapeDialog*`：移除每集图片开关并使用默认行为。
- `web/src/pages/LibrariesPage.tsx` 及相关 header：删除 retired 全库入口。
- `web/src/api/library.ts`、`web/src/api/tools.ts`：请求契约与死 API 清理。
- `internal/handler/` 媒体/剧集列表、专用批量路由及相关测试。
- `internal/service/` 媒体列表、剧集聚合、刮削 worker 及相关测试。
- `internal/repository/media_view_repository.go` 及测试：电影卡片 SQL 筛选。

不修改数据库 schema，不新增依赖，不改任务中心业务逻辑。
