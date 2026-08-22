# 设计：播放器请求内复用与 Web 单一逻辑

## Boundary

本任务只调整 Emby 播放服务、字幕服务和两个 Web 调用点。数据库结构、路由注册、JSON 契约、权限规则及播放器传输层保持不变。

预期产品代码改动集中在：

- `internal/service/emby_playback.go`：ID 解析顺序、PlaybackInfo 请求内数据组织和选择校验。
- `internal/service/emby_item_identity.go`：保留 metadata ID 解析已经加载的可见版本，避免随后重查 siblings。
- `internal/service/emby_media_sources.go`：使用已加载 probe 文档与字幕选择构造媒体源。
- `internal/service/subtitle.go`：对已加载媒体执行发现、索引重校验和交付。
- `internal/handler/emby_playback.go`：concrete media 字幕快路及兼容回退。
- `web/src/components/ExternalPlayerButton.tsx`：复用 `/external-players` 的 URL。
- `web/src/pages/PlayerPage.tsx`：复用 `mediaLibraryBackTarget`。
- 对应后端定向测试；Web 现有无测试框架，不为两个一行复用改动新增框架。

## Backend Data Flow

### PlaybackInfo

```text
request ID
  → resolve selected media + visible siblings once
  → batch-load probe documents once
  → schedule repair only for missing documents
  → discover each sibling's current subtitle selections once
  → validate requested source/audio/subtitle against loaded request data
  → build MediaSources from the same request data
```

请求数据使用局部变量或最小的私有参数传递，不进入 `EmbyService` 全局状态，不引入缓存生命周期与失效逻辑。

### Playable ID Resolution

先用现有可见 `MediaView` 查询识别 concrete media 或直接挂载媒体的 metadata。仅未命中时执行 season/series group 查询。metadata、season、series 的兼容行为仍由现有方法完成，避免在 handler 复制版本选择规则。

### Subtitle Delivery

```text
route ID
  → concrete media visible fast path
  → otherwise existing PlayableMediaID compatibility fallback
  → load concrete media once
  → load probe document once
  → rediscover current sidecars and resolve index
  → validate selected path against the same media directory
  → stream/convert subtitle
```

字幕服务增加仅在内部使用的“已加载媒体”执行路径，公共 `Discover`/`Serve` 行为保持兼容。每次请求仍重新扫描目录，确保 sidecar 删除测试继续成立。

## Web Data Flow

- `/external-players` 继续是弹窗所需 `players + url` 的单一响应；`externalURL` API 方法保留但该组件不再调用。
- `PlayerPage` 只保留 `state.from` 与详情页 fallback；媒体库/剧集 URL 由现有 `mediaLibraryBackTarget` 生成。

## Compatibility

- Emby 路由、状态码、字段、字幕 DeliveryUrl 与鉴权不变。
- metadata、season、series ID 仍可通过兼容回退解析。
- 选择值 `nil`、`0`、`-1` 和非法索引的语义不变。
- `/external-url` 不删除、不重定向。
- 无数据库迁移、无配置变化。

## Trade-offs

- 不做跨请求缓存：目录 IO 每个请求仍至少一次，但没有失效风险。
- 不创建通用 request context 框架：仅传递本链路已经需要的文档与字幕结果，避免单用途抽象扩散。
- 不单独优化两条约 0.1ms 的空 group SQL；该收益随共享 ID 解析顺序调整自然获得。

## Rollback

每一项均为局部代码与测试变更，可按实施清单逐项回滚。无 schema、缓存或数据写入变化。
