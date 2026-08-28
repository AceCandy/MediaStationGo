# 当前实现与运行数据证据

## 运行数据

- 运行库中有 864 个唯一豆瓣 Movie ID、864 个豆瓣快照。
- 864 个快照中，当前解析器支持的海报 URL 字段命中数为 0；豆瓣 poster candidate 为 0，豆瓣来源 selection 仅 3 个。
- 示例 metadata `ef06d14b-e9ae-4d8b-994e-e50faa4945f0` 对应豆瓣 ID `1432701`，快照无图片字段，也没有任何 artwork selection/candidate。
- `subject_abstract?id=1432701` 返回 `short_comment`、评分等摘要字段但没有海报；`rexxar/api/v2/movie/1432701` 返回 `intro`、`cover_url`、`pic.large`、`rating.value` 等完整字段。

## 后端代码

- `internal/service/douban.go:142-207`：`GetMatchByID` 当前只请求 `subject_abstract`；`doubanMatchFromRawJSON` 支持顶层、`subject` 或 `data` payload。
- `internal/service/douban.go:74-116`：`subject_suggest` 是关键词转豆瓣 ID 的搜索入口，不能由按 ID 的移动端详情替代。
- `internal/service/douban.go:218-256`：`GetEpisodeCountByID` 当前另行请求 `subject_abstract`；移动端详情实测对 TV subject 返回 `episodes_count`，可同步切换。
- `internal/service/douban_discover.go:14-87`：`search_subjects` 是分页发现入口，不能由按 ID 的移动端详情替代。
- `internal/service/douban_parse.go:8-40`：字符串解析已递归支持 `normal/large/small/url`，评分解析已递归支持 `value/score`，因此移动端嵌套 `pic.large` 和 `rating.value` 无需新增专用 DTO。
- `internal/service/scraper_metadata_persistence.go:32-69`：豆瓣详情请求失败保留已接受 match；只有有效 `RawJSON` 才保存 provider snapshot。
- `internal/service/douban_enrichment.go:48-93`：补齐真实请求、fill-only 字段、导入豆瓣 poster candidate，并在最后更新快照时间。
- `internal/repository/metadata_repository.go:418-450`：候选查询当前只按无快照或过期且 canonical 缺简介/中文标题/最终海报选择；完整 canonical 的旧 `subject_abstract` 快照不会刷新。
- `internal/repository/artwork_repository.go:136-177`：保存 provider candidate 时，只有没有当前 selection 才提升，已有选择不覆盖。

## 前端代码

- `web/src/pages/MediaDetailMetadata.tsx:53-102`：评分当前仅在大于 0 时出现；provider 外链显示明文 ID 和 emoji `✅`。
- `web/src/types/media.ts:41-44`：详情类型只有 provider snapshot 布尔值，没有三态字段。
- 项目已依赖 `lucide-react`，但没有现成 provider logo 或 Tooltip 组件；可用原生 `title`/`aria-label` 和紧凑 monogram，避免新增依赖。

## 调用链与接口枚举

- 正常自动刮削、手动按 ID 搜索/应用、已有豆瓣 ID lookup、持久化补抓和历史补齐最终都调用 `DoubanProvider.GetMatchByID`；在共享方法切换即可覆盖这些详情链路。
- `GetEpisodeCount(query)` 先调用 `subject_suggest` 搜索 ID，再调用 `GetEpisodeCountByID`；只切换后半段详情请求。
- `internal/` 中未发现绕过 `DoubanProvider` 的其他豆瓣元数据请求。`doubanio.com` 仅用于图片代理/下载，不是详情 API。
- 2026-08-28 实测 `https://m.douban.com/rexxar/api/v2/movie/35588177` 返回《漫长的季节》的 `type: "tv"`、`episodes_count: 12`、`intro`、`cover_url`、`pic.large` 与 `rating.value`。

## 约束

- 移动端接口：`https://m.douban.com/rexxar/api/v2/movie/{doubanID}`，Referer 为对应移动端 subject 页面。
- 主接口失败时可降级 `subject_abstract`；旧/降级 payload 具有 `subject`/`data` wrapper，可与移动端顶层 payload 区分。
- 详情三态描述本地缓存覆盖度，不宣称远端每个可选字段均有值。
