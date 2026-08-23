# 媒体入库刮削策略与失败处置：技术设计

## 1. 边界与原则

- 媒体扫描负责发现文件、写入 `Media`、绑定已存在的精确 canonical metadata；不制造本地 canonical metadata。
- metadata 业务状态仍由 `media.scrape_status`/`scrape_error` 持有，任务执行与日志只负责可观测性。
- 继续使用现有三个媒体 worker、一个串行 catalog worker、`ResetLibraryScrape`、单媒体重新入队和 `ManualScrapeDialog`。
- 不增加刮削队列、失败表、任务来源字段或新的 metadata kind。

## 2. 库类型与刮削策略

新增内部值：

| Library.Type | Web 标签 | 处理形态 | metadata 来源 |
| --- | --- | --- | --- |
| `nfo_movie` | 非常规电影 | Movie | 本地 NFO-only |
| `nfo_tv` | 非常规剧集 | Series/Season/Episode | 本地 NFO-only |

- 两个内部值都不超过 `libraries.type varchar(16)`，因此不扩大字段或增加 schema 迁移。
- 后端建库服务必须显式识别并保留用户选择的 `nfo_movie/nfo_tv`，不能被名称/路径启发式重新推断为普通类型；Web 下拉框不是类型语义的唯一校验点。
- 将 Go 中的系列/NFO/可刮削判断收敛到已有或最小共享函数，并替换所有实际读取 `Library.Type` 的 scanner、scraper、manual provider、organizer 和 Emby 分支；Web 两处系列判断复用一个 owner。不要顺带改与 Library 无关的通用 media type 分类器。
- 普通 `movie/tv/anime/variety` 保留现有 provider 链与本地 fallback 行为。
- 非常规类型在 scraper 入口走专用短分支：读取现有 `ReadLocalMetadata` 结果，有有效 NFO 时走现有 `applyLocalMetadataMatch`；缺失 NFO 写 `no_match`；解析失败写 `error`；不进入 provider 查询或网络手动匹配。
- scanner 对非常规类型只使用 NFO 及其边车内容，不叠加文件/目录 path hint；缺失 NFO 不能因路径中的 provider ID 直接绑定 canonical metadata。
- 手动 provider search/apply 在后端也拒绝 `nfo_movie/nfo_tv`，不能只依赖 Web 隐藏按钮。
- 全部媒体库手动模式允许 `movie/tv/anime/variety/show/shows/nfo_movie/nfo_tv`；明确跳过 `music`、`adult` 和未知类型，成人网络刮削仍只允许既有显式入口。

## 3. 默认自动处理

- 删除 Web 的“扫描后自动刮削”控件、配置字段和提交键。
- 全量/单 root 扫描在产生变化且调用方允许 auto-scrape 时直接唤醒 worker，不再读取 `scrape.auto_on_scan`，也不以常规 provider 是否启用为前提。
- watcher 成功新增/更新媒体后直接唤醒 worker。
- 保留 organizer 自己的 `organize.scrape_after` 语义和防重复调用参数；移除它对已废弃 `scrape.auto_on_scan` 的兼容回退。
- 按数据库规范在 `AutoMigrate` 精确删除退休键 `scrape.auto_on_scan`；不做前缀删除。

## 4. 手动执行

任务定义 `media_scrape` 增加服务端 action。请求接受二选一目标：

```json
{ "library_id": "<uuid>" }
```

或：

```json
{ "all_libraries": true }
```

- handler 校验 JSON、目标互斥、单库存在且类型可刮削。
- 单库扩展现有 `ResetLibraryScrape(libraryID, false)`：空状态/`pending/error/no_match` 都标记为 manual trigger，其中 `error/no_match` 同时清错并重新置为 `pending`；随后唤醒 worker。
- 全部模式读取媒体库列表，逐个处理可刮削类型并汇总 queued 数；不是第二套全局任务执行器。
- 已是 `matched` 的媒体保持不变，尚未扫描入库的磁盘文件不在该操作范围内。

## 5. 统一来源日志

面向用户只保留一个“媒体入库刮削”历史/日志入口。每个成功明细显示以下来源之一：

- `已有元数据`：精确 canonical metadata 命中，未发起 provider 请求。
- `网络刮削（provider）`：使用 `Match.Source`，例如 TMDB、豆瓣、Bangumi、TheTVDB。
- `本地 NFO`：通过现有本地 metadata 持久化链路成功。

实现边界：

- worker 执行期间用一个仅内存的结果来源值传给 `mediaScrapeTaskDetail`，写入 `TaskUpdate.Details`；不增加 `TaskExecution`/数据库字段。
- scanner 直接绑定已有 canonical 的路径仍遵守 shared metadata contract；当待写媒体可精确命中且旧状态不是 matched 时，在执行绑定前创建 `TaskKindScrape`，随后按 Upsert 成败结束该执行，来源为“已有元数据”。重复扫描一个已 matched 或未变化的媒体不重复记录。
- provider/NFO/快速 canonical 命中都写入同一个稳定定义 `media_scrape` 的每日任务日志。
- 继续使用现有错误清洗函数，日志不包含 URL、query、密钥或未清洗错误。

## 6. Web 待处理区

普通 `MediaView` 明确排除未绑定 metadata 的媒体，因此新增只读 remediation endpoint，而不是放宽共享 inner join：

```text
GET /api/media/scrape-issues?library_id=&status=error,no_match&page=&page_size=
```

返回专用 DTO：`id`、扫描标题、年份、库 ID/名称/类型、`scrape_status`、安全的失败原因，以及分页字段。status 只允许 `error`、`no_match`；空错误按状态和库策略生成可操作文案，例如“未找到匹配元数据”或“未找到本地 NFO”。

任务中心“媒体入库刮削”区域复用该 endpoint，提供：

- 媒体库与状态筛选。
- `error`：重新处理。
- 普通库 `no_match`：重新处理或打开现有 `ManualScrapeDialog`。
- 非常规库 `no_match/error`：提示补充/修复 NFO 后重新处理，不提供网络手动匹配。

列表只解决失败可见性和处置，不新增独立媒体详情页或 NFO 编辑器。

## 7. 兼容、回滚与风险

- 保留已有 API、状态值、worker 并发和 canonical 层级；新 action/endpoint 为增量接口。
- 回滚 Web 与 handler 不影响持久业务状态；已退休设置键删除后回滚版本会恢复为其既有默认关闭语义，不会丢失媒体数据。
- 最大风险是库类型判断遗漏导致 `nfo_tv` 在某条链路被当作电影；实施时必须用全局 `rg` 复核所有直接类型分支。
- scanner 直接 canonical 命中补 task 时必须比较前后状态，避免每次重扫制造重复日志。
- remediation 查询必须从 raw media 构造最小 DTO并应用管理员权限，不得把未匹配扫描提示扩散到普通 `MediaView` 列表。
