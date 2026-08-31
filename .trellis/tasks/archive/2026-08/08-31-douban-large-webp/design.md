# 技术设计

## 边界

由 `DoubanProvider` 统一拥有豆瓣图片 URL 规则。搜索、详情 enrichment、历史修复和发现页最终投影都复用同一个解析入口；前端及通用图片代理不感知豆瓣特例。

## URL 处理契约

1. 保留一个纯函数，将有效的 `/view/photo/<variant>/public/<file>` URL 规范为 `/view/photo/l/public/<file>`；无法识别时返回空，供搜索/发现拒绝不确定的小图。
2. 将现有 `resolveArtworkURL` 提升为供发现页调用的 `ResolveArtworkURL`：
   - 对可识别路径先规范为 `/l/`；无法识别时保留调用方已经确认是 large 的有效 URL。
   - 每次调用读取当前 Douban API 配置。
   - 配置域名时替换 scheme/host，把路径扩展名改成 `.webp`，并同步已知 Qiniu 查询格式。
   - 未配置时只保留大图规范化结果，不改域名和格式。
3. 搜索 `img` 和发现 `cover` 本身是小图来源，必须先经过严格的大图派生；派生失败则 `PosterURL` 为空。

## 数据流

### 普通搜索

`subject_suggest.img → 严格派生 /l/ → ResolveArtworkURL(当前配置) → DoubanMatch.Img → ExternalMediaResult.PosterURL → /api/img`

### 发现页

`search_subjects.cover → 严格派生 /l/ → DiscoverSectionCache(原域名/原格式) → handler 返回前 ResolveArtworkURL(当前配置) → 响应 + WarmExternalArtwork`

缓存返回的是 clone，因此 handler 对结果切片的投影不会污染缓存；配置变化会在下一次响应立即生效。

### 详情与下载

`cover.image.large.url | pic.large → ResolveArtworkURL → ImageProxy.Fetch → ArtworkStore`

历史修复先从快照或候选 URL 选择/派生大图，再走同一 `ResolveArtworkURL`。本地文件扩展名和 MIME 继续由实际响应内容决定。

## 兼容性与回滚

- 不改 API 响应结构、数据库 schema、配置结构或前端调用方式。
- 空配置保持豆瓣原域名与 JPEG 格式，但小图路径仍升级为大图。
- 回滚只需撤回 service/handler 的 URL 投影及相应测试，不涉及数据迁移；已经本地化的 WebP 资产仍是受支持的正常图片资产。

## 取舍

- 不在配置保存时清除发现缓存：缓存持有配置无关 URL、响应前动态投影更少耦合，也覆盖 stale fallback。
- 不增加通用 CDN 格式抽象：只处理已确认存在的豆瓣路径和查询格式。
- 不修改前端：现有图片代理链路已能展示后端返回的 URL。
