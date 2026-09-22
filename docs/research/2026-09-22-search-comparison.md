# MediaStationGo、Emby 与 Jellyfin 搜索对比

研究日期：2026-09-22。项目代码基线：`b62d858`。

## 结论

当前实测瓶颈主要在 PostgreSQL 搜索后的结果加载，而非 OpenSearch。
网页与 Emby 兼容接口也不是同一种搜索：匹配字段、返回数量、来源和详情投影不同。
不能用“三个来源、权限检查比较复杂”笼统解释慢，也不能凭公开客户端代码断言 Emby 的内部索引算法。

最重的已复现问题是网页搜索逐条加载 NFO 文件：搜“爱情”返回 200 条，执行 211 次 SQL，约 8.84 秒；其中 100 次 NFO 文件查询累计约 8.02 秒。
普通库挑选代表文件的 SQL 也存在全表扫描，单次约 0.24～0.44 秒。

## 研究范围与测量方法

- 阅读当前项目代码、Jellyfin 服务端和网页源码、Emby 官方 API 客户端。
- Jellyfin 固定 `v12.1`，GitHub Release 时间为 2026-09-15；不把默认分支当作用户安装版本。
- Emby 官方客户端固定提交 `7ddab6d24b60d90d304079d02fdef29c5b3f2568`，`Version.txt` 为 `4.10.0.40 Release`。这不是完整服务端源码。
- 在当前数据库和 OpenSearch 上调用项目服务层，以 PostgreSQL `READ ONLY` 事务保护数据，单条 SQL 超时 8 秒。
- 三个关键词、五条实际服务路径各顺序运行三次；表中为三次中位数。没有清空系统缓存，不是冷启动基准。
- 固定电影/剧集范围、允许全部库及 NSFW，无用户播放状态。测量不含 HTTP 中间件、网络传输、JSON 编码、海报加载和浏览器渲染。
- 对三个热点 SQL 运行只读 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`；只保留表名、行数、循环次数和耗时，不记录绑定值或文件路径。
- 当时数据包含 375,567 个普通文件、8,848 个 NFO 文件，没有红果文件。因此红果查询不是这组实测的主要耗时，不能据此评价红果大库性能。
- 未拿到独立 Emby/Jellyfin 实例在同库、同硬件、同参数下的响应数据，不能给出三款服务的真实倍数排名。

## 项目实测

“Emby 兼容”指 MediaStationGo 自己的 Emby 协议服务，不是独立 Emby Server。

| 关键词 | Emby 兼容，20 条、BasicSyncInfo | 网页，诊断时限制 20 条 | 网页实际页面，请求 2000 条 |
| --- | ---: | ---: | ---: |
| 航海王 | 211 ms，1 条 | 504 ms，14 条 | 486 ms，14 条 |
| 海贼王 | 177 ms，4 条 | 535 ms，14 条 | 511 ms，14 条 |
| 爱情 | 212 ms，20 条 | 531 ms，20 条 | 8,837 ms，200 条 |

同一进程使用相同数据与连接配置，普通 OpenSearch 调用约 5～20 ms，观察到的请求均成功，没有进入普通索引故障回退。
网页“爱情”的未压缩结果体约 416 KB；这只说明传输/解析负担，表中 8.84 秒并不包含传输。

另做一次控制变量探测：让网页也只匹配标题/原名，搜索“航海王”只返回 1 条，仍需约 436 ms，其中代表文件查询约 239 ms。
因此网页更慢不仅因为匹配范围宽；结果加载 SQL 本身也有问题。

## 我们慢在哪里

### 1. 网页 NFO 结果是逐条查询，而且单条查询也扫描大量无关文件

`internal/repository/media_search_repository.go:542` 在每个 NFO ID 上依次调用：

1. `NFOItemViews`：读取该作品的文件。
2. `NFOPresentation`：读取展示字段。

`internal/repository/nfo_media_view.go:79` 使用跨三个层级的条件：

```sql
ni.id = ? OR ns.id = ? OR nw.id = ?
```

它没有只选一个代表文件，而是加载匹配的完整文件列表；上层最终只使用 `files[0].ID`。
一次执行计划显示，先处理 8,829 条绑定，之后排除 8,828 条，最后返回 1 条；约 85 ms。

“爱情”完整网页结果的分组计时：

| 操作 | SQL 次数 | 累计耗时 |
| --- | ---: | ---: |
| NFO 文件详情 | 100 | 8,022.55 ms |
| NFO 展示资料 | 100 | 64.56 ms |
| NFO 搜索候选 | 1 | 237.14 ms |
| 普通库代表文件 | 1 | 424.32 ms |

这是优先级最高的瓶颈：既要改为批量加载，也要使查询从命中的作品/绑定出发；仅把 100 次查询并发会保留大量重复扫描。

### 2. 普通库已命中 ID，代表文件选择仍扫描整个媒体表

`internal/repository/media_search_repository.go:572` 把电影/剧集身份折叠为 `CASE`，再用 `ROW_NUMBER()` 为每个作品选代表文件。
实际计划包含 `media` 和 `metadata_items` 的并行顺序扫描、哈希连接、排序和窗口运算。

计划中的 `media` 节点为平均 128,138 行 × 3 次循环，约等于整个 384,415 行文件表。
100 个命中作品的计划约 440 ms；“航海王”只有 1 个命中也需约 239 ms。

应按已知电影/剧集类型，从命中的资料 ID 及其子级映射到文件，再在这些文件内选代表。
现有迁移已声明 `media(metadata_id, library_id)` 索引；不能未检查实际计划便认定只是缺少索引。

### 3. NFO 候选查询在没有命中时也要付出明显成本

`internal/repository/media_search_repository.go:404` 在普通索引召回后继续查 NFO；`nfo_media_view.go:157` 复用文件展示查询构造可见顶层作品集合。

“航海王”没有 NFO 命中时，这段仍约 138～245 ms，视标题/网页字段范围而变。
“爱情”执行计划先构建约 8,633 个可见身份，再逐个查询标题/简介等匹配，约 247 ms。

应首先减少候选查询携带的展示连接，并使关键词和作品类型能够更早限制待验证身份。
NFO 独立 OpenSearch 可以作为后续选择，但它不能消除第 1 项的逐条详情加载问题。

### 4. 网页和 Emby 的语义、首屏数量及来源不一致

| 项目 | 当前网页普通搜索 | 当前 Emby 兼容全局作品搜索 |
| --- | --- | --- |
| 入口 | `/api/media?q=...` | `Items` 的 `SearchTerm` |
| 匹配字段 | 标题、原名、简介、类型 | 标题、原名 |
| 来源 | 普通资料 + NFO | 普通资料 + NFO + 红果 |
| 分页调用 | 页面请求 2000，循环继续取页 | 按客户端 Limit |
| 候选上限 | 普通最多 100 + NFO 最多 100 | 合并后再截为最多 100 |
| 展示加载 | 代表文件 + 展示资料 | 最终页的协议字段投影 |

代码依据：`web/src/pages/useSearchPage.ts:11,61`、`web/src/api/library.ts:233`、`internal/repository/media_view_repository.go:484`、`internal/repository/media_search_ranking.go:241`、`internal/service/emby_search_items.go:52`。

网页首批返回后会展示结果，并非必须等循环结束；问题是首批本身请求量过大。
在当前每来源候选上限下，2000 不会真的返回 2000 个作品，但会一次取完最多 200 个候选。
后续方案应让首屏按需取页，同时明确候选截断与 total 的语义，不能把这些 total 当作全库精确匹配总量。

### 5. 其他可确认但不是本次首要瓶颈的问题

- 来源查询串行：普通 OpenSearch → 普通资料复核 → NFO → 候选字段补读 → 红果 → 最终页详情。启动索引预热并行不等于查询并行。
- `SearchCandidateDetails` 在候选字段已读过后再次读取排序所需字段，是额外往返；此次成本较小。
- `internal/service/emby_search_hints.go:17` 先完整执行 `Items`，再裁剪成提示。搜“海贼王”时默认 Items 和 Hints 都执行 22 次 SQL；提示体变小，没有相应减少查询。
- 网页输入联想延迟 220 ms，正式页面延迟 300 ms；现有序号机制忽略旧响应，但没有向这些 API 请求传 AbortSignal。快速改词会保留已发出的无用请求。
- 红果先全量取当前可见作品 ID，再作为 OpenSearch terms 过滤；超过 65,536 个可见 ID 回退数据库。这是扩展性风险，此次数据无红果文件，未做性能实测。

## Jellyfin v12.1 的实际做法

### 服务端

`ItemsController` 通过 `SearchManager` 找候选，再加载条目；请求有 Limit 时给搜索提供者的候选量为 Limit × 3。
内置 `SqlSearchProvider` 查询 `BaseItems`，对预规范化的 `CleanName` 使用 `Contains`，对 `OriginalTitle` 使用 `LIKE`。
查询内应用类型、父级、用户权限和版本排除；候选只投影 ID 与分数，经排序和 Take 返回。

这不是“Jellyfin 一定使用全文倒排索引”的证据。也不能说它完全没有二次查询或权限成本；它同样需要加载候选实体，并支持外部搜索提供者。
`SearchManager` 会并行发起内部与外部提供者，但等待两者完成；有外部结果时使用经权限过滤后的外部结果，否则使用内部结果。

`SearchHints` 走专用查询设置较少 DTO 字段，而不是先调用完整 Items 控制器再丢弃字段。

### 网页

- 页面输入防抖 500 ms，比我们正式页面的 300 ms 更长；不能将其可能更快归因于防抖更短。
- 公共搜索请求选少量字段，`enableTotalRecordCount:false`、`imageTypeLimit:1`。
- 主媒体请求 Limit 为 800，并非只取 20 条，不能把“小页”当作该版本的统一优势。
- 人物、艺人、工作室等有独立查询，但主查询的 enabled 依赖这些查询状态；页面在主查询 pending 时显示 Loading。因此不能笼统描述成“所有请求完全并行、每类结果随到随展示”。
- 请求传递 AbortSignal，允许取消过时请求。

可借鉴的是候选只取必要字段、规范化搜索字段、专用轻量提示和取消过期请求。是否更快仍需同库实测。

## Emby 能确认与不能确认的内容

官方 `4.10.0.40` API 客户端可确认 `GET /Items` 支持 `SearchTerm`、`IncludeItemTypes`、`StartIndex`、`Limit`、`Fields`、图片及用户数据开关。
这些开关使客户端可以控制返回范围和负载。

官方公开客户端模型中出现 `EnableTotalRecordCount`，但本次查到的 GetItems 签名没有该参数；不能把模型字段直接等同为该端点的现行行为。
当前 Emby 内部 SQL、分词、索引、缓存和网页请求策略未取得足够官方证据，不用历史 MediaBrowser 或 Jellyfin 源码代替。

## 优化顺序与验收重点

1. 批量加载 NFO 搜索结果，只取页面需要的代表文件和展示字段。验收：结果从 20 增加到 100 时 SQL 次数不再按每条 +2 增长；多版本、整剧、季、隐藏库和排序保持正确。
2. 普通库代表文件查询改为从命中作品出发。验收：EXPLAIN 不再为单作品扫描整个 media 表；多版本代表选择保持原顺序。
3. 精简 NFO 候选 SQL，关键词先缩小作品集合。验收：无匹配词不再构造全量文件层级；权限、标题/简介搜索契约不变。
4. 网页首屏按需分页、补充请求取消，明确与 Emby 的搜索字段及 total 差异。验收：第一屏不用等待所有候选详情；翻页不漏不重。
5. 按实测收益再处理来源并行、专用 Hints、红果可见身份投影，以及是否需要 NFO 独立索引。

不预先承诺优化后的毫秒数；OpenSearch 调参无法解决已测得的 8 秒结果加载成本。

## 外部出处

- [Jellyfin v12.1 发布](https://github.com/jellyfin/jellyfin/releases/tag/v12.1)
- [SqlSearchProvider：匹配、权限、投影及排序](https://github.com/jellyfin/jellyfin/blob/v12.1/Emby.Server.Implementations/Library/Search/SqlSearchProvider.cs#L79)
- [ItemsController：候选到实体加载](https://github.com/jellyfin/jellyfin/blob/v12.1/Jellyfin.Api/Controllers/ItemsController.cs#L352)
- [SearchManager：提供者并行及 Hints](https://github.com/jellyfin/jellyfin/blob/v12.1/Emby.Server.Implementations/Library/Search/SearchManager.cs#L78)
- [网页请求字段及总数开关](https://github.com/jellyfin/jellyfin-web/blob/v12.1/src/apps/legacy/features/search/constants/queryOptions.ts#L3)
- [网页主查询及依赖条件](https://github.com/jellyfin/jellyfin-web/blob/v12.1/src/apps/legacy/features/search/api/useSearchItems.ts#L23)
- [网页 Loading 与显示条件](https://github.com/jellyfin/jellyfin-web/blob/v12.1/src/apps/legacy/features/search/components/SearchResults.tsx#L25)
- [网页 500ms 防抖](https://github.com/jellyfin/jellyfin-web/blob/v12.1/src/apps/legacy/routes/search.tsx#L21)
- [Emby 官方 Items API 客户端](https://github.com/MediaBrowser/Emby.ApiClients/blob/7ddab6d24b60d90d304079d02fdef29c5b3f2568/Clients/Emby.ApiClient/Emby.ApiClient/Api/ItemsServiceApi.cs#L149)
- [Emby 客户端版本](https://github.com/MediaBrowser/Emby.ApiClients/blob/7ddab6d24b60d90d304079d02fdef29c5b3f2568/Clients/Emby.ApiClient/Version.txt)

## 工作边界

本轮只研究与测量，新增本报告；未修改产品代码、数据库结构或索引，未重启服务。
临时只读探测测试已删除。没有执行独立 Emby/Jellyfin 压测，也未验证带实际用户权限、收藏或播放状态的延迟。
