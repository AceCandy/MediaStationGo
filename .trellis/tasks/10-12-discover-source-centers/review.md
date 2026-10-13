# Review and Verification

Implementation and independent read-only review are complete. No commit,
archive, production database access or user-service restart was performed.

## Final Behavior

- Discover owns separate TMDb, Douban, HongGuo, HuangGuo AI and Bangumi branches.
  TMDb/Douban expose saved-data poster grids and their existing source rankings.
- Task Center owns common/media/tmdb/douban/hongguo/huangguoai systems. Local
  scan/scrape/artwork/people/translation/episode maintenance belongs to media;
  source supplement and periodic refresh belong to their source. Startup updates
  history classification without changing execution IDs, logs or timestamps.
- Both ranking paths persist source summaries without fetching every result.
  Opened missing details register durable, deduplicated source supplementation,
  sharing existing TTL, rate limits and backoff. Local titles, translations,
  credits and selected artwork remain untouched.
- TMDb reuses component jobs. Douban stores summary, API-credit success time and
  supplement intent/lease/token/retry on its subject table; no new queue/person
  table. Automatic Douban work calls only base/celebrities APIs.
- Ordinary Discover users can browse and supplement missing source data. Full
  refresh/import, management diagnostics and raw payloads remain administrator
  operations. Public details/ensure responses expose safe pending/error flags.
- Failed pagination retries the same page; sections errors stay explicit;
  same-account relogin isolates in-flight reads. Missing images retain cards.
  Shared ModalShell contains focus in the top dialog and restores its trigger.
- Both source dialogs stop polling after 45 two-second attempts, explain that
  background work continues and offer a manual reread.

## Independent Review Disposition

- Source boundaries, permissions, history mapping, queue quota/deduplication,
  leases/retry, shutdown and source-truth projections passed the final backend
  read-only review without a new material finding.
- A suggested older-retry batch starvation concern did not match the SQL:
  eligibility is filtered before LIMIT. The focused regression passes.
- Fixed cancellation reported as completed, and skip invisible worker batches
  when task execution creation fails. Tests verify interrupted status and durable
  intent/lease release.
- Fixed unbounded Douban polling and silent TMDb polling exhaustion, retaining
  bounded reads with status and manual reread. The TMDb regression accelerates
  only the two-second wait, asserts 46 reads, pause and one completed reread.
- The final frontend read-only review also covered routing, public/admin UI,
  session isolation, failed-page retry and nested/async modal focus. The shared
  focus fix captures the opener before child autofocus and StrictMode effects.

## Passed Checks

- Web lint with no warnings; TypeScript and production bundle build.
- Image URL helper check and git diff whitespace check.
- Isolated PostgreSQL focused handler/service/database tests covering TMDb
  center/details, Douban source/cache/rich/supplement, Discover permissions,
  task handlers/definitions, NFO task ownership and history classification.
- Latest backend focus includes DoubanRankingSummary, DoubanSupplement,
  DoubanRichRefreshPreserves, TMDbCenterSupplement, TMDbCenterPublic,
  DiscoverCatalogSupplement and TaskSystemHistory.
- Mock browser checks on isolated Vite: discover-centers, discover-detail,
  discover-boundaries, douban-data-center, hongguo-discover, huangguoai,
  task-log and task-startup. Latest shared-shell checks pass nested focus,
  Escape/return and both existing source flows.
- Coverage includes viewer/admin controls, source scoping, automatic missing
  Douban supplementation, series/season/episode/person works, URL/history,
  malformed/duplicate state, cancellation/auth switching, image placeholders,
  failed-page [1,2,2] retry, same-account relogin, source-section failure,
  task progress/logs and responsive light/dark layouts.

## Limits and Operation

- Browser APIs were mocked; production records, deployed login/Cookie integration
  and complete upstream end-to-end behavior were not exercised. No whole-repository
  or race suite was run. Focused backend checks use real isolated PostgreSQL.
- Public celebrities API verification for subject 36600459 returned one director
  and 37 actors with total 87; requesting count 100 returned the same arrays.
  The application stores all returned personnel/roles; it cannot promise all
  website credits while keeping automatic supplementation API-only.
- Normal backend rebuild/start activates schema fields, task-history grouping,
  catalog routes and workers. Web production assets have been built. Existing
  source schedules are preserved; source supplements do not edit local works.
- Temporary Vite, PostgreSQL, owned browser sessions and test logs are stopped or
  removed at completion. The pre-existing untracked core file remains untouched.

## 2026-10-13 分类与详情样式收尾

本轮新增持久化分类查询（TMDb 多题材/人物主要职业、豆瓣兼容格式题材），
用同一筛选条件计数与分页，不新增分类表、不请求上游分类。前端保留来源
各自游标、搜索和管理权限，统一分类标签、海报网格与编号榜单行。

TMDb 分类与榜单现复用 TMDbDetailModal：来源信息/评分区、圆形可点击人物、
同弹窗剧季集和人物作品导航。强刷/图片重试/履历刷新折叠到管理菜单；
普通作品履历浏览只读。当前详情按需 ensure，轮询限 45 次；关联读取失败
有独立同页重试，成功详情保留。无生产数据操作，无新增图片存储逻辑。

已完成独立只读 SQL 与前端审查，并由主线程复核疑点。允许观众登记缺项
ensure 属现行授权；有界图片重试后保留占位是预期行为。详情重试会重置
ensured 引用并重新读取，此路径追加浏览器回归。浏览器 URL 改变与延迟页面
切换不同时完成，跨源断言需等待前一来源卸载；分类切换后需等待布局稳定。

本轮验证：
- 独立 PostgreSQL 的 CatalogCategories、DoubanCategories、DiscoverCatalogReadPermissions
  聚焦测试通过（多题材去重、计数/分页、人物职业、豆瓣格式、注入字符串、权限）。
- Web lint、TypeScript 与生产构建通过。
- check-discover-centers、check-discover-detail、check-discover-boundaries、
  check-douban-data-center、check-catalog-rankings、check-hongguo-discover、
  check-huangguoai 通过；接口全部使用隔离模拟。
- 查看 TMDb 深浅主题桌面/手机截图；响应式检查覆盖 390–1440px。

限制：未连接生产资料库，未做真实上游抓取/下载与大数据量分类性能压测；
本轮不重启用户服务、不提交或归档。原有未提交工作和 core 保持原样。

最终补测：ensure 失败→读取失败→再次重试成功的观众恢复路径通过。
本轮独立 PostgreSQL、Vite 与测试浏览器均已关闭；临时截图、测试库与编辑脚本已删除。

## 2026-10-13 对齐审查收敛

六项已确认问题均已修复：TMDb 关联分页保留成功内容与响应页码，失败页可
原页重试；中心 ID 解析后的首次详情读取失败保留榜单摘要；榜单行骨架、
手动续载与结束提示统一；分类搜索/分页控件统一；豆瓣分类和榜单共用
管理员管理菜单与更新状态。截图复核另修复管理菜单展开后原页按钮被拉高。

独立只读审查 alignment_fixes_review 未发现新的明确功能或权限缺陷；
主线程复核共享组件、刷新取消、失败页请求序列，并补充回归断言。

验证通过：
- Web lint、TypeScript/生产构建、git diff --check。
- check-discover-detail：失败页请求序列 2→2→成功返回第一页、摘要保留、
  演职员/人物作品、轮询上限、权限和焦点恢复。
- check-discover-centers：五来源隔离、两入口豆瓣管理操作一致且仅走管理员
  更新接口、URL/取消、390–1440px 深浅主题；新增展开菜单按钮高度断言。
- check-discover-boundaries：失败续载不显示结束，成功末页显示结束，
  同账号会话隔离和嵌套弹窗焦点。
- check-douban-data-center、check-catalog-rankings、check-hongguo-search、
  check-huangguoai-search 全部通过。
- 人工查看分类搜索栏和豆瓣展开管理菜单的深浅主题手机/桌面截图。

测试修正：管理菜单新增 details 后，原始 JSON 测试必须定位诊断区域；
结束提示也使用 role=status，不能把任意 status 都判为加载中；指针翻页
须等按钮启用和布局稳定，实际请求页序列断言保持严格。

本轮只改前端及验证文档，浏览器使用模拟接口；未验证真实上游、生产数据
或大数据量性能。未操作生产数据库、重启用户服务、提交或归档。
临时 Vite/测试浏览器和截图、编辑脚本在完成时清理。
