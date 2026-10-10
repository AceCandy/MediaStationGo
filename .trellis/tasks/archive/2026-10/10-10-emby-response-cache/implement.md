本次状态：实现与独立复核已完成，按用户授权提交归档。沿用已确认的单实例与 Web 排除边界；实际验证结果见本轮实施记录。

# ITEM 缓存实施与验收计划

状态：实施完成。以下计划保留原设计阶段的验收要求，本轮实际执行结果见文末记录。

## 顺序与验证点

1. 列全实际提交入口：媒体、来源资料/合集/绑定/图片、Web/Emby 收藏/历史/进度。确认 media: 已覆盖 ITEM，权限依赖 ReadCacheKey；不修改无关 PermissionService。
2. 先补运行时内容/用户代数及可单用户删除的结构化 v2 键。验证顺序、分隔符、版本切换、旧读回填与 context 取消；不依赖 sleep 定时猜竞争。
3. 补写端通知：给 Playback/必要来源服务注入同一 Cache；覆盖事务、部分提交、共享服务历史删除、自动补标前集及 NFO 编辑；消除双重通知与确定的 no-op 全清。每组验证写后列表和详情读取。
4. 统一 Items 最终页读写，移除 mediaItems 内层缓存。保留前瞻及 LibraryIds 算法，恢复顶层类型和嵌套数字精度；1 MiB 以上跳过缓存。
5. 接入 CachedItem 和 CachedResumeItemsPage 浏览入口，统一 Emby TTL 300 秒；Items 的 IsResumable 也缓存，保留 resume-only 与 resume-or-next 区别。验证两个 token、字幕 URL、对象独立、内部 Item 调用不变和异步探测。
6. 独立复核全部 diff 与需求矩阵；执行受影响包回归，更新缓存布局规范和实际验证记录。纯透明缓存只比较 API catalog，不无故修改前端。

每一步可独立撤回。新值 v2 不读取旧结构，写端失效补全不依赖新增读取入口。

## 验证矩阵

| 分组 | 必测案例 | 证据 |
| --- | --- | --- |
| 键/协议 | IDs 顺序/重复，字段内逗号/竖线，不同用户/Fields/分页/计数 | 键区分及实际完整 JSON 等价 |
| 覆盖 | 四来源、电影/剧集/混合库、搜索/人物/根/季/集/收藏 | 冷暖 JSON 对比，暖读候选/状态/展示 SQL 为零 |
| 分页 | 首尾/越界页、准确/下界、500/501、IDs 完整集合 | optional-total/count-mode 原回归不变 |
| 权限 | 默认配置增删/切换、空受限配置、允许库、成人映射、hide_adult、库 type/name/display、目标用户 | 写后新请求新版本；认证/撤销/跨用户原行为 |
| 用户状态 | Web/Emby 收藏、进度、已看/未看、精确同步、历史删除、自动补标 | 写后列表/详情一致；其他用户隔离；回滚不制造成功 |
| 来源/媒体 | NFO 编辑，红果资料/合集/绑定，黄果资料/绑定，图片，扫描/刮削/编辑/删除 | 每个提交点生效；后半段失败仍通知已提交内容 |
| 竞争 | channel 屏障控制旧读、写后换代、旧读结束；权限快照和异步探测 | 后续新请求不能命中旧响应 |
| 命中保持 | A 写进度/收藏，B 暖读仍命中；短进度、零行删除、回滚不换代；同值但影响时间/代表版本的写仍通知 | 捕获对应全局/用户版本及实际 SQL |
| 安全/类型 | 两 token，修改嵌套对象，大整数，顶层 Items 类型，无请求 session/上游目标 | 伪造 fixture 和缓存字节检查，不打印真实凭证 |
| 故障/资源 | nil Cache、损坏 JSON、Redis 故障、取消、不存在、合法空页、TTL、容量、1 MiB | 回退、负面不缓存、过期重读、超限返回正常 |
| 动态入口 | Random 跳过；Resume/IsResumable 冷暖读、准确总数、版本、跨季推荐及写后失效 | 原协议不变；精确进度/PlaybackInfo 仍实时 |
| 修改边界 | Emby 专用 300 秒常量；Web 配置/handler/浏览器文件无改动 | Web 原 TTL 与接口行为不变，直写历史删除的已知延迟单列 |

## 建议命令及条件

原设计阶段仅交付文档。本轮已执行隔离数据库、Redis 与回归验证，以下保留原建议命令。

实现时新增回归以 TestItemCache/TestRuntimeCache 前缀命名，确保以下筛选实际覆盖：

```bash
go test ./internal/service ./internal/handler ./internal/repository -run 'Test(ItemCache|RuntimeCache|EmbyItemsCache|EmbyItemsOptionalTotal|EmbyItemsCountModes|EmbySeriesAndSeasonPlayedState|EmbyTargetUserRequired|Favorite|Playback|Continuation)' -count=1
go test -race ./internal/service ./internal/handler ./internal/repository -run 'Test(ItemCache|RuntimeCache)' -count=1
git diff --check
```

据实际测试名追加来源、层级、字段及权限回归；最终执行受影响包完整测试，筛选命令不是完整质量门。

PostgreSQL 使用 MEDIASTATION_TEST_POSTGRES_DSN 及 internal/testdb/postgres.go 的随机隔离 schema；缺失 DSN 导致 Skip 时记为未验证。Redis 集成只用测试命名空间；若启动临时 Redis，结束关闭。

性能独立记录相同数据/查询的冷暖耗时、SQL、快照大小与失效频率；前置认证/可见性查询单列，不承诺未经测量的毫秒数和命中率。播放器实机体验另列未验证。

## 最终审查门

- 单实例边界已确认；修订方案需包含用户失效而非旧全局状态失效。Latest 未扩大范围。
- implement/check JSONL 引用实际规范与研究；不是空模板。
- 两份规范超过自动注入字节上限：work-level-queries.md、playback-contracts.md。实施/审查必须完整读取原文件，不能把被截断的自动上下文当作完整规范；不为本任务修改全局注入配置。
- 所有提交点通知；部分成功/取消不遗漏，回滚不通知成功数据。
- 仓储读版本、内容/用户代数、认证快照和 token 注入分别测试。
- JSON 冷暖等价，顶层 Go 类型兼容，嵌套整数无精度损失。
- 更新规范的缓存布局；不把透明缓存变成前端功能。
- 记录实际执行/Skip/未验证/风险；独立复核后才报告实现完成。

## 最新范围确认

用户要求仅处理 Emby API，列表/详情/继续观看均 5 分钟。Web 读取缓存及 Web handler 不改；共享服务的必要 Emby 失效允许。Emby Resume 和 IsResumable 纳入范围，Web 继续观看不纳入。Web 直接 DB 删除历史缺少即时通知，接受与否需在实施前明确，不得静默扩大 Web 修改范围或声称该路径已完全覆盖。


## 2026-10-10 实施记录

### 已实现

- Items 公共包装层保存前瞻裁切与收藏归属补全后的最终响应，移除 mediaItems 的内层旧缓存。
- HTTP 详情使用 CachedItem、Resume 使用 CachedResumeItemsPage；统一 300 秒和 1 MiB 门槛，随机与独立 SearchHints 绕过，内部 Item/Resume 算法不变。
- v2 结构化键保留 IDs 顺序和重复，包含仓储实例/权限版本、公共内容代数、用户状态代数、完整参数和认证可见范围。
- JSON 字节快照每次独立解码，嵌套 UseNumber 保精度；凭据仍在 HTTP 层最后注入。绝对过期时间约束 Redis 到 L1 的提升，防止短暂超过五分钟。
- PlaybackService 共享写端注入缓存，成功进度、收藏、有效历史删除仅通知当前用户；Emby 转调不重复通知，直接状态写和 CAS 各自通知。
- 补齐 NFO 编辑、根删除部分提交、来源详情/合集/绑定、刮削元数据/演职员、人物译名及 TMDB 标识失效通知。
- 保留前序搜索/人物关联只支持 Movie/Series/Person 的未提交改动，并将不支持类型的快速返回放到缓存身份查询之前。

### 独立审查核验

读链与写链分别只读复核。确认 nil cache receiver 已由统一方法保护，普通 progress 外层 changed=false 不与 saveProgress 重复通知；各来源和元数据阶段的独立事务已提交后，即使后续失败仍需通知。库封面 UpdateFields 已推进同一仓储 ReadCacheKey，响应键自动换代，无须重复清缓存。SourceArtwork LocalKey 不改变现有 Emby ImageTags 标识，因此不为纯图片下载重试添加通知。

### 验证状态

在独立 PostgreSQL 15 schema 及临时 Redis 7 容器验证，不使用业务数据库或现有 Redis 数据。新增测试覆盖最终页冷热 JSON 一致、SQL 查询归零、前瞻/准确计数/ID 顺序重复/收藏归属、四来源浏览、详情与 Resume、用户状态隔离、事务回滚/零行删除/短进度、人物译名与演职员部分提交、版本变化阻止旧回填、大整数与对象独立、取消/过期/超限/nil 详情、随机/SearchHints 绕过、认证快照、仓储权限版本、HTTP 字幕凭据逐请求注入及 Redis L1/L2/删除失败隔离。

验证结果：

- 新增缓存测试及已有类型快返/剧季状态/来源部分提交测试在真实 PostgreSQL + Redis 环境下 `-race` 通过；最大 500/501 前瞻另行 `-race` 通过。
- `go vet ./internal/service ./internal/handler` 与 `git diff --check` 通过。
- 接口层全包回归通过（95.097 秒）。服务层全包回归跑到 20 分钟上限，停在 Telegram 测试；中断后的 71 个顶层用例单独续跑全部通过（59.217 秒）。
- 服务层出现四项既有失败，已在独立 `bc0ce96` HEAD 工作树运行相同四用例，均复现：`TestEmbySeriesPaginationDoesNotProbeFilesForWholeCatalog`（总数字面 int 断言）、`TestHongGuoEmbyPlayableIdentityAndUserState`（旧来源人物搜索预期）、`TestHongGuoImportTaskAndIsolation`（旧任务定义数量 5，与现有 6 不符）、`TestLibraryMetadataPaginationBoundsFileReads`（混合页的旧排序/文件读取预期）。本次不扩大范围修改既有失败。

初次旧测试对嵌套 int 类型断言失败，已改为比较 JSON 数值契约；对外 JSON 不变，不能恢复浮点数而损失整数精度。旧缓存值结构移至测试文件，生产不保留仅为旧断言服务的类型。

### 边界

未部署、未生产压测、未使用实际播放器验收；仅单后端实例。已确认的 Web watch_history 两个直接删除 handler 未改，Emby 最多等五分钟 TTL 刷新。本轮按用户授权提交归档。临时数据库、Redis 与日志已关闭删除。
