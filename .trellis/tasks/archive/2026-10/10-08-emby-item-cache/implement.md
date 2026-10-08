# ITEM 缓存实施与验收计划

状态：未来实施计划。当前仅完成设计交付并按用户要求归档；未执行 task.py start 或以下实现步骤。

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

本轮仅设计，不跑 Go 编译/测试、不连接数据库/Redis、不启动服务。

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
