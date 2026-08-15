# Emby Items 查询批量化设计

## Boundary

只优化 `EmbyService.payloadsForViews` 构造物理媒体列表项时的关联数据读取。路由、请求参数、分页选择、缓存、JSON 字段和详情探测路径保持不变。

## Current Data Flow

1. `metadataPage` 选择一页逻辑 metadata，并批量得到代表 `MediaView`。
2. `payloadsForViews` 批量读取收藏和播放历史。
3. 条目循环调用 `itemPayload`。
4. `itemPayload` 为每条媒体分别读取 credits/people、identifiers、媒体版本；剧集类型判断最多重复读取三次 Library。

第 4 步使查询量随页大小线性增长。

## Proposed Data Flow

1. 保留现有分页、收藏和历史查询。
2. 在进入条目循环前收集本页唯一 metadata ID 和 library ID。
3. 通过 repository 批量读取：
   - metadata credits 及关联 Person；
   - metadata identifiers；
   - 所有可见 sibling `MediaView`。
4. 一次读取剧集类型 Library ID 集合，并结合现有路径启发式为每条媒体计算 Episode 判定。
5. 将以上结果按 metadata/media ID 分组为仅本次请求使用的关系映射；条目循环只做内存查找和现有 payload 映射。

## Repository Contracts

- 为 PersonRepository 增加按 metadata ID 集合批量读取 credits+people 的方法；现有单 ID 方法复用批量实现。
- 为 MetadataRepository 增加按 metadata ID 集合批量读取 identifiers 的方法；现有单 ID 方法复用批量实现。
- 为 MediaViewRepository 增加按 metadata ID 集合批量读取可见版本的方法，继续应用 `MediaQueryFilter`。
- 空 ID 集合直接返回空 slice；批量结果保持确定性排序，服务层按 metadata ID 分组。

不新增通用 loader、缓存或接口层。

## Service Mapping

- 增加一个仅供列表批量构造使用的小型关系结构，保存 People、ProviderIds、siblings 和 Episode 判定。
- 保留现有 `itemPayload` 作为详情/其他调用方入口；它继续按原路径即时加载。
- 列表调用内部的 relations 版本，复用相同的字段映射函数。
- 将 People 排序、Provider 映射、MediaSource sibling 去重/排序提取为当前单条与批量路径共同调用的纯映射函数，避免两套语义漂移。
- 批量读取失败按字段降级，不改变现有列表整体成功语义。

## Compatibility

- 默认列表继续返回 `People`、`ProviderIds`、`MediaSources`。
- `MediaSources` 只包含用户可见版本，当前代表版本排在首位，并继续使用 `completeStreams=false`。
- 单条 `Item` 详情保持 `completeStreams=true`，不受批量列表路径影响。
- 外部请求/响应没有变化，因此无需同步 Emby API catalog。

## Risks and Controls

- 排序或分组变化：单条与批量路径复用同一映射/排序函数，并用现有多版本测试保护。
- 隐藏库版本泄漏：批量 MediaView 查询必须接收现有 `MediaQueryFilter`。
- 批量任一查询失败导致整个列表失败：保持当前 best-effort 降级，不向 handler 返回新增错误。
- 测试只验证结果却漏掉性能回退：加入基于 GORM 查询回调的最小查询计数回归。

## Rollback

改动不涉及 schema 或数据迁移。回滚相关 repository 批量方法和列表 relations 调用即可恢复原路径。
