# 技术设计

## 1. 边界与所有权

本次保留两类互不替代的数据：

- `playback_histories`：每个账户、每个作品元数据一行，保存最新位置和完成状态。
- 新增播放事件表：每个达到 20 秒的独立播放会话、每个作品一行，只用于播放次数统计。

完成判定、输入校验、20 秒门槛和媒体可见性由播放服务统一拥有。Web 与 Emby handler 只负责解析各自协议、解析目标账户和传入会话 ID，不再自行决定完成状态。

## 2. 账户与权限

- 自助接口不接受目标账户时，只使用鉴权上下文账户，包括管理员。
- 兼容协议中显式出现 `userId` 时，统一解析规则：与当前账户相同则允许；不同则仅管理员允许；否则返回 403。
- 收藏、历史和 UserData 的显式目标账户入口共用同一解析规则，避免每个 handler 各写一份权限判断。
- 统计查询挂在现有 `/api/admin` 路由组，直接复用管理员中间件。
- `can_view_history` 保留在持久化和兼容响应中，但服务端规范化为 `true`，Web 权限表不再允许切换。

## 3. 进度规则

服务层只保留一个完成判定函数：

```text
duration < 600000ms  -> position >= max(duration - 30000ms, 0)
duration >= 600000ms -> position >= duration * 90%
```

自动上报必须满足 `position >= 20000ms` 才能写历史或事件。手动标记走独立服务方法：

- 已看：直接写完成状态，不走 20 秒门槛，不创建播放事件。
- 未看：按账户和 `metadata_id` 删除历史，不写零位置行，不删除播放事件。

入口拒绝负位置、非正时长和 `position > duration`。恰好 20,000ms 属于有效自动进度；读取侧百分比统一夹在 0 到 100 之间，防御既有异常数据。

## 4. 历史原子写入

`HistoryRepository.Upsert` 改用 PostgreSQL `INSERT ... ON CONFLICT (user_id, metadata_id) WHERE deleted_at IS NULL DO UPDATE`。更新具体版本、位置、时长、最后观看时间和完成状态；不再执行“先查后写”。现有部分唯一索引继续作为数据库事实来源。

## 5. 播放事件

新增追加型事件模型，最小字段如下：

```text
id, user_id, session_id, metadata_id, media_id, library_id, played_at
```

- 唯一约束：`(user_id, session_id, metadata_id)`，同会话重复上报使用 `ON CONFLICT DO NOTHING`。
- 除唯一约束外，只为已确认查询建立时间索引以及 `(user_id, played_at)`、`(library_id, played_at)` 组合索引；其他索引等真实查询计划证明需要时再加。
- `metadata_id` 是作品统计身份；`media_id` 和 `library_id` 保存播放发生时的具体版本与库，避免媒体后来移动或删除改变历史统计。
- 事件中的媒体和库 ID 是审计快照，不设置会阻止媒体/库删除的限制性外键；删除源实体不级联删除既有事件。
- Web 每次打开播放实例生成一个 UUID，并随周期、暂停、结束和离页上报复用。
- Emby 读取标准 `PlaySessionId`；PlaybackInfo 生成不可碰撞的会话 ID，Playing、Progress、Stopped 及其小写兼容路由都原样传递并执行相同目标账户校验。
- 没有有效会话 ID 的自动上报仍可更新历史，但不创建无法可靠去重的播放事件；兼容客户端行为不因统计失败而丢进度。
- 有有效会话 ID 时，历史 Upsert 和事件去重插入在同一数据库事务中完成；失败明确返回，客户端重试仍由两个数据库冲突策略保证幂等，不留下“进度成功但事件丢失”的部分状态。

## 6. 继续观看与批量加载

播放服务提供统一的继续观看查询，固定应用：当前账户、`completed = false`、`position_ms >= 20000`、最后观看时间倒序和媒体可见性。Web 首页和 Emby Resume 都消费这份结果。

查询先取得有界历史集合，再按 ID 集合批量加载可见 `MediaView`、同元数据版本和 UserData，最后按原历史顺序组装；不允许在结果循环中逐条查询媒体或版本。

## 7. Web 最终进度与缓存

- 周期上报保留现有节流。
- pause、ended 主动 flush 最新位置；正常请求失败时只做一次有界重试。
- pagehide/离页使用浏览器原生 keepalive 请求提交小体积 JSON，不引入依赖。
- 同一时刻相同账户的历史/继续观看请求复用进行中的 Promise；成功结果使用短 TTL 内存缓存。
- 缓存 key 必须包含账户 ID。进度成功上报、手动已看/未看、删除/清空历史和账户切换均使对应缓存失效；旧账户的在途响应不得写入新账户缓存。

## 8. 管理员统计 API

新增 `GET /api/admin/playback-stats`：

- 查询参数：`grain=day|week|month`、`from`、`to`、可选 `user_id`、可选 `media_type`、逗号分隔的可选 `library_ids`；重复库 ID 先去重。
- 日、周、月沿用项目现有的服务端本地时区语义聚合，响应 period 使用可排序的日期字符串；本次不另建时区配置。
- 返回总播放次数和时间桶：`total`、`buckets[{period,count}]`，空区间返回空桶而非错误。
- handler 拒绝非法粒度、日期、`from > to` 和不存在/无权引用的筛选值；聚合和过滤由 repository 用 PostgreSQL 完成，不把全部事件加载到内存。
- `user_id` 为空表示所有账户；多个库使用 OR/IN 语义；其他筛选之间使用 AND。

## 9. Web 统计页

- 新路由 `/playback-stats` 设置 `adminOnly: true`、`navigation.scope: 'viewer'`，排序位于“我的”和“播放器日志”之间。
- 页面提供粒度、日期范围、账户、媒体类型和媒体库筛选；媒体库支持一个或多个选择。
- 使用现有 API 客户端、管理员用户/媒体库列表和原生表单控件；结果以总次数和带数值标签的 CSS 条形时间序列展示，不新增图表包。
- 普通用户导航中不可见，直接访问由现有 `RequireAdmin` 拒绝。

## 10. 兼容、迁移与回滚

- 新事件表通过幂等 PostgreSQL migration/AutoMigrate 创建；不回填旧历史，因为最新位置不能推导真实播放次数。
- `playback_histories.completed` 保留；删除的是客户端进度请求中的 `completed` 字段。
- JSON 兼容解析继续忽略旧客户端携带的 `completed`，但服务方法和新前端请求类型不再暴露该参数。
- `can_view_history` 数据库列暂不删除，旧客户端仍接收 `true`。
- 若 Emby 进度请求参数或 PlaybackInfo 会话行为的对外说明发生变化，同步 `web/src/pages/embyApiCatalog.ts`。
- 回滚前端和服务代码不会影响现有历史表；新增事件表可保留为无消费者数据，避免破坏性回滚。

## 11. 关键取舍

- 不复用 `PlaybackHistory` 统计次数：它只有最新状态，会覆盖旧播放。
- 不使用内存 SessionTracker 去重：重启后会重复计数。
- 不增加消息队列、统计预聚合表或图表依赖：当前数据库唯一约束和按需聚合足以满足已确认范围，出现可测量性能瓶颈后再扩展。

## 12. 作品列表与播放版本边界

- 首页最近添加由 repository 直接按逻辑作品聚合：movie 使用自身 metadata，episode/season 归属 series metadata，以关联可见媒体的 `MAX(created_at)` 排序，归组后再 `LIMIT`。
- 首页删除未展示的媒体库请求；最近添加和继续观看仍是两个独立并行请求，避免增加只为减少请求数而存在的聚合接口。
- 收藏以 `favorites.metadata_id` 连接作品 metadata 与 artwork，批量返回作品卡片；兼容保留 `favorites.media_id`，但列表不读取它。
- 卡片与详情路由传递 `metadata_id`。详情一次加载作品、可见版本和该账户历史；历史中的 `media_id` 仍可见时作为默认版本，否则复用现有版本优选规则。播放路由只接收用户最终选定的 `media_id`。
- Emby/Jellyfin 列表、Latest 和 Resume 同样先按逻辑 metadata 分页，再批量加载当前页媒体和 UserData；单项详情和 PlaybackInfo 可接受 metadata，并只在需要播放信息时解析具体版本。
- 不为作品列表选择“代表播放版本”。海报来自作品 metadata/artwork；媒体版本属于详情和播放边界。
