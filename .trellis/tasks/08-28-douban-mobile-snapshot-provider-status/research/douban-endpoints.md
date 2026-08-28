# internal/ 豆瓣 HTTP/URL 入口核验

扫描范围排除 `docs/cankao`、`node_modules`、`dist`、`build`。仓库存在 `.codegraph/`，先用 `codegraph explore` 定位，再以当前工作树 `rg`/行号核对。

## 事实清单

| Endpoint / URL | 位置与符号 | 用途 | `movie/{id}` 替换判断 |
|---|---|---|---|
| `https://movie.douban.com/j/subject_suggest?q={query}` | `internal/service/douban.go:74-116`, `(*DoubanProvider).Search` | 关键词建议；解码数组并取首项，得到 ID、标题、年份、图片、类型 | **不能直接替换**：这是 query 搜索，不是已知 ID 详情；`movie/{id}` 需要已有 ID |
| `https://movie.douban.com/j/subject_abstract?subject_id={doubanID}` | `internal/service/douban.go:142-165`, `(*DoubanProvider).GetMatchByID` | 已知豆瓣 ID 详情；读取完整原文后交 `doubanMatchFromRawJSON` | **可以替换**：语义与移动端详情 `https://m.douban.com/rexxar/api/v2/movie/{doubanID}` 对应；这是当前快照/按 ID 查询主入口 |
| `https://movie.douban.com/j/subject_abstract?subject_id={doubanID}` | `internal/service/douban.go:218-256`, `(*DoubanProvider).GetEpisodeCountByID` | 已知 ID 获取集数；检查顶层及 `subject`/`data` 下 `episode_count`、`episodes_count`、`episodes`、`eps` | **存在条件替换可能**：同样按 ID，但移动端响应是否含这些集数字段需运行证据确认；不能仅凭 URL 形态断言等价 |
| `https://movie.douban.com/j/search_subjects?{type,tag,sort,page_limit,page_start}` | `internal/service/douban_discover.go:14-87`, `(*DoubanProvider).Discover` | 豆瓣热门/高分电影和热门 TV 分页发现，返回 subjects 列表 | **不能直接替换**：这是列表/筛选分页接口，不是单电影详情 |

## 相关但不是详情 API 的 URL/域名入口

- `internal/service/douban.go:259-266`, `(*DoubanProvider).setHeaders`：所有上述 `movie.douban.com` 请求统一设置 `Referer: https://movie.douban.com/`、随机 User-Agent、可选 Cookie；这是请求头，不是额外 endpoint。
- `internal/service/image_proxy_remote_fetch.go:96-112`、`114-140`：对 `doubanio.com` 图片 URL 设置豆瓣 Referer，并允许该图片 host；图片下载入口接收上游 `raw` URL，非 `movie/{id}` 可替换的 metadata endpoint。测试中的图片样例位于 `internal/service/image_proxy_remote_test.go:134,224,265,299`。
- `internal/model/api_config_legacy.go:13` 仅为 `douban.com (cookie)` 注释，不产生网络请求。

## 调用关系核验（与 endpoint 替换影响面有关）

- CodeGraph 显示 `GetMatchByID` 有 6 个调用方，定位到 `internal/service/manual_scrape_providers.go:154-171`、`internal/service/manual_scrape.go:101-105`、`internal/service/scraper_lookup.go:52-64`、`internal/service/scraper_metadata_persistence.go:32-40`、`internal/service/douban_enrichment.go:50-56`，另有测试调用；因此替换该方法会覆盖按 ID 手动刮削、常规 lookup、持久化补抓和 enrichment。
- `Search` 被 `SearchMatch`（`internal/service/douban.go:119-139`）及 `external_search.go` 等调用，仍承担从文本取得豆瓣 ID；不能用仅按 ID 的移动端详情替代。
- `GetEpisodeCount`（`internal/service/douban.go:210-216`）先走 `Search`，再走 `GetEpisodeCountByID`；前者仍不可由详情 URL 取代。

## 推断与存疑

- **推断**：移动端 `rexxar` 详情最直接覆盖 `GetMatchByID` 的 abstract 请求，因为两者都按同一个 `doubanID` 返回单 subject 详情；解析器当前支持顶层及 `subject`/`data` wrapper（`internal/service/douban.go:168-207`）。
- **存疑**：`GetEpisodeCountByID` 是否应改用移动端详情，取决于 `rexxar` 实际响应是否提供上述集数字段；当前代码/静态搜索没有该响应字段证据。
- **未发现**：`internal/` 中除上述入口外，没有 `m.douban.com`、`rexxar` 或其他通过常量拼接形成的豆瓣 metadata URL；`rg` 命中仅为上述 endpoint、Referer 与图片域名处理。
