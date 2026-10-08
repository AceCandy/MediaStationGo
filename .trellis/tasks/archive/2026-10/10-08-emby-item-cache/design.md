# ITEM 接口缓存详细设计

状态：设计交付完成，按用户要求提交归档；列表/详情/Resume 统一五分钟，仅处理 Emby API。尚未实现或运行功能验收。

## 1. 推荐方案与假设

复用 RuntimeCacheService，统一缓存通用 Items 的最终服务响应，给 HTTP 单条目浏览增加缓存入口。Emby 列表、详情、Resume 及 IsResumable 新响应统一固定 300 秒；用一个 Emby 专用 TTL 常量，保留 cache.media_ttl_seconds 的 15 秒现有默认，不改变 Web 和其他缓存。以仓储读版本、全局内容版本和用户状态版本隔离旧结果，补齐写端通知。

用户已确认单实例，并授权选择 Redis/本地方案。采用现有本地 L1 + Redis L2：先读本地，未命中再读 Redis，仍未命中才读取数据库并保存未注入凭证的快照。配置且连接可用时写入两层；未配置或连接失败时本地缓存仍可工作，运行时 Redis 失败不能阻断查询。

缓存收益和正确性同时验收：有内容变化就失效相应范围，没有变化不做无意义全清；不能把“永远生效”实现成永不过期或永久命中。Emby 采用 5 分钟 TTL 并保留容量淘汰，数据不变的请求在有效期内可复用。

## 2. 当前链路

```text
认证与目标用户校验
  → 参数解析
  → Items 归一化与前瞻
  → items 分发（人物 / IDs / 搜索 / 来源 / 层级 / 库作品 / mediaItems）
  → 裁切前瞻与计数下界
  → 收藏 LibraryIds 补充
  → HTTP 注入本次 token
  → JSON
```

当前只有 mediaItems 缓存。命中仍需走前置分发和收藏归属补充；逐来源增加缓存会重复规则并遗漏分支。

现有键将字段以 | 和 , 拼接后散列，并排序 IDs。扩展到直接 IDs 查询后，[b,a,b] 与 [a,b,b] 响应不同，不能共用键；结构化编码也能避免字段内分隔符造成的碰撞。

当前 DeletePrefix 只删除现存条目，无法阻止先前查询随后写回旧响应。必须同时补版本隔离。

## 3. 范围矩阵

| 入口/请求 | 本次行为 | 说明 |
| --- | --- | --- |
| Items、用户 Items 及别名 | 缓存最终服务响应 | 一处覆盖四来源、搜索、人物、层级和库作品 |
| 经 Items 查询的 Shows 季/集 | 使用同一服务缓存 | 保留专用入口计数及参数，不套通用 HTTP 默认 |
| Items 的 IDs | 缓存，保留顺序和重复项 | 不按集合解释 |
| 单条目浏览详情及别名 | 新增窄缓存入口 | 不影响其他内部 Item 调用 |
| Items 内根目录/人物/搜索 | 缓存 Items 响应 | 专用接口另算 |
| Random（包括组合排序） | 跳过缓存 | 不缓存随机种子 |
| Emby Resume、Items 的 IsResumable | 5 分钟缓存 | 用户状态、内容和权限版本均参与键；不改变续播算法 |
| 独立 Latest/Views/Persons/SearchHints/Counts | 本次不新增 | Latest 后续可独立扩展 |
| NextUp/PlaybackInfo/PlaybackProgress | 保持现有入口 | 固定兼容响应、播放选择和精确进度不新增缓存 |
| Web API、Web 浏览器缓存 | 本次不修改 | 共享服务仅补必要的 Emby 失效通知 |
| 播放重定向/图片/字幕字节 | 保持现有机制 | 生命周期及安全边界不同 |

合法空列表可缓存。详情 nil、错误和请求取消不缓存。

## 4. 放置位置与响应快照

### Items

缓存放在 EmbyService.Items 的公共包装层：归一化外部 Limit/StartIndex → 判断是否适用 → 捕获版本与可见范围并尝试命中 → 原样执行前瞻、分发、裁切、收藏归属 → 全部成功后保存最终响应 → HTTP 注入 token。

移除 mediaItems 的旧读写，避免两层缓存及旧键绕过版本保护。保留查询、排序及 payload 逻辑。新键带 v2，旧条目自然过期。

现有规范的“缓存原始前瞻页、裁切返回页”描述内层布局；迁移时更新为最终页缓存，仍保留多取一项、500/501、准确计数及下界。命中最终页不能再次裁切或重复补充 LibraryIds。

### 浏览详情

新增 EmbyService.CachedItem(ctx, itemID, userID)，仅由 embyItemByIDHandler 调用；未命中调用原 Item。

原 Item 保持内部读取入口：收藏/已看等写后返回、ID 解析和其他业务调用继续走它，避免 JSON 解码改变 []map[string]any 或整数的 Go 类型，影响内部消费者。IDs 列表也调用原 Item，仅缓存最终列表，不叠加详情缓存。

### Emby 继续观看

新增面向 HTTP 的 CachedResumeItemsPage，供 embyResumeItemsHandler 调用；内部 ResumeItems/ResumeItemsPage 保留原算法和类型。缓存采用独立 kind=resume，完整捕获原入口归一化后的 UserID、分页、Fields、过滤及有效范围，保持准确总数、续播代表版本和 resume-or-next 行为。缓存位于 token 注入前，TTL 与 Items/详情均为 300 秒。

Items 的 IsResumable 由通用列表缓存覆盖，保留其 resume-only 行为，不混用 Resume 的推荐候选。自动/手动进度、历史删改、版本删除、下一集入库或权限变化必须失效对应结果；Random 即使同时带续播筛选也跳过缓存。

### 编解码和对象独立

新增 ITEM 值保存 json.RawMessage 响应快照，使用现有 GetJSON/SetJSON 传输小包装。Items 读取恢复明确的 Items []map[string]any、TotalRecordCount int64、StartIndex int，嵌套数字用 json.Decoder.UseNumber 保精度。CachedItem 解码为 map；HTTP token 递归函数已支持 []any。

只存序列化字节，每次命中解码为独立对象；冷读在 token 注入前保存字节。任何响应修改都不会污染缓存。实施时检查内部 Items 消费者的类型断言，不能只比较 HTTP JSON。

DirectStreamUrl 是本服务稳定播放路径；STRM 的 MediaSource.Path 也映射回本服务（emby_playback.go:259、:361）。不缓存真实上游重定向、签名、播放会话或原始 STRM 目标，不新增敏感日志。

快照包装与解码流程固定为：

```go
type embyResponseSnapshot struct {
    Body json.RawMessage `json:"body"`
}
```

SetJSON 保存该包装，Body 是完整且尚未注入 token 的响应 JSON；GetJSON 只恢复包装，不能把 Body 先解码成 any。随后对 Body 创建 decoder 并调用 UseNumber：列表解码到带三个明确字段的 envelope，再返回 map[string]any；详情解码到 map[string]any。嵌套集合保持 HTTP 层接受的 []any，内部 Item 则仍返回原类型。新增 Items 命中路径若有内部消费者依赖嵌套 []map[string]any，应在缓存恢复处仅恢复这些确切字段，并补测试，不扩张为通用 JSON 类型转换框架。

列表和详情的完整 Body 超过 1 MiB 时均正常返回、跳过缓存；沿用 2048 条共享容量与现有 TTL，无新清理协程。1 MiB 是单条入缓存门槛，不宣称总内存硬上限：条目及 JSON 对象仍有额外开销。

## 5. 键设计

以固定结构体 JSON 编码后 SHA-256，不手工拼接字段值。

```text
media:emby:v2:<ownerHash>:<结构化身份的 SHA-256>

结构化身份：
  schema、kind(items/detail/resume)
  repositoryReadVersion、contentVersion、userStateVersion
  userID、authenticatedAdultSnapshot、effectiveVisibility
  itemID 或 ItemsParams 的全部外部查询字段
```

- repositoryReadVersion 复用 repo.ReadCacheKey()，隔离仓储实例及默认播放配置/成人/库配置变更。
- ownerHash 是完整 userID 的 SHA-256；有用户状态的不同用户不可共用。匿名服务调用使用独立标记，不能与用户空字符串编码混淆。
- contentVersion 和 userStateVersion 见第 6 节；用户前缀可直接删除，无须扫描整个缓存找 userID。
- 可见范围包含 IncludeNSFW、LibraryRestricted、AllowedLibraryIDs、HiddenLibraryIDs，来自原权限投影，不重写权限算法。
- 按现有 mediaVisibility 的方式隔离认证 hideAdult 快照；旧快照不能填充新权限请求使用的键。
- 列表参数完整覆盖 UserID、ParentID、IDs、PersonIDs、SearchTerm、IncludeItemTypes、Filters、Fields、Recursive、SortBy、SortOrder、Limit、StartIndex、SkipTotalRecordCount。
- IDs 保留顺序/重复，其余数组先保留解析后的顺序；宁可多 miss，不做未经证实的集合化、大小写合并或重排。
- token、设备 ID、请求 URL 和随机种子不进入键。
- 只保留当前已有参数归一化，不统一重写各来源排序默认。

## 6. 写后失效和并发保护

### 版本机制

在共享 RuntimeCacheService 维护全局内容代数与每个真实用户的状态代数；读者捕获 contentVersion、userStateVersion 和 repo.ReadCacheKey()。三个版本必须都参与实际使用的键和命中后的比较。

- DeletePrefix("media:") / DeletePrefix("media:emby:") 推进全局内容代数后删除原范围。
- 新增统一用户状态失效入口，先推进该用户代数，再删除 media:emby:v2:<ownerHash>:；不能因此推进全局代数。
- 用户代数表在单实例共享 Cache 内同步维护，只接受已解析/授权的实际用户，不按任意请求参数制造无限命名空间；不能随意清零或回收造成旧键重新有效。当前规模按真实用户数增长，进程重启通过仓储实例身份隔离旧键。
- 原删除规则继续保留；换代防旧结果被读，删除释放空间。L1 和 Redis 使用同一版本键；换代不依赖 Redis 成功或 context 有效。

开始时保存三个可比较的快照，键只使用这些开始值。命中解码后、原查询/序列化完成后，再比较当前版本；变化时不存结果，不把旧数据迁移到新键，缓存命中转原读取而不无限重试。最终比较与 SetJSON 之间再有写入时，结果也只能存旧版本键，不会污染新请求。

```text
A 用户读取：内容 7，A 状态 3
B 用户播放：只把 B 状态 5 → 6；A 的缓存仍有效
A 用户收藏：A 状态 3 → 4；A 原查询不得写到状态 4 的键
新影片入库：内容 7 → 8；所有用户的旧内容缓存均不再命中
```

写成功返回后的新请求不命中对应旧值；已开始的请求可以按原快照完成。每次认证/授权都在缓存之前执行。

### 哪些情况不失效

- 缓存读取、HTTP token/设备变化、常规登录续期，不改变媒体响应；token 仍逐请求注入。
- 参数校验失败、完整事务回滚、未找到目标、进度被既有门槛忽略、零行历史删除，不制造变更通知。
- 内部重试时间、任务进度、诊断日志、下载分片状态不影响当前 ITEM 展示时，不清缓存；真实下载入库/绑定变化仍清。
- “幂等接口成功”不自动等于“无变化”：相同收藏状态可能更新代表 media_id，同位置上报可能更新 watched_at/版本/有效完成状态。没有可靠 changed/RowsAffected 信号时，保守失效该用户，不额外查数据库做昂贵差异比较。
- 同一次事务和同一用户写只能有一个通知所有者；Emby 转调 PlaybackService 时不能里外重复通知。直接 Emby 写路径各自负责，公共写路径由服务提交边界负责。

### 写端矩阵

| 变更 | 已有机制 | 必要补全 |
| --- | --- | --- |
| 普通扫描/刮削/编辑/删除 | media: 失效已包含 ITEM | 复用并核对提交后调用，不重复清 ITEM |
| 异步轨道探测成功 | media_probe.go:164 清 media: | 复用，测试探测落库与旧详情回填竞争 |
| Emby 收藏/已看/进度/精确同步 | 当前清 media:emby: 全局 | 改为当前用户代数/前缀；覆盖所有来源、容器汇总、前集补标 |
| Web 进度/收藏/历史删改 | PlaybackService 未注入 Cache | 注入共享 Cache，公共变更方法提交后仅清该用户；本次不改 Web watch_history 的两个直接删除 handler，按已知边界记录 |
| NFO 手动编辑 | updateNFOMetadata 未失效 | 数据提交后调用现有 MediaService 失效，不等待读回详情成功 |
| 红果/黄果资料/合集/绑定/图片 | 来源服务未注入 RuntimeCache | 注入共享 Cache，每个改变公开响应的成功提交点通知；纯重试/排名内部字段不清 |
| 默认播放配置/成人/库基础 | ReadCacheKey 已换代 | 纳入响应键，无需再建 visibility 清理系统 |
| 用户禁用/撤销/功能权限 | 请求前认证/权限检查 | 每次执行，不把 Web 权限表当 Emby 可见库范围 |

资料提交后绑定或网络补充失败，已经提交的资料仍须失效。不能只在整个批任务 err==nil 时通知，也不能在事务提交前换代。临时 repository.New(tx) 的私有版本不能代替主服务版本。

用户状态失效按用户，不按单个作品：父级 Played/未看数、收藏列表、筛选分页与代表版本也会变化，删一个详情不足以保证正确。公共内容变化暂按全局，避免遗漏跨库归属、来源合集及搜索索引影响；不建立作品反向依赖图。

已点验但本轮排除：Web watch_history.go:118/:160 的直接历史删除；不修改这两个 Web handler 时，其影响只能由 TTL 兜底。若要求这两条 Web 写也即时刷新 Emby，实施前需另确认仅加失效通知。共享 PlaybackService 的写后通知仍纳入。其余补充入口：media_library_roots.go:236 的先删媒体后删根；people_backfill.go:139 的演职员提交；scraper.go:218 之后的资料提交和媒体更新失败。库封面 UpdateFields 已换代仓储读版本，不能误报为没有任何保护。完整条件和证据见 research/invalidation-policy.md。

数据库提交后即使 HTTP context 取消，仍先执行对应的本地全局/用户换代，再进行内存与 Redis 删除。Redis 删除如需独立 context，使用短超时；失败不让本进程命中旧版本。不启动无期限清理任务。

## 7. 动态数据与降级

- 详情未命中仍异步探测，不变为同步等待；探测成功通知，失败保持现状，TTL 后再走原路径。
- 外部字幕文件变更在本进程发现前不承诺即时响应；无显式通知时等待 TTL 后重新组装。若需要字幕严格实时，须另选每次重投影 MediaSources 的方案。
- Cache nil、JSON 损坏/编解码失败、Redis 故障回退原读取；错误不伪装为空成功，不缓存负面详情。
- 新键只读新结构；无需迁移旧条目或持久数据。
- 不新增 singleflight、预热和 stale-while-revalidate。冷读并发仍可能重复查询，验证需区分首次并发与暖缓存重复请求。

## 8. 兼容与回滚

路由、参数、认证、HTTP 状态、JSON shape 不变。计数模式分键，Shows 保留其原模式；LibraryIds 是补充完成后的可见集合；容器播放状态继续来自原聚合。

比较 API catalog；透明缓存没有外部契约变化时不修改前端。更新工作级查询规范的缓存布局与版本保护说明。

回退新增读取入口与内层迁移即可恢复原实时读取；写端失效补全可保留。无 schema、持久迁移或不可逆操作。

## 9. 已确认单实例边界

仓储版本和 L1 是进程内状态。A 的 Redis 删除不能清 B 的 L1，B 的权限版本也不会自动变化；Redis 存储本身不是全实例同步。

若需多实例即时一致，应调整为共享媒体/权限版本，每次命中前校验共享版本，L1 绑定版本；校验失败绕过缓存并读取当前权限。仅 SCAN/DEL 或仅 Pub/Sub 不足以可靠保证。还需规定 Redis 故障降级、数据库权限直读与双实例验收，本提案不提前实现。

## 10. 改动边界

必改：emby_compat.go（最终列表）、emby_items_cache.go（键/快照/CachedItem）、emby_items_list.go（移除内层读写）、runtime_cache.go（内容/用户代数与前缀）、emby_items_handlers.go（详情/Resume 浏览入口）及对应测试。

失效补全：playback.go、emby_user_data.go、emby_progress_sync.go、service_builder.go、nfo_metadata.go、media_library_roots.go、people_backfill.go、scraper.go、hongguo.go、hongguo_album.go、huangguoai.go；图片/绑定辅助文件按真实提交清单确定，实施前须列全。

不改来源查询 SQL、仓储用户状态算法、播放协商、前端页面或无关缓存。

## 11. Web 修改边界

不新增/延长 Web 服务端缓存，不改 history.ts、playback.ts，不取消其原 5 秒缓存。Web handler 也排除本轮改动。共享 PlaybackService/Scraper/Scanner 等服务仅补 Emby 失效通知，读取行为和 Web HTTP 契约不变。直接数据库历史删除是已知剩余边界，不能在宣称“失效全覆盖”时隐藏。
