# 豆瓣图片统一大图与 WebP

## Goal

豆瓣搜索、发现、详情下载和历史本地化修复统一使用大图；管理员配置豆瓣图片域名后，所有这些入口使用该域名的 WebP 图片，减少小图和 JPEG 带来的清晰度、流量及上游防盗链问题。

## Background

- 普通搜索请求 `https://movie.douban.com/j/subject_suggest`，读取 `img`；现场样本是 `/view/photo/s_ratio_poster/public/*.jpg` 小图。
- 发现页请求 `https://movie.douban.com/j/search_subjects`，读取 `cover`；现场样本同样是 `s_ratio_poster` 小图。
- 详情快照当前按 `cover.image.large.url → pic.large → pic.normal → 兼容字段` 选择海报，仍可能回退小图。
- 发现页结果会在内存缓存 6 小时；缓存命中、刷新失败回退和正常请求最终都由 `discoverFeedHandler` 返回并用于图片预热。
- 714 条现场快照的 `cover.image.large.url` 均为 `.jpg`；其中 93 条还带有 `imageView2/.../format/jpg` 查询串。
- 本地图片存储已支持按真实内容识别、校验和保存 WebP，无需新增图片依赖。

## Requirements

1. 详情快照只按 `cover.image.large.url`、`pic.large` 的顺序选择有效 HTTP(S) 图片；删除 `pic.normal` 和兼容小图字段回退。首选字段无效时仍继续检查下一个大图字段。
2. 对已知 `/view/photo/<variant>/public/<file>` 豆瓣路径统一派生 `/view/photo/l/public/<file>`：
   - 搜索和发现接口返回的小图只有成功派生后才可展示；非标准路径无法确认大图时返回无图。
   - 详情已明确标记为 large 的有效非标准 URL 可保留，避免误删真实大图。
3. 普通搜索和发现页返回给前端的豆瓣海报都应用“外部 API → Douban”的当前图片域名配置；不处理 TMDb、Bangumi 或其他来源图片。
4. 配置图片域名且配置启用时：
   - 仅替换图片 URL 的 scheme/host，不修改豆瓣 JSON API 域名；
   - 将图片路径最后一个文件扩展名改成 `.webp`；
   - 将已观察到的查询片段 `/format/jpg`、`/format/jpeg` 同步改成 `/format/webp`；
   - 没有文件扩展名的有效非标准 URL 只替换域名，不猜测或追加扩展名。
5. 未配置图片域名、配置禁用或读取配置失败时，仍把已知豆瓣小图路径派生为 `/l/public/` 大图，但保留原始图片域名、文件后缀和查询串格式。
6. 发现页缓存继续保存配置无关的豆瓣大图 URL；每次响应及图片预热前按当前配置投影，配置修改后不需要等待缓存过期或主动清缓存。
7. 新豆瓣海报下载与“豆瓣图片本地化修复”共用同一域名/WebP 规则；已有本地图片不会在保存配置时自动批量处理，历史升级仍由管理员手动启动修复任务。
8. URL 转换必须幂等；已经是 `/l/`、配置域名或 `.webp` 的 URL 再处理不得继续变化。

## Acceptance Criteria

- [ ] 详情快照只有 `pic.normal`、`img`、`cover_url` 等小图/兼容字段时返回无图；无效 `cover.image.large.url` 不阻止有效 `pic.large`。
- [ ] 搜索和发现样本中的 `s_ratio_poster/*.jpg` 返回为 `/l/public/*.jpg`；配置图片域名后返回该域名下的 `/l/public/*.webp`。
- [ ] 配置图片域名后，带 `imageView2/.../format/jpg` 或 `/format/jpeg` 的 URL 同时变为 `.webp` 路径和 `/format/webp` 查询。
- [ ] 发现页已有缓存时修改图片域名，下一次响应和预热立即使用新域名，无需重新请求豆瓣或清除缓存。
- [ ] enrichment 新下载和历史修复下载收到与展示一致的配置域名、大图路径及 WebP URL；实际落盘仍按响应真实 MIME 处理。
- [ ] 未配置图片域名时不强制 WebP，但搜索、发现和可识别详情路径仍只使用 `/l/` 大图。
- [ ] 非标准搜索/发现图片路径返回无图；详情 large 字段的有效非标准 URL 保留，配置域名时无扩展名路径不追加 `.webp`。
- [ ] 非豆瓣图片、豆瓣 JSON API、Cookie、保存配置行为及现有本地图片不受影响。
- [ ] 聚焦 service/handler 测试、`go vet` 和 `git diff --check` 通过。

## Out of Scope

- 保存图片域名配置后自动扫描或重下全部历史图片。
- 修改豆瓣 Cookie、JSON API 请求域名、前端图片代理协议或其他 provider 的图片规则。
- 为未观察到的任意 CDN 查询语法实现通用图片格式转换器。
- 验证 `db-pic1.acecandy.cn` 在当前代理 Fake-IP 环境中的公网可达性；以 URL 转换测试和下载响应的真实图片校验为准。

## Technical Notes

- URL 解析和改写使用 Go 标准库 `net/url`、`path.Ext`、`strings`，不新增依赖。
- 前端现有 `imageURL()` 会把远端 URL 交给 `/api/img`；后端返回正确 `poster_url` 后无需修改前端。
- 现场数据库审计只用于确认 URL 形态；实施和测试不得写生产数据。
