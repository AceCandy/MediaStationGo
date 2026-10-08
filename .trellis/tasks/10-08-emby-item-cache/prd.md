# ITEM 接口缓存需求

## 目标

降低 Emby ITEM 列表和详情的重复查询与组装成本，保持协议、用户隔离、权限及更新行为。已授权创建任务和详细设计；设计及失效审计已交付，按用户要求提交归档；业务代码未实现，以下功能验收仍属后续工作。用户已确认单实例，并授权缓存选型。

## 背景与确认事实

- 普通 mediaItems 已缓存，其他分发未统一覆盖；其边界在前瞻裁切和收藏归属补充之前。出处：internal/service/emby_items_list.go:13、internal/service/emby_compat.go:128。
- RuntimeCache 已有最多 2048 条的内存层和可选 Redis，媒体 TTL 默认 15 秒。出处：internal/service/runtime_cache.go:31、internal/config/defaults.go:43。
- ITEM 前缀是 media:emby:，现有 DeletePrefix("media:") 已覆盖它。扫描、刮削、删除和轨道探测的通用失效可复用。出处：internal/service/emby_items_cache.go:11、internal/service/media_cache.go:44、internal/service/media_probe.go:164。
- token 在 HTTP 层最后注入；详情触发后台轨道探测。出处：internal/handler/emby_items_handlers.go:99、internal/handler/emby_playback.go:114、internal/service/emby_playback.go:111。
- 默认播放配置、成人设置、用户 hide_adult 和库基础信息写入已有仓储读版本换代。出处：internal/repository/read_cache.go:26、internal/repository/play_profile_repo.go:18、internal/repository/setting_repository.go:30、internal/repository/user_repository.go:113、internal/repository/library_repository.go:122。
- Web 进度/收藏走未注入 Cache 的 PlaybackService；NFO 编辑及红果/黄果刷新需要补充失效。出处：internal/handler/playback.go:31、internal/service/service_builder.go:127、internal/service/playback.go:359、internal/service/nfo_metadata.go:54、internal/service/hongguo.go:324、internal/service/huangguoai.go:220。

## 范围

仅新增 Emby API 的通用 Items 列表、单条目浏览详情、Resume 继续观看及 Items 的 IsResumable 查询缓存，统一 5 分钟，覆盖普通、NFO、红果、黄果及用户/大小写/emby 路径别名。经 Items 查询的季/集列表自然受益，保持专用入口的计数和参数规则。

通过 Items 返回的根目录、人物和搜索属于本次范围；独立 Latest、Views、Persons、SearchHints、Counts 不新增缓存。Latest 可在后续明确扩展时复用基础能力。

Web 接口、前端和浏览器缓存本次不修改。NextUp 固定兼容响应、随机排序、PlaybackInfo、精确 PlaybackProgress、重定向、图片及字幕内容不新增缓存。不改 SQL、排序、来源身份、字段、HTTP 缓存头、数据库结构；不新增 Redis 部署、缓存 UI、预热或后台刷新。

## 后续实现要求与验收（当前未执行）

| ID | 要求 | 可观察验收 |
| --- | --- | --- |
| R1 | 重复读取复用响应 | 同一用户和查询 TTL 内第二次读取无候选/状态/展示 SQL；认证和前置可见性查询单独统计 |
| R2 | 协议不变 | 冷热 JSON 等价；IDs 顺序/重复、准确计数/下界、500/501、收藏 LibraryIds 和字段缺省不变 |
| R3 | 用户与访问范围隔离 | 不同用户不共用响应；默认播放配置/成人/可见库变更成功后的新请求不命中旧权限；认证和跨用户拒绝始终执行。Web 功能权限表不参与可见范围缓存键 |
| R4 | 写后失效 | 公共媒体/资料/轨道变更失效全部用户；收藏/进度/历史只失效当前用户且包含容器和筛选列表；Emby 写入和共享服务提交均覆盖，部分提交也通知。Web handler 的直接写本次不改，见一致性边界 |
| R5 | 并发旧读不能污染 | 屏障复现旧查询→写成功换代→旧查询完成；后续请求不能命中旧结果 |
| R6 | 安全与对象独立 | 两 token 使用各自 URL；缓存无请求 token/session/真实上游目标；修改响应不影响下一响应；大整数不丢精度 |
| R7 | 动态入口规则 | Random 跳过缓存；Resume/IsResumable 缓存 300 秒且进度/已看/历史/内容/权限变更失效；精确进度和播放协商实时 |
| R8 | 故障回退 | nil 缓存、损坏 JSON、Redis 故障回退读取；错误/取消/不存在详情不缓存；合法空列表可缓存 |
| R10 | 缓存持续有收益 | A 用户播放进度不清 B 用户缓存；被忽略的进度、零行历史删除及失败/回滚不换代；未知幂等写保守清当前用户 |
| R9 | 有限资源和回滚 | 保留现有容量，Emby 新响应统一 300 秒，其他缓存 TTL 不变，超过 1 MiB 的新增 ITEM 快照不缓存；无数据迁移，可退回实时读取 |

## 已确认选择与一致性边界

用户已确认一个后端服务实例。选择本地 L1 + 可选 Redis L2，沿用当前配置，有可用 Redis 时使用两层，无 Redis 时本地仍有效。范围失效、版本保护和 TTL 同时保留，不承诺永久命中。

若实际有多个后端实例共享数据库且要求任一实例写后全实例立即可见，必须先调整为共享版本与每次读取校验，不能直接实施本提案。

直接 SQL、其他进程及外部文件修改在本进程发现前不承诺立即刷新；TTL 兜底不能代替已知写路径失效。

Web 范围明确排除前端、浏览器缓存、Web HTTP handler 和接口行为；共享业务服务中面向 Emby 的必要失效通知仍可补齐。Web watch_history.go 的直接数据库删除本轮不修改，因此这些路径可能使 Emby 缓存保留旧状态至 TTL 到期。实施前若要求跨 Web 所有写入口也立即同步，需另确认这两处最小失效通知；不宣称当前收敛范围已保证所有 Web 写后即时可见。
