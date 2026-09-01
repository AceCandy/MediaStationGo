# 外部 API 代理池技术设计

## 1. 设计目标与边界

本次只实现一个全局静态代理列表和豆瓣 JSON GET 请求的显式接入。代理配置、HTTP 客户端构建和豆瓣线路状态分别归属配置服务、共享代理池服务和 `DoubanProvider`；不改全局 `NewExternalHTTPClient`，避免其他 Provider 被隐式接入。

数据流：

```text
Web 外部 API 页面
  → 管理 API（校验并屏蔽凭据）
  → proxy_pool_entries（整条 URL 加密、顺序明文）
  → ProxyPoolService（解密、构建长期复用的 Transport、发布 generation）
  → DoubanProvider（读取 use_proxy_pool、维护豆瓣独立粘性线路）
  → 真正直连或指定代理
```

不复用 `APIConfig.Extra` 或通用 `settings`：两者现有列表接口会原样返回值，无法满足代理凭据不泄露的要求。也不使用伪 Provider 行承载代理池，避免两套 APIConfig 服务把内部行当作普通 Provider 暴露。

## 2. 持久化模型

### APIConfig 扩展

在 `model.APIConfig` 增加：

```go
UseProxyPool bool `gorm:"default:false" json:"use_proxy_pool"`
```

同步贯通 `PublicView`、`Resolved`、`APIConfigPatch` 和前端类型。`APIConfigService` 维护一个进程内单调 revision，每次成功更新或删除豆瓣配置后递增，`Resolved` 携带当前 revision；应用重启本身会重建 `DoubanProvider`，无需持久化该运行时版本。启动兼容迁移显式补充该列，默认 `false`，不改变已有安装行为。历史 `model.ApiConfig` 不消费该字段，但仍映射同一张表；不得修改其现有共享列定义。

### ProxyPoolEntry

新增独立表模型：

```go
type ProxyPoolEntry struct {
    PermanentBase
    URL      string `gorm:"type:text;not null" json:"-"`
    Position int    `gorm:"not null;index" json:"position"`
}
```

- `URL` 保存完整代理 URL 的 AES-GCM 密文，复用现有 `CryptoService`。
- `Position` 是管理员顺序的唯一依据；读取时再以 `created_at`/`id` 稳定排序。
- 使用 `PermanentBase`，删除节点时物理删除密文，不保留软删除凭据行。
- 保存前若 `CryptoService` 无法产出带加密前缀的密文，则拒绝更新，不能降级为明文存储。
- 表通过现有 `AllModels()` / `AutoMigrate` 创建；`api_configs.use_proxy_pool` 通过现有兼容迁移补列。

## 3. 管理 API 契约

新增管理员路由，避开现有 `/admin/api-configs/:provider` 动态路由：

```http
GET /api/admin/api-proxy-pool
PUT /api/admin/api-proxy-pool
```

其中 Gin 的管理员路由组内注册路径为 `/api-proxy-pool`；`/api/admin` 是应用统一外部前缀。

GET 响应：

```json
{
  "items": [
    {
      "id": "uuid",
      "display_url": "http://proxy.example.com:8080",
      "has_auth": true
    }
  ]
}
```

PUT 请求：

```json
{
  "items": [
    {"id": "existing-id"},
    {"id": "replace-id", "url": "socks5://user:pass@host:1080"},
    {"url": "http://new-host:8080"}
  ]
}
```

- 数组顺序即保存顺序。
- 已有 `id` 且省略 `url` 表示保留原密文；提供 `url` 表示替换。
- 无 `id` 的新节点必须提供 `url`。
- 未出现在请求中的已有节点被删除；整个替换操作在一个事务内完成。
- 拒绝重复或不存在的 `id`，任一节点无效时整次请求不落库。
- 响应始终使用 GET 的安全投影；`display_url` 完全移除 `Userinfo`，以 `has_auth` 表示认证存在。

### URL 规范化与校验

- 裸 `host[:port]` 按现有规则补为 `http://`。
- 仅接受 Go Transport 原生支持的 `http`、`https`、`socks5`、`socks5h`。
- 必须有 hostname；拒绝 query、fragment 和非根 path。
- 认证信息允许输入但只进入密文，不进入响应和错误文本。
- 错误只指出列表位置和规则，例如“第 2 个代理协议不支持”，不回显原值。

## 4. 运行时代理快照

新增 `ProxyPoolService`，负责：

- 首次使用时从数据库加载有序条目，解密并为每个节点创建长期复用的 `http.Client` / `http.Transport`。
- Transport 复用 `NewExternalTransport` 的连接池参数，但将 `Proxy` 固定为 `http.ProxyURL(parsedURL)`。
- 以不可变快照发布 `{generation, clients}`；保存列表成功后重建快照、递增 generation，并关闭旧 Transport 的空闲连接。
- 配置为空时返回空 clients，而不是回退到环境/系统代理。

`ProxyPoolService` 不保存某个 Provider 的当前线路。这样后续 Provider 即使复用静态配置，也会有自己的失败判断和粘性状态。

## 5. 豆瓣请求选路

`DoubanProvider` 保留现有 `client` 作为开关关闭时的环境/系统代理感知客户端，并新增：

- 一个 `Proxy=nil`、15 秒超时的真正直连客户端；
- `ProxyPoolService` 引用；
- 豆瓣独立的当前线路、快照 generation 和互斥状态。

Search、Discover、Detail 统一通过一个读取完整响应体的 JSON GET helper。每次实际 HTTP 尝试都重新创建 GET Request，并设置现有 User-Agent、Referer、Accept、Accept-Language 和当次解析到的 Cookie，避免重用已发送 Request。调用方继续生成现有 `douban search/discover/detail: <status>` 错误并保留详情分类逻辑。

### 开关关闭

1. 解析当前 `douban` 配置。
2. `use_proxy_pool=false` 时将粘性状态重置为真正直连。
3. 本次请求仍只使用原有 `client`，不使用新直连客户端或代理池。

### 开关开启

线路结果只看 HTTP 状态：精确 400 为失败；任何其他 HTTP 状态均停止选路并沿用调用方现有语义。没有 HTTP 响应的网络/超时/DNS/TLS 错误也立即返回且不修改当前线路。

```text
先请求当前线路
  非 400 → 返回；若这是重选中的候选线路，则保存为当前线路
  400：
    当前线路是代理 → 真正直连
    当前线路是直连 → 跳到代理列表
  真正直连仍 400 → 按配置顺序代理 1..N
  全部代理均 400 → 最后真正直连一次
  最后直连非 400 → 当前线路=直连并返回
  最后直连 400 → 当前线路=直连并返回现有 400 错误
```

代理池为空等价于 `N=0`，因此首次直连 400 后仍执行最终直连。

### 并发与配置变更

- 正常使用当前线路的请求只短暂读取状态，可以并发执行。
- 仅 400 后的重选过程串行化；拿到重选锁后先比较当前线路和 generation。若另一请求或配置更新已经改变状态，则从最新状态重新判断，避免多个失败请求互相覆盖线路。
- 代理列表 generation 或豆瓣配置 revision 改变时，豆瓣当前线路重置为真正直连；revision 不依赖数据库时间精度，因此即使关闭期间没有请求，连续重新开启后的首个请求也不会复用旧代理。
- 快照在一次重选中保持不变；配置更新只影响后续新快照，不中断已经发出的请求。

## 6. Web 交互

- 豆瓣编辑行的高级设置增加“使用代理池”复选框，保存到 `use_proxy_pool`。
- Provider 表格下方新增“代理池”面板。
- 已有节点显示 `display_url` 和“已配置认证”标记；认证值不回填输入框。
- 节点支持上移、下移、替换和删除；新增/替换时使用 password 类型输入。保存时未替换的节点只提交 `id`，避免把掩码当成新凭据。
- 保存失败保留本地编辑内容并展示后端安全错误；保存成功刷新安全投影。

## 7. 兼容、失败与回滚

- 默认关闭保证升级后现有豆瓣网络路径不变。
- 无效代理列表更新整体失败，旧数据库记录和运行时快照继续生效。
- 代理配置读取/解密失败时返回配置错误，不把密文或原始 URL 写入日志，也不静默改走环境代理。
- 回滚代码时新增表和布尔列可保留为空闲数据，不需要破坏性降级迁移；运行中可先关闭豆瓣“使用代理池”立即恢复旧路径。
- 不新增动态健康状态、失败计数、随机调度、后台 goroutine 或新第三方依赖。
