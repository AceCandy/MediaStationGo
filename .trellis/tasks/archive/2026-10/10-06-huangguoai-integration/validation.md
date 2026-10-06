# 验证记录（2026-10-06）

## 自动化

- 隔离 PostgreSQL 16，`MEDIASTATION_TEST_POSTGRES_DSN` 指向临时容器；测试各自隔离 schema，不使用生产数据库。
- 定向 `go test -race ./internal/service ./internal/repository ./internal/handler ./internal/huangguoai`：`TestHuangGuoAI*`、来源分类/详情/HLS/榜单/搜索及任务数量断言通过。最后一轮 service 41.204s、repository 13.818s、handler 4.909s、来源 1.021s。
- `TestHuangGuoAIWorkPagePlanAndExactCounts`：2,000 作品/4,000 分集，Emby库页准确计数/页内容 EXPLAIN 扫描量检查；全局 Items、Latest 与 Web第2页行为通过。没有覆盖所有过滤组合/generic prepared plans。
- `TestHuangGuoAIHTTPAdultAndProfileBoundary`：管理员允许访问、profile关闭、全局关闭、直接详情/图片/收藏状态/下载/刷新入口边界通过。
- 合成浏览器 `node web/scripts/check-huangguoai.mjs` 通过。修正过模拟 route 匹配顺序；不是来源网站或产品接口运行错误。
- Web lint/build通过；合成详情页面暗亮主题截图已人工检视，390/640/768/1024/1440宽度无横向溢出。
- `git diff --check`通过。
- 全量 PostgreSQL `go test ./internal/... ./cmd/...` 的首次 service 运行触发默认 10 分钟超时，其余包通过。临时 PostgreSQL 关闭 fsync/synchronous_commit/full_page_writes 后，延长至 30 分钟重跑 service（不测试断电持久性）：842.857 秒执行结束，共 1,724 个测试/子测试事件，五组既有断言失败，未出现新黄果失败。
- 重跑发现 `TestBackgroundPendingChecksUseIndexes/probe` 严格计划索引名断言失败；在干净 HEAD 54db963 的独立临时 worktree、同一隔离 PostgreSQL 重跑同一测试，同样失败，证明该项不是本次改动引入。临时 worktree 首次移除后，又用于复测 `TestHongGuoPageNodesPreservePayloadsAndBoundWorkReads`，该红果页内 hydration 扫描量断言在干净 HEAD 也失败；两项均为原有计划测试问题，最终清理时移除 worktree。

## 网络抽验

仅文本/媒体抽验，不下载、查看或分析来源海报。7833 第1集：27个AES128 VOD分片，154.48秒完整解码；第10集清单165.60秒/文件165.65秒；明确完结单集541清单271.83秒/文件271.88秒，后二者亦完整解码。未完结 AI 换脸样本 219 第1集：来源时长1268.00秒/VOD1268.47秒/文件1268.48秒，完整解码通过。794第2集 Resolve 拒绝，未生成假集。

临时在线验收代码已删除，测试媒体通过 t.TempDir 自动删除。不保存页面HTML、媒体签名地址或密钥。候选镜像未通过固定作品测试，没有可宣称的有效镜像。

## 独立复核

只读后端/Web及修补后复核完成。主代理修复并测试：停用仍新增绑定、电影整理清零坐标、已完成前集状态被自动补记覆盖、混合库作品列表加载。Season payload作品名/季名明确，并补集成断言。

## 未验证/限制

用户实际部署、Android模拟器、第三方Emby实机、真实海报、全站全分集、全部上游失败/登录/付费/镜像情况未验收。没有自动下载补集、远程来源直播或电影额外分集的下载/播放扩展。裸旧版本二进制降级兼容未验证；回退应停用来源保留表和文件。


## 完整回归的既有失败

下列五组在本工作树和干净 HEAD 54db963 独立 worktree、同一 PostgreSQL 环境均失败，未修改相关旧逻辑或放宽断言：

1. `TestBackgroundPendingChecksUseIndexes/probe`：严格索引名断言。
2. `TestHongGuoPageNodesPreservePayloadsAndBoundWorkReads`：旧红果页内 hydration 扫描量。
3. `TestHongGuoDownloadWorkPagePlan`（含状态子测试）：旧红果分集计划全扫断言。
4. `TestHongGuoEmbyPlayableIdentityAndUserState`：旧全局 Latest 期待的展示身份。
5. `TestLibraryMetadataPaginationBoundsFileReads`：旧普通混合库第一页排序/文件读取预期。

因此不能声称全量回归全绿；本次源码接入、专项验证和边界复核已完成。旧库数据未用于测试，未运行生产迁移/部署/提交。

## 收尾

前端 Vite 已关闭，浏览器脚本 finally 关闭会话；测试容器最终停止且自动删除。临时截图、真实媒体、在线验收代码、测试 JSON 日志和基线 worktree 已清理。最终 `git diff --check` 通过。

## Web 对齐增量（2026-10-06）

- 已完成两个独立只读复核：列表/官网搜索/详情共用下载标记；下载汇总先分页作品再统计全部分集。回退分集页时丢弃越界响应。
- 隔离 PostgreSQL 两个新增定向测试通过：`TestHuangGuoAIListDownloadedBadge`、`TestHuangGuoAIDownloadWorkStatusCounts`。未再次执行耗时全量后端测试，既有全量失败仍见上文。
- 合成 Web 检查覆盖续载同页重试、去重、刷新回到起始页、切筛选取消旧响应、详情保留已加载列表；下载当前页展开/分页、已知和未知大小进度、四个操作端点及反馈、设置焦点返回、隐藏暂停/恢复及收起停止轮询。
- 红果 `check-download-space-polling.mjs` 通过：隐藏暂停、即时恢复、单请求及收起清理。
- 原有 `check-download-space.mjs` 在旧“仅显示含失败集的剧集”复选框断言处失败；干净 HEAD 页面已经使用“整剧状态” Select，该脚本断言过时。本轮没有修改该旧脚本或将结果宣称为通过。
- 合成发现、详情、下载的暗亮主题及390/640/768/1024/1440宽度检查；截图复核发现窄屏搜索与作品标题被挤压，改为窄屏独立一行并增加宽度回归断言。不访问真实海报。

- 最终增量 lint/build（无告警）、合成浏览器与 diff 检查通过；手机搜索框和作品标题宽度断言通过，桌面/手机截图已复核。Vite、浏览器会话、隔离 PostgreSQL 已关闭，临时截图与本轮调试日志已删除。未提交、部署或访问生产库。


## Catalog ranking list presentation (2026-10-06)

- HongGuo/HuangGuo AI share vertical ranking header/rows and 20-item URL pagination. CSS reveals poster/synopsis on hover or keyboard focus and respects reduced motion. Rank switches reset page; page switches scroll to the header. Category/search retain 50-item grids and their previous loading behavior. No backend or schema change.
- PASS: `npm run lint`, `npm run build`, `git diff --check`.
- PASS: `node scripts/check-catalog-rankings.mjs` from `web`: 43 synthetic works as 20/20/3, ordinal continuity, explicit pagination/no prefetch, failed-page retry, rank switching and ignored obsolete response, detail preservation, HongGuo admin selection/pending restrictions, category regression, broken-image fallback, hover/focus and reduced motion, dark/light widths 390/639/640/768/1440.
- PASS: existing `check-hongguo-discover.mjs`, `check-hongguo-search.mjs`, and `check-huangguoai.mjs`. Existing selector updated from rank dropdown to rank buttons; HuangGuo rank mock now supplies 20 items.
- Independently reviewed the presentation/pages/API arguments; no UI blocker found. Reviewer caught a synthetic route pattern/query-order mismatch; corrected the API parameter assembly to preserve existing serialization order. Synthetic detail routes must be registered before the detail-prefix mock, otherwise the browser route matcher returns a work object to episode/media requests. No application detail change was needed.
- Screenshot inspection found that overflow checks alone missed a squeezed mobile heading. Shared header copy now occupies its own row below desktop widths; added heading width/height assertions and re-ran all ranking checks. Final dark/light mobile and desktop screenshots personally inspected, using only synthetic data/placeholders.
- No live-source poster, production database, deployment or full Go suite verification in this UI task. Upstream missing posters/synopses use placeholders. Temporary screenshots/scripts cleaned and local Vite stopped after verification; changes remain uncommitted.


## Unified HuangGuo AI discovery search

- Removed search-scope selection and manual tag filtering; tags still display on work cards. Legacy mode/tag query values normalize away. Keyword search starts local and official reads at page one, ignores category/rank restrictions, deduplicates source IDs and retains hydrated metadata plus download flags. Backend APIs/schema are unchanged.
- PASS: Web lint/build; `check-huangguoai-search.mjs`, `check-huangguoai.mjs`, `check-catalog-rankings.mjs`. Synthetic search covers 50 local + 24 official ->72 distinct initial works, both page-two responses ->75, official/local failures and source-only retry, refreshed cursors, ignored obsolete keyword response, empty results, URL canonicalization and dark/light responsive widths. Existing checks cover detail retention, catalog switch cancellation, downloads, mixed libraries and 20-item ranking pages.
- Independent read-only review found no blocking issue. Account/profile changes are covered structurally by the existing access-key remount/controller cleanup, not by a new account-switch browser test.
- No live upstream search/image, production database, deployment or Go suite verification for this frontend-only change. Source availability remains external; the UI preserves local/previous results on official failure. Changes remain uncommitted.

- Final search regression also verifies continued local pagination while the official source is unavailable, followed by official-only retry. Dark mobile and light desktop screenshots personally inspected with synthetic placeholders; screenshot files and temporary Vite log removed, and Vite closed.

## 提交归档授权

2026-10-06：用户明确要求提交并归档。上述未提交说明为当轮验证时的历史状态；本次归档保留所有既有测试限制与未部署边界。没有新增源码实现或重复全量回归；提交前范围独立核对和 git diff --check 通过。

源码与规范已提交：`e0af2aa`。本次暂存检查修正一处新增 TypeScript 文件末尾多余空行，未改变运行行为。
