# Emby Items 查询批量化设计

## Boundary

优化 `/Items` 中 Movie/Episode/Series/Season 列表项的关联数据读取，并支持 `Fields` 对高成本可选关系的裁剪。路由、鉴权、分页选择和详情探测路径保持不变。

## Current Data Flow

1. `metadataPage` 选择一页逻辑 metadata，并批量得到代表 `MediaView`。
2. `payloadsForViews` 批量读取收藏和播放历史。
3. 条目循环调用 `itemPayload` 或 `seriesPayload` / `seasonPayload`。
4. 每条媒体分别读取 credits/people、identifiers、媒体版本；Series/Season 还分别读取 metadata、artwork、favorite 和 history。

第 4 步使查询量随页大小线性增长。

## Proposed Data Flow

1. 保留现有分页、收藏和历史查询。
2. 在进入条目循环前收集本页唯一 metadata ID 和 library ID，并解析请求的 `Fields`。
3. 通过 repository 批量读取 metadata、artwork、credits 及关联 Person、identifiers、用户状态和所有可见 sibling `MediaView`；未请求的可选关系不读取。
4. 一次读取剧集类型 Library ID 集合，并结合现有路径启发式为每条媒体计算 Episode 判定。
5. 将结果按 metadata/media ID 分组为仅本次请求使用的关系映射；条目循环只做内存查找和现有 payload 映射。

## Repository Contracts

- 为 PersonRepository 增加按 metadata ID 集合批量读取 credits+people 的方法；现有单 ID 方法复用批量实现。
- 为 MetadataRepository 增加按 metadata ID 集合批量读取 identifiers 的方法；现有单 ID 方法复用批量实现。
- 为 MetadataRepository 和 ArtworkRepository 增加按 metadata ID 集合批量读取元数据与当前图片选择的方法。
- 为 MediaViewRepository 增加按 metadata ID 集合批量读取可见版本的方法，继续应用 `MediaQueryFilter`。
- 空 ID 集合直接返回空 slice；批量结果保持确定性排序，服务层按 metadata ID 分组。

不新增通用 loader、缓存或接口层。

## Service Mapping

- 增加仅供列表批量构造使用的关系结构，分别保存物理媒体关系与 Series/Season 卡片关系。
- 保留现有 `itemPayload` 作为详情/其他调用方入口；它继续按原路径即时加载。
- 列表调用内部的 relations 版本，复用相同的字段映射函数。
- 将 People 排序、Provider 映射、MediaSource sibling 去重/排序提取为当前单条与批量路径共同调用的纯映射函数，避免两套语义漂移。
- 批量读取失败按字段降级，不改变现有列表整体成功语义。
- `Fields` 加入 Items 缓存键；省略时启用全部兼容字段，显式指定时只装载并输出对应可选字段。

## Compatibility

- 默认列表继续返回 `People`、`ProviderIds`、`MediaSources`。
- 显式 `Fields` 只裁剪 `People`、`ProviderIds`、`MediaSources`；标题、图片、类型和 UserData 等卡片基础字段始终保留。
- `MediaSources` 只包含用户可见版本，当前代表版本排在首位，并继续使用 `completeStreams=false`。
- 单条 `Item` 详情保持 `completeStreams=true`，不受批量列表路径影响。
- Emby API catalog 同步记录 `Fields` 参数及默认兼容行为。

## Player Request Diagnostics

- Emby 双前缀路由通过路由组中间件标记，现有全局 `http` 日志仅对这些请求追加 `player_api=true`、`headers` 和 `query`。
- Header 与 query 保留多值结构；认证、Cookie、Token、设备标识及其他凭据字段统一脱敏，请求体不进入日志。
- 日志不改变路由、鉴权或响应，不新增独立日志流。

## Risks and Controls

- 排序或分组变化：单条与批量路径复用同一映射/排序函数，并用现有多版本测试保护。
- 隐藏库版本泄漏：批量 MediaView 查询必须接收现有 `MediaQueryFilter`。
- 批量任一查询失败导致整个列表失败：保持当前 best-effort 降级，不向 handler 返回新增错误。
- 测试只验证结果却漏掉性能回退：加入基于 GORM 查询回调的最小查询计数回归。
- 字段裁剪破坏旧客户端：仅在 `Fields` 显式存在时裁剪，省略时完全保留当前形状。

## Rollback

改动不涉及 schema 或数据迁移。回滚相关 repository 批量方法和列表 relations 调用即可恢复原路径。
