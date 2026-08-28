# 技术设计

## 边界

本任务修改豆瓣按 ID 详情与集数获取、历史豆瓣补齐候选、单媒体详情 provider 状态和详情页展示。不修改 schema、provider ID 绑定、当前海报覆盖规则、豆瓣关键词搜索、分页发现或其他 provider 任务。

## 豆瓣详情来源

`DoubanProvider.GetMatchByID` 首先请求：

`https://m.douban.com/rexxar/api/v2/movie/{doubanID}`

请求沿用现有外部 HTTP client、随机 User-Agent 和可选豆瓣 Cookie，并设置移动端 subject Referer。成功响应交给现有 `doubanMatchFromRawJSON`；其通用 map 解析已经覆盖 `intro`、`cover_url`、`pic.large` 和 `rating.value`。完整移动端响应作为 `Match.RawJSON` 保存。

主接口 HTTP、读取或 JSON 解析失败时，再请求现有 `subject_abstract`。降级成功仍可 fill-only 并保存快照，但其 wrapper 结构被识别为 legacy/partial；24 小时后允许再次尝试移动端接口。两者均失败则不保存快照、不推进冷却。

正常自动刮削、手动按 ID 搜索/应用、持久化补抓和历史补齐最终都共用 `GetMatchByID`，因此只切换这一共享入口即可覆盖详情链路。`subject_suggest` 仍负责把关键词解析为豆瓣 ID，`search_subjects` 仍负责分页发现，两者不能由按 ID 详情接口替代。

`GetEpisodeCountByID` 改为复用移动端详情响应中的 `episodes_count`，避免继续单独依赖 abstract。实测移动端 Movie 路径对 TV subject 同样返回 `type: "tv"` 与 `episodes_count`；`GetEpisodeCount(query)` 的前置关键词搜索保持不变。

## 旧快照刷新

候选查询保留唯一豆瓣 Movie ID、metadata-ID keyset、batch 20、串行节流和 24 小时截止时间。候选包括：

1. 无豆瓣快照。
2. 快照超过 24 小时且 payload 是旧/降级 wrapper 格式。
3. 当前移动端快照超过 24 小时且自身缺 `intro` / 图片字段、缺豆瓣本地图片，或 canonical 仍缺简介、中文标题。

移动端快照成功保存后替换旧 payload；图片成为豆瓣本地 candidate，仅在 selection 缺失时提升。已有 selection 保持不变。

## Provider 缓存状态

`MediaView` 保留现有 `tmdb_snapshot` / `douban_snapshot` 兼容字段，并新增：

- `tmdb_status`
- `douban_status`

值域为 `missing | partial | complete`。仅在 canonical provider ID 存在时计算：

- `missing`：无 provider snapshot。
- `partial`：有 snapshot，但没有带有效 asset 的该 provider 本地图片；豆瓣旧/降级快照也始终为 partial。
- `complete`：当前格式 snapshot 且存在该 provider 的有效 selection 或 candidate 图片。

状态只描述本地详情与图片缓存覆盖度，不保证评分或远端全部可选字段存在。详情服务复用 snapshot repository，并增加一个 provider artwork existence 查询；列表不加载该状态。

## Web 展示

评分徽章始终渲染，值为正时一位小数，否则为 `-`。

provider 外链隐藏 ID，使用紧凑圆形 monogram（豆瓣“豆”、TMDb“TM”）与 Lucide 状态图标：灰色空心、琥珀色警示、绿色勾选。每个链接包含 provider 与状态的 `aria-label`/`title`；颜色不是唯一信息。URL 仍使用 canonical ID，Season/Episode 继续用 Series TMDb ID 生成深链。

## 兼容与回滚

- 旧 snapshot 布尔字段保留，旧客户端不受影响。
- 新状态字段为可选 JSON 字段，不改数据库。
- 回滚 provider endpoint 不删除已保存的移动端快照或本地候选图片。
- 不自动重启当前用户运行中的服务。

## 风险

- `rexxar` 是非公开接口，可能限流或调整结构；保留 abstract 降级和现有 24 小时冷却。
- 旧快照数量较多，按现有 batch/节流逐步刷新，不在一次执行中突发请求。
