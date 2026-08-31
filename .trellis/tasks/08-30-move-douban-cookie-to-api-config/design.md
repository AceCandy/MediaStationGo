# 技术设计：豆瓣 Cookie 数据库唯一来源

## Data Flow

```text
管理员输入 Cookie
  → PUT /admin/api-configs/douban { api_key }
  → APIConfigService.Update 加密
  → api_configs.api_key
  → DoubanProvider 每次请求 Resolve(ctx, "douban") 解密
  → 配置启用且值非空时设置 HTTP Cookie 头
```

清除沿用现有 DELETE/`<clear>` 语义，将 `api_key` 置空。公共 API 继续只返回 `has_key` 和 `masked_key`，不暴露明文。

单媒体补齐：

```text
管理员点击详情页“补齐豆瓣信息”
  → POST /api/media/:id/douban-enrichment
  → 按现有详情解析规则得到 canonical metadata_id
  → ScraperService 复用电影补齐逻辑
  → DoubanProvider 只请求移动详情接口
  → 成功后刷新页面；可恢复上游错误返回 429
```

批量补齐仍按 metadata ID 游标一次读取最多 20 个不完整候选。若当前条目收到需要稍后重试的上游错误，本次执行立即结束，保留已完成条目的游标，当前条目不推进；因此下次定时或手动执行会先重新请求同一条，而不是跳过它。

## Backend Changes

1. `DoubanProvider` 删除 `*config.Config` 依赖，改为持有现有 `*APIConfigService`。
2. `NewDoubanProvider` 从服务构建器接收已创建的 `APIConfigService`；不保留旧构造器或旧配置回退。
3. `setHeaders` 增加 `context.Context`，在搜索、详情 fallback 的每次尝试以及发现请求中动态调用 `Resolve`。
4. 仅当 `resolved.Enabled && strings.TrimSpace(resolved.APIKey) != ""` 时设置 Cookie。
5. `Resolve` 出错时仅记录 provider/错误，不记录明文 Cookie，并继续发送匿名请求。
6. 删除 `SecretsConfig.DoubanCookie` 和 `.env.example` 中旧环境变量示例。
7. 统一两个映射 `api_configs` 的历史模型，将 `api_key` 定义为 `text`，并用幂等兼容迁移扩宽已有 `varchar(512)` 列。
8. 为补齐流程增加独立的移动详情入口；现有普通详情/剧集数入口继续允许 `subject_abstract` 降级。
9. 用类型化错误区分补齐移动接口的可恢复上游错误和明确不存在。网络/超时、HTTP 403/429/5xx、空响应、非法 JSON、以及 HTTP 200 错误对象属于可恢复错误；HTTP 404 属于明确不存在。豆瓣明确的 `subject_ip_rate_limit` 必须按结构化响应识别，不得依赖最终展示错误文本匹配。
10. 暴露最小的单媒体补齐服务方法，内部继续复用现有唯一豆瓣电影 ID、只填空字段、海报候选和快照持久化逻辑；该方法不读取或修改全局游标。
11. 新增管理员路由 `POST /api/media/:id/douban-enrichment`。`:id` 先沿用现有媒体详情解析规则解析为 canonical metadata ID；不存在返回 404，非电影或无唯一豆瓣电影 ID 返回 400，限流返回 429，成功返回最小状态对象。
12. 批量循环在可恢复错误条目写游标之前停批；暂停执行不触发尾部游标清空。明确永久错误继续保持现有逐条隔离。
13. 保留 `snapshot_saved` 等既有指标，并新增明确分类，区分字段/海报变化、仅刷新完整快照、上游没有当前缺失数据、永久失败以及接口异常暂停。暂停日志明确说明未使用摘要降级。

## Frontend Changes

复用 `APIConfigsPanel` 的既有编辑行，不新增组件或 API 类型：

- Douban 的凭据标签显示 `Cookie`，无值占位文案显示“输入 Cookie”。
- 已配置时继续显示通用遮蔽占位文案，输入框保持 `type="password"`。
- 清除按钮的标题、确认标题和确认消息对 Douban 使用“Cookie”，其他 provider 保持“API Key”。
- 保存仍提交 `patch.api_key`，避免扩展跨层契约。
- 单媒体详情的现有管理员操作菜单增加“补齐豆瓣信息”；仅电影且存在 `douban_id` 时显示。
- 点击后使用 pending guard 防止重复提交；成功刷新当前详情，429 显示“豆瓣请求受限，请稍后重试”，其他错误沿用现有错误提示模式。
- 不新增权限类型，前后端继续使用详情页现有管理员边界。

## Error and Security Behavior

- 配置不存在、禁用或为空：匿名请求。
- 数据库读取失败：warning 后匿名请求，不让可选 Cookie 阻断豆瓣调用。
- 普通刮削或剧集数请求被豆瓣拒绝：沿用当前状态码/摘要 fallback 行为；补齐专用请求按下述错误分类处理且不降级。
- 补齐专用移动请求发生网络/超时、HTTP 403/429/5xx、空响应、非法 JSON 或 HTTP 200 错误对象：不降级，返回可判定的可恢复上游错误。
- 批量可恢复错误：当前执行以“因豆瓣接口异常暂停”完成记录，不标记为业务失败；当前条目及后续条目不请求、不推进游标，任务计划保持启用，下次执行先重试同一条。
- 明确 HTTP 404/条目不存在或本地永久性歧义：记录失败/跳过并推进当前游标，继续后续候选，避免永久阻塞。
- 单媒体可恢复错误：HTTP 429，页面提示“豆瓣请求受限或暂时不可用，请稍后重试”；不写快照或游标。
- 移动详情成功但没有当前缺失字段或新海报：仍可刷新完整快照，日志明确说明没有字段/海报变化，不再使用笼统“本次无变更”。
- 明文只存在于管理员提交、服务端解密结果和出站请求头中；不得进入日志或公共响应。

## Compatibility and Rollback

- 按用户选择，不兼容旧 `secrets.douban_cookie`；不自动迁移旧值。
- 普通刮削和剧集数获取的摘要降级保持兼容；只有批量与单媒体补齐强制移动详情。
- 不新增表或列；启动迁移将 `api_configs.api_key` 从 `varchar(512)` 无损扩宽为 `text`，已有密文保留。
- 回滚到旧版本时，数据库中的 Douban Cookie 会保留但旧版本不会读取；需要重新配置旧入口才能恢复携带。

## Trade-offs

- 选择每次请求查询数据库，换取保存后立即生效和最少状态同步代码。
- 复用 `api_key` 列，牺牲内部命名精确性，避免一次性用途的新字段、迁移和 API 契约扩张。
- 使用 PostgreSQL `text` 而非猜测 Cookie 最大长度，避免加密膨胀再次触发列宽限制。
- 单媒体操作复用现有补齐函数而不是另建持久化流程，避免字段优先级、海报和快照语义分叉。
- 可恢复上游错误只停止当前执行而不禁用计划，既避免继续消耗请求，也保证下次从同一条自然重试；明确永久错误继续推进，避免游标死锁。
