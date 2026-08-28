# DoubanProvider 调用链核验

## 事实

- Provider 入口：`DoubanProvider.Search` 请求 `subject_suggest`，只返回首项（`internal/service/douban.go:74-117`）；`SearchMatch` 直接调用 `d.Search(ctx, query)` 并组装轻量 `Match`（`internal/service/douban.go:119-140`）。
- 正常自动刮削：`ScraperService.lookupWithOutcome` 的 TMDb 失败/不匹配后调用 `s.douban.SearchMatch(ctx, query)`（`internal/service/scraper_lookup.go:46-66`），命中后最终由 `scraper.go` 的 `applyProviderMatchWithOptions` 调 `persistProviderMetadata`（`internal/service/scraper.go:237-257`）。因此正常“按标题搜索”不经过 `GetMatchByID`；持久化阶段若豆瓣 Match 没有 RawJSON，会补调详情。
- 手动搜索：`ManualSearch` → `manualDoubanMatch`（`internal/service/manual_scrape_search.go:78-86`、`internal/service/manual_scrape_providers.go:154-174`）。若查询含 `douban:` ID，先 `GetMatchByID`；否则 `SearchMatch`。手动应用：`ApplyManualMatchWithOptions` → `manualRequestMatch`；请求带 `DoubanID` 时直接 `GetMatchByID`（`internal/service/manual_scrape.go:63-78`、`98-106`），随后同样进入 `applyProviderMatchWithOptions` / `persistProviderMetadata`。
- 历史豆瓣补齐：`runDoubanMovieEnrichment` → `enrichMovieFromDouban`，从 canonical 唯一豆瓣电影标识取得 ID 后直接 `s.douban.GetMatchByID`（`internal/service/douban_enrichment.go:154-183`、`33-70`），填充缺失字段并保存 snapshot；不经过 `Search`/`SearchMatch`。
- `persistProviderMetadata` 是共用详情入口：当 `source == "douban" && len(match.RawJSON) == 0 && DoubanID != ""` 时调用 `GetMatchByID`（`internal/service/scraper_metadata_persistence.go:27-47`），成功后写入 `match.RawJSON`；有 RawJSON 则跳过详情请求。之后有 RawJSON 才 `UpsertProviderSnapshot`（`66-69`）。
- 已有豆瓣 ID 的自动刮削：`matchFromMediaExternalIDsWithOutcome` 在 TMDb 路径后直接调用 `s.douban.GetMatchByID`（`internal/service/scraper_lookup.go:40-64`），命中后返回；这也是正常刮削的一条 ID 优先分支。
- 集数查询：`GetEpisodeCount(query)` 内部为 `Search` → `GetEpisodeCountByID`（`internal/service/douban.go:210-216`）；`GetEpisodeCountByID` 对同一 `subject_abstract` URL 另发请求并解析 episode 字段（`internal/service/douban.go:218-256`）。仓库中除该内部调用外未检出业务调用方（`rg` 仅命中定义和 `GetEpisodeCount` 的第 215 行），当前似乎是未接线 API。

## 改 GetMatchByID 的影响

- 生效：手动按豆瓣 ID 搜索/应用；已有媒体豆瓣 ID 的自动刮削；历史豆瓣电影补齐；`persistProviderMetadata` 对“搜索得到但没有 RawJSON”的豆瓣结果所做的详情补全；以及显式调用 `GetMatchByID` 的测试。
- 不直接生效：标题搜索本身（`Search`/`SearchMatch` 返回的轻量结果）；`GetEpisodeCount` 的首段搜索不受影响，但其随后独立调用 `GetEpisodeCountByID`，所以集数实际请求/解析逻辑不因只改 `GetMatchByID` 改变。
- 正常搜索最终大多会触发 `persistProviderMetadata` 的共用详情入口，因此若结果没有 RawJSON，改动仍会间接影响搜索命中后的持久化；若 Match 已带 RawJSON，则不会。

## 是否绕过 provider

- 元数据/集数的 Douban HTTP 请求均在 `DoubanProvider` 方法内：`subject_suggest`（`douban.go:79`）、`subject_abstract`（`douban.go:147`、`223`）。`DoubanProvider.Discover` 也在 provider 内直接请求 `search_subjects`（`internal/service/douban_discover.go:16-43`）。
- 未检出 service/handler 中绕过 `DoubanProvider` 的豆瓣元数据直接请求。另有 `image_proxy_remote_fetch.go:104-138` 对豆瓣图片做远程图片代理/下载，这是 artwork 传输链，不是 metadata provider 调用。

