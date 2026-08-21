# 播放统计明细与热榜设计

## Boundaries

- 保留 `GET /api/admin/playback-stats` 和现有管理员鉴权，不新增路由或权限。
- 扩展现有统计响应，统一返回摘要、时间桶、当前明细页和所选周期热榜。
- 数据只来自 `playback_events`；`playback_histories` 不参与热榜，避免把手动状态或最后进度误算为真实播放。
- 不修改播放事件模型、写入流程或数据库结构。

## Request Contract

在现有 `grain/from/to/user_id/media_type/library_ids` 上增加：

- `page`：明细页码，默认 1。
- `page_size`：前端固定传 20，后端允许 1..100。
- `rank_grain`：`day|week`。
- `rank_date`：`YYYY-MM-DD`，表示所选日或所选周内日期；缺省时使用筛选结束日期。

所有参数仍由现有 handler 集中校验。`page < 1`、`page_size` 超出 1..100、页偏移计算溢出、非法周期及 `rank_date` 超出 `from..to` 均返回 `400`；超过最后一页返回空 `items` 和不变的 `total`。handler 先解析唯一有效统计时区：使用具名 `time.Local`，无法取得具名时区时统一回退 UTC；`from/to/rank_date` 解析、Go 周期边界及 SQL `AT TIME ZONE` 全部使用该位置。日榜使用所选日期零点至次日零点；周榜使用包含该日期的周一零点至下一周周一零点。周期窗口必须再与页面 `from/to` 半开区间求交，因此首尾不完整周期不会计入范围外事件。边界使用 `time.ParseInLocation`/`AddDate`，不用固定 24 小时规避 DST 偏差。

## Response Contract

保留兼容字段 `total` 与 `buckets`，新增：

- `details`：`items/page/page_size/total`。
- 明细项：事件 ID、播放时间、账户 ID/名称、媒体库 ID/名称、快照媒体 ID、元数据 ID、标题、剧名、季号、集号、海报和 `available`。季/集号为可空整数；标题缺失显示“媒体已不可用”，其他展示字段允许为空；`available=false` 时仍返回快照 ID 但不提供详情链接。
- `ranking`：`grain/period/items`。
- 热榜项：名次所需稳定顺序、聚合身份、标题、剧名、季号、海报和播放次数。

旧前端只读取 `total/buckets`，因此响应扩展保持兼容。

## Query Design

- 建立一次公共筛选查询构造，供总数、时间桶、明细和热榜复用，避免四处漂移筛选规则。
- 明细在 PostgreSQL 中按 `played_at DESC, id DESC` 稳定排序并 `LIMIT/OFFSET`。
- 用户、媒体库、当前媒体和元数据使用 `LEFT JOIN`，确保被删除的当前媒体文件不会让审计事件消失；项目删除合同会保留共享元数据，因此媒体类型和标题仍可解析。异常缺失元数据的孤儿事件在无媒体类型筛选时保留为不可用项，在 `movie|tv` 筛选下无法可靠归类并予以排除。
- 电影热榜以电影 `metadata_id` 聚合。
- 电视剧事件若指向单集，则由 `episode.parent_id = season.id` 聚合到季，再由 `season.parent_id = series.id` 取得剧名；若事件已经指向季则使用自身，异常缺少父季时以原元数据 ID 作为稳定退化分组。季标题展示剧名与季号。
- 热榜按 `count DESC`，再按稳定聚合 ID 排序，限制 10 条。
- 不把事件全量读入 Go，也不建立缓存或预聚合。

## Frontend Design

- 保留现有标题、筛选、总数和时间序列区域。
- 新增热榜面板：每日/每周分段切换、原生日期输入、Top 10 海报/横条组合；复用主题变量和现有徽标/面板样式。
- 新增明细面板：桌面表格、移动卡片列表、上一页/下一页；筛选变化时页码归 1。
- 热榜日期默认等于当前筛选结束日期；切换日/周只刷新对应榜单请求参数。
- 作品链接仅在 `available=true` 且存在媒体 ID 时生成。

## Compatibility and Failure Behavior

- 无数据库迁移，回滚只需撤销 repository/handler/API type/page 改动。
- 非管理员仍在现有路由中间件处返回 `403`。
- 任一无效筛选、分页或热榜参数返回 `400`。
- 查询失败沿用 `500`；前端保留旧数据时显示本次刷新失败，不伪造空榜。
- 当前媒体文件删除时保留事件计数和元数据展示，`available=false` 且不生成链接；异常缺失共享元数据时按 Query Design 的孤儿事件规则处理。
