# 验证记录

## 真实数据只读测量

- 使用运行服务的数据库与已有 Yamby 日志参数，直接调用 `EmbyService.Items`；开启数据库只读模式和 15 秒查询超时，未启用响应结果缓存。
- 参数：Series、Recursive=true、Limit=30、StartIndex=0、SortBy=DateLastContentAdded,SortName、Descending；Fields 与日志一致。
- 修改前三次完整服务耗时：1681ms、1624ms、1611ms；计数约 798–846ms，分页约 798–809ms。
- 修改后三次完整服务耗时：241ms、214ms、217ms；计数约 97–105ms，分页约 101–103ms，当前页摘要约 8–11ms。
- 中位数从 1624ms 降至 217ms，约快 7.5 倍。两组完整 JSON 响应校验一致：总数 895、当前页 30 条。
- 执行计划确认旧查询先遍历约 15.3 万条目录分集，再逐条探测媒体；新查询先处理库内约 1.97 万个有效文件关联。

## 回归与复核

- 新测试使用独立 PostgreSQL 15 实例和隔离 schema，2 万条目录分集、4000 个文件、20 部可播放剧集。
- 将测试中的执行计划查询临时恢复旧连接后，两段 SQL 均触发 20000 次文件探测断言失败；恢复修复后通过。不使用机器耗时阈值。
- 独立静态审查确认两个调用方、GORM Session 隔离、过滤传播、排序/分页及摘要范围无高/中严重性问题。
- 追加组合过滤回归发现收藏 JOIN 提前引用整剧别名；改用带用户及软删除条件的 EXISTS 后通过。静态审查不能代替执行所有带别名依赖的筛选分支，此规则已写入 spec。
- 7 项针对性 PostgreSQL service 测试通过，包含分页、整剧/季/分集、版本去重、图片归属和新执行计划回归；`go vet ./internal/service`、`git diff --check` 通过。
- Emby catalog 对照：路由、参数、响应及排序语义未变，无需修改前端目录。

## 边界

- 服务耗时不含 HTTP 认证中间件、网络传输、图片加载与 Yamby 渲染。
- 未重启或替换用户现有运行服务，未部署，未修改运行库数据、索引或全局规划器设置。
- 临时只读测量测试在验证后删除，不保留实际请求身份或数据库凭据。

## Web 扩展验证

- 同一实际媒体库，只读调用 `ListLibrarySeriesCards`，默认非 NSFW 筛选、第一页 50 张卡，不使用结果缓存。
- 优化前完整服务：554ms、524ms、508ms；优化后：374ms、356ms、352ms。中位数降低约 32%。
- 分页 SQL 从约 408–423ms 降至 237–256ms；计数和展示关联读取保留原实现。
- 两组卡片 JSON 校验值一致，总数均 895，当前页均 50 条。
- 扩展已有目录规模执行计划回归覆盖 Web 分页。service/handler 的 `TestEmbySeriesPagination|TestLibraryMetadataPagination|TestLibrarySeriesCards|TestListLibrarySeries|TestListMediaVisibleGrouped|TestMediaSeriesDetail` 在隔离 PostgreSQL 实例通过。
- 临时将 Web 执行计划查询恢复旧 FROM 后，20200 次文件探测触发回归断言失败；恢复后通过。独立只读审查及主线程差异复核未发现生产行为问题，仓储/service 静态检查通过。
- Web 与 Emby 复用数据模型/仓储能力，但列表语义不同：Web 支持直接整剧/季/分集归属、代表文件操作及缺失字段筛选；Emby 支持播放器排序、Fields、播放状态与层级摘要。本次不引入通用列表抽象。
