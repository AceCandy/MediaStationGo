# 验证与交接

## 最终收尾

用户已批准提交与归档，代码提交为 `ed51a23`。提交前差异及暂存区检查通过，无额外业务改动；沿用下列已完成的测试结果。本轮未推送、未部署或重启生产服务，真机验证与数据库回滚风险仍适用。下文“未提交”记录描述当时验收状态。

## 实现

- 合集收藏直接保存 `hg-group-<albumID>`；电影/待归组作品保存稳定源 ID。Web、Emby、详情、搜索、收藏筛选和个人收藏页共用新表。
- 旧 episode=0 收藏在启动事务中合并迁移；播放进度和事件不变。已有新表取消状态优先，重复迁移不复活。
- 补齐合集或电影转为已归组剧集时原子提升源收藏；新季自动继承，合集收藏不随单个成员改组移动。
- 收藏定位和收藏列表成员定位均使用原生索引条件，无需为写收藏展开全库节点。

## 已执行

- 独立临时 PostgreSQL 17，真实执行隔离 schema 测试；未使用或写入生产数据库跑测试。
- database / repository / handler 的 `Test.*(HongGuo|Favorite|Favourit|Resume|Continuation|PlaybackState)` 回归通过。
- service 的同组回归通过（大型计划测试单独执行，其余回归 116.599 秒）；`TestHongGuoLibraryPagePlan` 96.292 秒通过，覆盖 60 万文件下的收藏写入、Web 我的收藏和既有分页/详情计划；当时未覆盖全局收藏准确计数。
- `TestHongGuoAlbumFavoriteIdentity` 验证单行/幂等、新季、原季文件移除、用户隔离、不可见/锁定拒绝、源身份提升及电影转剧集。
- `TestMigrateHongGuoFavorites` 验证同合集合并、孤立源、电影、重复执行、取消不复活与播放状态保留。
- `TestHongGuoHTTPAccessAndStateIsolation` 验证 Web 收藏在 Emby 可见、Emby 取消后 Web 读到取消。
- `go vet`（database / repository / service / handler）、Web lint/build、`git diff --check` 通过。
- `node scripts/check-nextup.mjs`（在 web 目录执行）通过：收藏目录说明、搜索/筛选/复制、三尺寸、明暗主题可访问性及管理员访问；预览与浏览器已关闭。
- 生产库仅执行新目标定位 SELECT 的只读 EXPLAIN：合集索引命中 7 个成员，找到首个可见文件即停止，执行 0.708 ms、计划 8.756 ms。不是完整 HTTP 请求耗时，不包含生产写入验证。
- 三次独立只读复核已完成。未采纳“缺失作品必须报错/无文件仍需展示收藏”的建议：源收藏保留与无可见文件隐藏均为既有契约，新增回归已覆盖。成员定位的原生索引条件已补齐。
- 临时测试容器/数据库已删除，浏览器和预览服务已关闭；本次 Web 构建产物及类型检查缓存已清理，可由构建命令重新生成。

## 未验证与发布风险

- 未部署、未重启运行实例、未运行生产迁移、未重放用户收藏请求，未验证真实播放器点击。
- 上线前备份数据库，通过正常启动迁移升级。升级会删除已迁移的旧 episode=0 行；降级需要恢复备份或显式反向迁移，不能只退二进制。
- 工作树尚未提交，按 finish-work 前置条件不执行自动归档或自动提交。

## Bug Analysis

1. 根因类别：跨层身份契约与性能覆盖缺口。展示按合集，写收藏却按当时的可见来源成员，并复用全库节点查询。
2. 纠偏：不是放宽超时或仅给现有节点 SQL 加过滤；收藏本身必须以合集为主体。扩展计划测试曾把本合集标题成员读取误判为无关扫描，现已按真实成员上限检查。
3. 预防：统一收藏身份、单行 upsert、迁移/跨端/新季回归、真实大数据执行计划检查。
4. 同类消费：已同步层级详情、库分页、全局候选/批次、搜索、续播收藏过滤与个人收藏页；未扩大到已看实现。
5. 知识沉淀：更新 `hongguo-catalog.md` 的存储、迁移、可见性及性能契约，并同步 Emby 接口目录。

## 全局收藏列表性能补充（2026-09-29）

- 生产日志：YAMBy Series + IsFavorite + DateLastContentAdded,SortName + EnableTotalRecordCount=true 两次耗时 4624/4118 ms；收藏写入为 204 ms。旧计数 SQL 只读 EXPLAIN 执行 3765.366 ms，其中 JIT 3554.342 ms。
- 实现仅在 `emby_hongguo_library.go` 增加收藏前置约束：原生 source/album 索引定位、收藏合集标题/时间范围、收藏文件日期的关联聚合。未改变响应、收藏存储、权限、普通/NFO 路径或数据库设置，无需更新接口目录。
- 新计划回归先在旧代码失败：1 个收藏、10,000 作品/600,000 文件，计数读取 20,001 行作品，JIT 216 functions，执行 113.664 ms。
- 同规模修复后：最近添加计数/取页 0.646/0.695 ms，各访问 7 行作品、1 行文件；标题排序 0.720/0.796 ms；文件日期排序 1.678/1.780 ms，各访问 301 行文件。均无 JIT。完整 `TestHongGuoLibraryPagePlan` 90.248 秒通过。
- 生产只读 SQL 对照：在原日志查询中应用与代码相同的两个收藏范围约束，执行 2.290 ms、规划 10.464 ms，无 JIT；新旧 COUNT 均为 2。该结果是 SELECT/EXPLAIN 对照，不是部署后 HTTP 或客户端耗时。
- 专用临时 PostgreSQL 17 的 9 项 service 回归通过（61.180 秒）：`TestHongGuoAlbumFavoriteIdentity`、`TestHongGuoLibraryPageMatchesHierarchy`、`TestEmbyGlobalBrowseCandidateBatches`、`TestEmbyGlobalBatchMixedSourcesRefill`、`TestEmbyResumeSourcesGroupBeforeMerge`、`TestEmbyItemsCountModes`、`TestEmbyWorkBatchContinuesAndCounts`、`TestHongGuoListsWaitForAlbumSupplement`、`TestHongGuoLibraryPagingAndLatest`。覆盖取消/新季/用户隔离、隐藏/锁定、电影源收藏、全局排序/分页/总数和原层级对照。
- `go vet ./internal/service` 与 `git diff --check` 通过。独立只读复核完成；“取消收藏仍参与 album 聚合”的疑虑经核验不成立：子查询含 `AND favorite`，取消收藏回归通过。
- 未改 Web/HTTP 契约，未重复 Web 构建和浏览器测试；未做全仓测试、生产部署或 YAMBy 真机复测。非收藏全局查询的已有 JIT 不在本次改动范围。
- 临时 PostgreSQL 容器已停止并自动移除，隔离测试数据已删除，可由测试重新生成；未留下本轮服务或调试文件。工作树保留未提交，finish-work 前置条件未满足，不自动提交/归档。

### Bug Analysis: 全局收藏计数遗漏

1. 根因类别：D（覆盖缺口）与 E（隐含假设）。把收藏写入、Web 卡片和全局未看列表的计划验证误当成收藏读取已全面覆盖；SQL 外层收藏过滤不保证先按收藏缩小候选。
2. 先前不足：收藏身份已修正，但全局计数仍走全量合集聚合，估算膨胀触发 JIT；只检查文件访问量无法发现编译成本。
3. 预防：P0 已完成实际 Items 精确计数/取页捕获，并断言稀疏收藏下作品访问和 JIT；保留旧层级 oracle。
4. 系统性边界：普通/NFO 的资格和总数不改；同类验证应按真实筛选/排序/计数组合区分，不以客户端名称或单一接口成绩代替。
5. 知识沉淀：已在 `hongguo-catalog.md` 收藏契约补充全局计数的候选边界与回归要求。
