# 优化播放器查询链并统一 Web 播放逻辑

## Goal

减少 Emby PlaybackInfo 与字幕交付链路中的重复数据库和文件系统工作，并让 Web 播放相关入口复用已有的服务端响应与返回路径逻辑；保持现有对外协议、权限、版本选择和兼容 ID 行为不变。

## Background

- `PlaybackInfoWithOptions` 当前先解析可播放媒体，再加载 sibling 版本；probe 文档会经 `ensureTrackMetadata`、选择校验和媒体源构造重复读取。`internal/service/emby_playback.go:24-46`
- 选择了字幕时，目标媒体的 sidecar 字幕会在选择校验和媒体源构造中重复发现；每次发现都会再次查询媒体并扫描相邻字幕目录。`internal/service/emby_playback.go:76-89`, `internal/service/subtitle.go:74-108,139-187`
- 服务端生成的字幕 `DeliveryUrl` 已使用具体 `media.id`，但字幕路由仍先经过通用 `PlayableMediaID`，之后 `ServeByIndex`、`Discover` 和 `Serve` 又重复加载媒体。`internal/handler/emby_playback.go:164-180`, `internal/service/subtitle.go:111-125,139-147,208-220`
- `playableMedia` 对普通具体媒体 ID 也会先执行 season 与 series 查询，再查询具体媒体。`internal/service/emby_playback.go:140-155`
- Web `ExternalPlayerButton` 同时调用 `/external-players` 与 `/external-url`，而前者已经返回 `players + url`。`web/src/components/ExternalPlayerButton.tsx:22-31`, `internal/handler/playback_extra.go:82-96`
- `PlayerPage` 手写了剧集返回路径；`mediaLibraryBackTarget` 已拥有同一媒体库/剧集定位规则。`web/src/pages/PlayerPage.tsx:32-40`, `web/src/pages/MediaDetailPageModel.ts:4-12`

## Requirements

1. 单次 PlaybackInfo 请求只解析一次可见 sibling 集，并在后续选择校验和响应构造中复用。
2. 单次 PlaybackInfo 请求只批量加载一次 sibling probe 文档；缺失文档仍按现有规则异步修复，不能阻塞响应。
3. 单次 PlaybackInfo 请求中，每个 sibling 的 sidecar 字幕最多发现一次，并在选择校验与 `MediaStreams` 构造中复用；复用范围仅限当前请求。
4. 服务端生成的 concrete media 字幕 URL 走直接媒体快路；手工传入 metadata、season 或 series ID 时保留现有兼容解析与可见性校验。
5. 单次字幕交付在确定具体媒体后复用该媒体记录，避免 `Discover` 与 `Serve` 重复查询；路径约束、索引重校验和 sidecar 实时发现语义保持不变。
6. `playableMedia` 应先识别具体媒体或直接挂载媒体的 metadata ID，仅在未命中时查询 season/series group；现有首选版本规则不变。
7. Web 外部播放器弹窗只调用 `/external-players` 并直接使用其 `url`；保留 `/external-url` API 供其他客户端使用。
8. `PlayerPage` 保持 `location.state.from` 最高优先级，其余媒体库/剧集返回目标复用 `mediaLibraryBackTarget`；无媒体库目标时仍回退 `/media/:id`。
9. 不改变 Emby 路由、参数、响应结构、错误状态、字幕索引、播放选择或鉴权契约，因此管理员 Emby API 目录无需修改。

## Acceptance Criteria

- [x] metadata ID 与 concrete media ID 的 PlaybackInfo 都返回与当前一致的可见 `MediaSources`、选中索引和直接播放 URL。
- [x] 一次 PlaybackInfo 对 sibling probe 文档只执行一次批量读取，目标文档不再被单独重复读取。
- [x] 一次 PlaybackInfo 对每个 sibling 的 sidecar 字幕最多扫描一次；显式字幕选择不会让目标媒体重复扫描。
- [x] probe 文档缺失时仍只触发既有异步修复，响应保持非阻塞。
- [x] concrete media ID 的字幕交付成功且不经过通用 group 解析；metadata/season/series 兼容路径仍可解析到可见具体媒体。
- [x] 字幕索引在 sidecar 删除后仍返回未找到，路径逃逸仍被拒绝，不能复用过期的跨请求发现结果。
- [x] 普通 media ID 不再先查 season/series；series 与 season ID 的既有首选 episode 行为保持不变。
- [x] 打开 Web 外部播放器弹窗只产生一次 `/external-players` 请求，播放器列表和直链仍正常显示。
- [x] PlayerPage 返回顺序保持 `state.from` → 媒体库/剧集 → `/media/:id` → `/`。
- [x] 后端定向测试、相关 `go vet`、Web `npm run lint`、`npm run build` 与 `git diff --check` 通过。

## Out of Scope

- 不增加全局缓存、TTL、Redis、数据库索引或 schema 变更。
- 不调整 ffprobe 执行策略、异步修复并发限制、播放转码或 Range 传输。
- 不删除 `/external-url`，不改变第三方客户端可用接口。
- 不抽象全站 API 错误消息解析、播放进度 keepalive 或详情页 probe 状态处理。
- 不修改管理员 Emby API 目录，除非实现时发现对外契约不可避免地发生变化；发生时必须退回规划确认。

## Risks and Deferred Items

- ID 快路必须保留用户可见性、NSFW、媒体库权限和跨版本选择规则；不能直接用未过滤的 raw media 替代 `MediaView` 校验。
- 字幕复用只能存在于一次请求内，避免 sidecar 新增/删除后继续使用旧结果。
- 当前 DBX 实测单条 probe 查询很快，优化价值主要是减少请求内往返与重复目录扫描，不承诺固定毫秒收益。
