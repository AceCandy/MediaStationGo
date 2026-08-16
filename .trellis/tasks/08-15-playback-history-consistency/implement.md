# 实施计划

## 成功标准

权限、完成判定、20 秒门槛、历史写入、Resume 和播放统计在 Web/Emby 两条链路保持一致；管理员可在“观看空间”查看可筛选的播放统计，普通账户无法访问。

## 实施顺序

1. 统一领域规则与权限边界
   - 增加共享的进度校验、完成判定和目标账户校验。
   - 让历史、收藏和 Emby UserData 入口遵守自助/管理员显式目标规则。
   - 固定 `can_view_history=true` 的兼容投影并移除 Web 开关。
   - 验证：逐一盘点历史、收藏和 UserData 的显式账户路由，对每个入口覆盖本人、普通跨账户 403、管理员显式目标三种情况；补齐 10 分钟边界和非法输入测试。

2. 修正历史写入与手动状态
   - 将历史 Upsert 改为 PostgreSQL `ON CONFLICT DO UPDATE`。
   - 自动进度统一应用 20 秒门槛和媒体可见性。
   - 手动已看直接完成；手动未看删除历史且不触碰播放事件。
   - 验证：并发首次写入、不可见媒体、已看/未看和百分比边界测试。

3. 新增播放事件与会话传递
   - 增加事件模型、幂等迁移、唯一约束、repository 插入与聚合查询。
   - Web 播放实例生成并复用 UUID；Emby 读取并传递 `PlaySessionId`。
   - 达到 20 秒时在数据库中去重插入；手动操作不插入。
   - 按需同步 Emby API Catalog。
   - 验证：Playing/Progress/Stopped 及小写路由的会话传递与账户校验；同会话重复上报一条、新会话第二条、20 秒前零条、恰好 20 秒一条、缺会话只写历史、不可见媒体两者都不写。

4. 合并继续观看与批量 Resume
   - Web 首页和 Emby Resume 共用播放服务规则。
   - 将媒体、版本和 UserData 改为批量查询并保持历史排序。
   - 验证：两入口集合/顺序一致，20 秒门槛、完成过滤、可见性和批量结果测试；用仓库调用计数或 SQL 记录断言结果规模增加时查询次数不线性增长。

5. 加固 Web 最终进度与历史缓存
   - pause/ended flush，离页 keepalive，存活期失败最多重试一次。
   - 历史和继续观看增加账户级 in-flight 去重、短 TTL 缓存与变更失效。
   - 验证：重复挂载只发一个请求，账户切换不串缓存，写入/删除后刷新；pagehide 请求携带现有认证与 keepalive，最终进度事件不产生无界重试。

6. 增加管理员统计 API 与 Web 页面
   - 在 `/api/admin` 增加按粒度、范围、账户、类型和多库筛选的统计接口。
   - 增加 `/playback-stats` 管理员路由，挂入“观看空间”并排在“我的”和“播放器日志”之间。
   - 复用现有用户/媒体库 API 和原生表单，以 CSS 条形与表格展示。
   - 验证：筛选组合、空结果、非法日期/粒度/范围、重复库 ID、普通用户 403、路由直接访问、导航可见性与响应式布局。

7. 修正作品列表、收藏与详情边界
   - 最近添加改为数据库按逻辑作品聚合、排序和限制数量，删除 50,000 条媒体内存归组。
   - 首页删除媒体库请求，保留最近添加与继续观看并行加载。
   - 收藏按 `metadata_id` 批量加载作品卡片，移除逐条媒体版本解析。
   - 卡片和详情改用 metadata ID；详情批量加载可见版本，并以仍可见的上次播放 media ID 作为默认选择，播放时才进入 media ID 路由。
   - 盘点并修正 Emby/Jellyfin Latest、Items、Resume、单项详情和 PlaybackInfo 的同类分页、N+1 与过早版本选择问题。
   - 验证：最近添加排序/数量/归组测试，收藏查询规模测试，详情默认版本回退测试，以及播放器 API 列表分页和批量加载测试。

8. 独立复核与回归
   - 逐条对照 PRD Acceptance Criteria，检查 Web/Emby 两条数据流。
   - 检查迁移幂等、事务边界、唯一约束和旧客户端兼容字段。
   - 确认没有新增依赖、临时文件、敏感日志或无关 diff。

## 最小验证命令

```bash
go test ./internal/service ./internal/handler -run 'Playback|playback|Resume|History'
MEDIASTATION_TEST_POSTGRES_DSN='<postgres test dsn>' go test ./internal/database ./internal/repository -run 'Playback|History|Migration|Schema'
cd web && npm run lint && npm run build
git diff --check
```

全量 `go vet ./...`、`go test ./...` 和 PostgreSQL 集成测试会在实施结束前说明原因并按项目 Java/编译之外的验证约束执行；若环境没有测试 DSN，明确记录数据库测试被跳过，不以 SQLite 替代。

## 高风险与回滚点

- 权限：目标账户解析必须先覆盖所有调用入口，再替换旧判断；失败时可按入口回滚，不改数据。
- 数据库：事件迁移和历史 Upsert 必须用 PostgreSQL 真实约束验证；事件表无历史回填，回滚无需删除表。
- 最终进度：keepalive 请求继续使用现有认证格式；若浏览器限制导致失败，保留 pause/ended 正常提交路径。
- 缓存：仅做进程内前端短缓存；发现一致性问题可删除缓存层，不影响后端协议。
- 统计性能：本次使用索引加按需聚合；只有真实数据证明不足时才考虑预聚合。
- 作品查询：可见性过滤必须发生在作品分页前；若聚合查询回归，可回滚到原服务方法而不迁移数据。

## 启动前复核

- [ ] PRD、设计与实施计划无未决产品问题。
- [ ] `implement.jsonl` 与 `check.jsonl` 包含真实 spec/research 上下文。
- [ ] 用户在最终规划摘要之后明确批准实施。
- [ ] 实施前加载 `trellis-before-dev`，完成后使用 `trellis-check` 独立复核。
