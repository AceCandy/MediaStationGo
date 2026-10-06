# Execution plan

用户最终设计确认及实施授权：2026-10-06。主代理实现，子代理仅探索/独立复核。

- [x] 创建任务；持久化已批准 PRD/design/执行计划。
- [x] 主项目 backend/frontend/consumer 注册及复用点探索。
- [x] 0 协议验证：四分类/搜索/三个榜单/详情与正集号/匿名完整 VOD+AES+合并解码；镜像严格验证。
- [x] 1 独立 15 表/迁移/资料 repository；公网受限协议客户端；同步/图片任务及生命周期。
- [x] 2 API 与发现/详情，URL/账号切换/权限/榜单一致性。
- [x] 3 独立下载队列/租约/取消/校验/发布与下载空间来源选择。
- [x] 4 文件扫描标签/绑定/MediaView/收藏进度/统计，精准拒绝旧metadata写入。
- [x] 5 Emby 层级/全局列表/搜索/Latest/Resume/NextUp/图片/播放回写。
- [x] 6 隔离 PostgreSQL 完整迁移和行为测试、大样本计划；Go tests/race；Web lint/build/浏览器；独立复核。
- [x] 7 更新规格与使用说明，关闭调试服务、清理临时产物，报告未验证项。

## Current evidence

2026-10-06：7833 第 1 集真实下载 27 个 AES-128 HLS 分片，154.48 秒，完整解码通过；第 10 集清单 165.60 秒/文件 165.65 秒、完结单集 541 清单 271.83 秒/文件 271.88 秒亦下载解码通过；未完结换脸样本 219 首集 VOD 1268.47 秒/文件 1268.48 秒也完整下载解码通过。794 未确认第 2 集取流拒绝。测试自动删除真实临时媒体，临时在线验收代码已删除。搜索分页和三榜读取通过；候选镜像固定作品验证未通过，主站优先，没有有效备用域名。

阶段 0 保留未全覆盖：没有全站逐集、实机播放器/实际海报或全部上游失败组合验收；不把抽验成功写成全站保证。自动下载补集不扩展，管理员手动入队。第 6 阶段定向/竞态/大样本/浏览器已通过，完整 PostgreSQL 回归已结束：五组旧断言失败均在干净 HEAD 复现，无新增黄果失败，详见 validation.md。第 7 阶段规格、使用说明、服务关闭和临时清理完成。勾选表示实施/验证已执行，不表示上述既有失败或部署实机验收已经消除。

## Independent review

本轮独立后端/Web复核发现停用后扫描新绑定和电影整理清零坐标，主代理已修复并补测试；第二轮只读复核确认 completed 前集保留、停用 binding 保留、整理类型/路径保护、Season 字段。前端混合库路径已由合成浏览器实际验证。没有将未运行的代理审查当成测试结果。

## Validation
使用现有 `MEDIASTATION_TEST_POSTGRES_DSN` 或启动可清理的隔离 PostgreSQL 测试实例；不得运行生产迁移当测试。Go按受影响包定向测试再集成与race；Web `npm run lint`、`npm run build`；`git diff --check`。源站海报不下载，合成图片覆盖验证。

## 2026-10-06 Web parity follow-up

用户明确授权“改吧”：对齐黄果 AI 与红果的发现卡片、筛选布局、滚动续载及下载空间交互。修改边界为两个黄果页面、下载公共状态/进度展示、来源 API 类型及两个现有分页投影。沿用现有表与队列；仅批量补充 completed 下载标记及当前页四个处理阶段计数。保留四分类固定类型、三榜单、官网分页搜索及取消能力。不引入红果特有来源优先级、画质配置、人物/合集或插件架构。

验证：发现失败页原页重试/去重/取消旧请求；下载只在展开时读取当前页分集、可见时轮询、已知/未知大小进度及作品/分集操作；合成响应式暗亮主题；Go 两个投影测试、Web lint/build、红果下载回归。

## Ranking list presentation (2026-10-06)

- User requested screenshot-style vertical rankings for HongGuo and HuangGuo AI: 20 items per page, animated poster/synopsis disclosure on hover.
- Scope: the two catalog pages, their existing list API page-size arguments, shared `CatalogRanking` presentation/CSS, and synthetic browser checks. No backend/schema changes. Category/search grids retain 50-item incremental loading.
- Use URL pagination for rankings, replace rather than append pages, keep ordinal numbering continuous, preserve detail dialogs and HongGuo administrator selection. Header uses existing rank names; do not fabricate upstream heat counts or update schedules.
- Verify page sizes/order, no scroll prefetch, last-page bounds, retry, filters, dialog preservation, keyboard disclosure, reduced motion, artwork fallback and responsive dark/light themes. Run lint/build and independent review.

## Unified discovery search

User approved removing the search-scope selector and manual tag input. Search reads local (50/page) and official (source paging) concurrently, merges by source ID in official-first order with hydrated metadata precedence and combined download badges. Independent page/error/retry state retains successful results and retries only the failed source; explicit refresh resets both to page one. Legacy mode/tag query parameters normalize away; search ignores category/rank filters. Keep category and 20-item ranking presentation unchanged. Scope: HuangGuoAIPage, synthetic search/browser checks and loading specs; reuse existing backend APIs without schema changes. Verify overlap, metadata precedence, both paginations, independent failure/retry and abort on keyword/account changes.
