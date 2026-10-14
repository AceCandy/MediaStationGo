# Journal - AceCandy (Part 4)

> Continuation from `journal-3.md` (archived at ~2000 lines)
> Started: 2026-10-12

---



## Session 234: 红果异常分集隔离
<!-- trellis-session: v=2 fp=2d992e193f48b3dc -->

**Date**: 2026-10-12
**Task**: 红果异常分集隔离
**Branch**: `main`

### Summary

按当前集隔离上游列表换位，保留有效视频 ID、已完成文件和恢复检查点；完成独立审查与针对性回归，任务已归档。

### Git Commits

| Hash | Message |
|------|---------|
| `c2e48ae` | fix(hongguo): 隔离异常分集并保留下载视频身份 |

### Testing

- [OK] 针对性回归 12 个测试函数通过；加强实际模拟传输和完成文件保护后的核心 6 个测试函数通过；gofmt 与 git diff --check 通过。

### Status

[OK] **Completed**

### Next Steps

- 后端重新加载后生效；未重试生产任务，持续异常的分集仍需人工确认。


## Session 235: 红果按原视频 ID 手动下载
<!-- trellis-session: v=2 fp=d76ff31ebab9658d -->

**Date**: 2026-10-12
**Task**: 红果按原视频 ID 手动下载
**Branch**: `main`

### Summary

失败分集详情新增确认后按原视频 ID 下载，持久化手动选择并保持普通重试核验、恢复和分集清理保护；补齐换源请求身份与旧表迁移验证，任务已归档。

### Git Commits

| Hash | Message |
|------|---------|
| `c9bb4f3` | feat(hongguo): 支持按原视频 ID 手动下载失败分集 |

### Testing

- [OK] 后端针对性回归、隔离 PostgreSQL 迁移测试、前端 lint/build 和浏览器确认/取消及响应式验证通过；完成独立审查和主线程复核。

### Status

[OK] **Completed**

### Next Steps

- 更新前后端后使用失败集详情中的按原视频 ID 下载；未操作生产队列、未重启服务、未验证真实剧情顺序。


## Session 236: 资料中心历史依赖退役
<!-- trellis-session: v=2 fp=2428017b49f62363 -->

**Date**: 2026-10-12
**Task**: 资料中心历史依赖退役
**Branch**: `main`

### Summary

完成TMDb来源保存优化并退役旧快照、迁移失败队列和全库回填。部署后只读确认旧表与设置已清除；专项真实PostgreSQL及race验证已有记录，提交前全包编译、前端类型和diff检查再次通过。后续全项目历史清理另建任务。

### Git Commits

| Hash | Message |
|------|---------|
| `e727fbc` | refactor(metadata): 退役旧快照和历史回填依赖 |

### Status

[OK] **Completed**


## Session 237: 项目历史依赖清理完成
<!-- trellis-session: v=2 fp=b166aca80abc1507 -->

**Date**: 2026-10-12
**Task**: 项目历史依赖清理完成
**Branch**: `main`

### Summary

完成重复 API 配置、历史迁移和图片键、任务别名、旧页面链接及日志格式清理；定向数据库清理及用户最终重启复查通过。任务已归档，保留原有 core，未推送远端。

### Main Changes

- 统一当前 API 配置链和现行 schema，退役历史转换与旧入口
- 来源图片严格使用 catalogs 键，保留海报、头像、旁置缩略图和缺图重试
- 移除旧任务归属与 Web 链接/存储/日志兼容，同步规范和当前回归

### Git Commits

| Hash | Message |
|------|---------|
| `576d4d1` | refactor: 清理项目历史依赖与兼容入口 |

### Testing

- [OK] 全包编译、受影响 PostgreSQL 专项及 race、go vet 通过
- [OK] Web lint/build、资料中心/发现/任务/媒体库专项检查通过
- [OK] SQL 隔离回滚保护验证通过；用户执行后确认旧表、149 条退役任务和两列旧字段清除
- [OK] 用户最终重启健康接口200、旧API404，旧结构与图片键未重建；完整登录态端到端未验证

### Status

[OK] **Completed**


## Session 238: 历史残留清理与重启验收归档
<!-- trellis-session: v=2 fp=1ba8fe84a30969cd -->

**Date**: 2026-10-12
**Task**: 历史残留清理与重启验收归档
**Branch**: `main`

### Summary

清除媒体库旧结构回退、退役配置及调度入口、孤立事件票据、旧哈希深链和红果一次性整理工具；独立复核与用户重启后的只读验收通过，提交 main 并归档，不推送远端。

### Main Changes

- 目录读取、扫描、监听、文件授权、图片根和路径规范化只依赖真实 roots；保留主目录投影。
- 删除退役接口、事件票据、旧作品哈希解析和无候选数据的红果整理工具，同步测试与规范。

### Git Commits

| Hash | Message |
|------|---------|
| `d9bc408` | refactor: 清除目录回退与退役兼容入口 |

### Testing

- [OK] 既有真实隔离 PostgreSQL 验证：服务 race 专项 191 项、handler 19 项、repository 缺表与回滚 8 个子例通过，无跳过；全包编译和受影响包 vet、Web lint/build 及链接检查通过。
- [OK] 用户重启后：健康接口 200，旧接口 404；退役表字段任务、缺 roots、旧图片键和红果历史整理候选均为 0；实际 Web 脚本包含当前身份链接。
- [OK] 未运行全仓全部测试、真实上游抓取或登录态完整端到端；结论限定已审计范围。

### Status

[OK] **Completed**

### Next Steps

- 本轮已完成提交归档；不推送远端，原有 core 保持未跟踪。


## Session 239: 发现来源资料、榜单与详情统一收尾
<!-- trellis-session: v=2 fp=32a9d9f693a04482 -->

**Date**: 2026-10-13
**Task**: 发现来源资料、榜单与详情统一收尾
**Branch**: `main`

### Summary

完成五来源发现入口与分类榜单统一、TMDB共享详情、豆瓣资料补全及任务体系拆分；收敛分页失败恢复、摘要保留、统一加载与管理菜单等审查问题。隔离数据库聚焦测试、前端构建与浏览器回归通过，验证范围与真实上游限制见归档审查记录。

### Git Commits

| Hash | Message |
|------|---------|
| `9264fb1` | feat(discover): 统一来源资料浏览与榜单并完善资料补全任务 |

### Status

[OK] **Completed**


## Session 240: 来源详情演职员分组与横向续载
<!-- trellis-session: v=2 fp=93935d8e750db1c4 -->

**Date**: 2026-10-13
**Task**: 来源详情演职员分组与横向续载
**Branch**: `main`

### Summary

完成演员和幕后独立横向续载，每批20条；修复TMDB分类过滤与排序，统一详情头部并补充中文标签。隔离PostgreSQL测试、浏览器回归、类型检查、Lint及构建通过，独立复核完成。未访问生产数据或执行真实上游刷新。

### Git Commits

| Hash | Message |
|------|---------|
| `cb7a60a` | feat(discover): 演员与幕后分组横向续载并统一详情展示 |

### Status

[OK] **Completed**


## Session 241: 完成 Emby 扩展发现接口
<!-- trellis-session: v=2 fp=9976253429767a0b -->

**Date**: 2026-10-13
**Task**: 完成 Emby 扩展发现接口
**Branch**: `main`

### Summary

资料中心榜单、持久公共周历、可见媒体精确匹配及播放器接口分类已实现；PostgreSQL竞态回归、前端门禁与浏览器检查通过，后续接入LinPlayer。

### Git Commits

| Hash | Message |
|------|---------|
| `04dfe02` | feat(emby-ext): 提供资料中心榜单日历与媒体匹配接口 |

### Status

[OK] **Completed**


## Session 242: 服务器弹幕能力接入提交
<!-- trellis-session: v=2 fp=a3f4d9dba76af166 -->

**Date**: 2026-10-13
**Task**: 服务器弹幕能力接入提交
**Branch**: `main`

### Summary

声明服务器原生弹幕能力；能力查询独立于发现权限，发现内容仍受权限保护。同步播放器接口目录与扩展规范。HTTP权限/raw回归已在临时PostgreSQL上通过，目录类型及lint通过；未部署。仅提交5个相关文件，其它资料库性能任务改动保留。

### Git Commits

| Hash | Message |
|------|---------|
| `6226796` | feat(emby-ext): 声明服务器弹幕能力并独立查询权限 |

### Status

[OK] **Completed**


## Session 243: 豆瓣目录提速与资料列表交互统一
<!-- trellis-session: v=2 fp=ed9b9b82e48bad07 -->

**Date**: 2026-10-13
**Task**: 豆瓣目录提速与资料列表交互统一
**Branch**: `main`

### Summary

豆瓣目录采用数据库生成字段和分页索引，TMDB/豆瓣分类列表统一为每次30条滚动续载，分类榜单切换改为紧凑布局。独立审查、针对性PostgreSQL与浏览器回归、类型检查、lint和构建通过；扩展回归存在已复现的旧任务归属断言失败，详见归档验证记录。未重启实际服务、未写入生产结构、未推送远端。

### Git Commits

| Hash | Message |
|------|---------|
| `90dacc3` | perf(discover): 优化豆瓣目录查询并统一资料列表滚动续载 |

### Status

[OK] **Completed**


## Session 244: 媒体来源展示、补全任务与大库扫描交付
<!-- trellis-session: v=2 fp=9c1ed5402c8c5850 -->

**Date**: 2026-10-14
**Task**: 媒体来源展示、补全任务与大库扫描交付
**Branch**: `main`

### Summary

合并提交并归档资料中心维护、红果单表、大库扫描和直接来源展示四项任务；完成旧结构清理及索引人物查询修复，保留已知验证限制。

### Main Changes

- 普通媒体直接读取来源资料，退休旧补全及整理链路；人物任务归 TMDb。
- 有界大库扫描、红果摘要详情单表与索引人物分批装载一起提交。

### Git Commits

| Hash | Message |
|------|---------|
| `751b3e3` | refactor(media): 统一来源资料展示与任务并优化大库扫描 |

### Testing

- [OK] PostgreSQL 定向回归、真实库只读计划、全包编译、前端 lint/build 与启动复核通过；最终清理脚本复验通过。

### Status

[OK] **Completed**

### Next Steps

- 完整 200 万文件扫描压测及独立红果人物搜索问题后续跟踪，未宣称全量测试全绿。


## Session 245: 下载空间状态合并并归档
<!-- trellis-session: v=2 fp=b06cf8cfedb7a51c -->

**Date**: 2026-10-14
**Task**: 下载空间状态合并并归档
**Branch**: `main`

### Summary

合并下载、等待校验、校验和发布的展示与筛选，统一两个来源的状态顺序，补齐回归并提交归档。

### Main Changes

- 下载中统一计数、分集标签及作品筛选；旧筛选链接自动归一。

### Git Commits

| Hash | Message |
|------|---------|
| `286d0c4` | feat(downloads): 合并下载空间状态展示与筛选 |

### Testing

- [OK] 前端 lint/build、专项浏览器检查、两来源隔离 PostgreSQL 筛选回归及独立复核通过。
- [OK] 真实下载全流程未验证；完整页面回归在设置弹窗 Escape 检查处失败，保留待排查。

### Status

[OK] **Completed**

### Next Steps

- 其他窗口的发现与 TMDb 修改及既有 core 文件留在工作区。


## Session 246: TMDB 列表与入库标记性能修复
<!-- trellis-session: v=2 fp=2e0f19656409891f -->

**Date**: 2026-10-14
**Task**: TMDB 列表与入库标记性能修复
**Branch**: `main`

### Summary

列表异步补入库标记；后端按页内身份和 EXISTS 检查可见文件，移除展示视图关联。

### Git Commits

| Hash | Message |
|------|---------|
| `101ecdb` | perf(discover): 优化 TMDB 列表加载与入库状态查询 |

### Testing

- [OK] 前端 lint/build、目录续载浏览器回归、隔离 PostgreSQL 入库状态回归及独立复核通过；实际库 30 电影 SQL 执行约 59ms。

### Status

[OK] **Completed**

### Next Steps

- 后端重启后验证完整接口耗时；榜单/搜索续载重复查询已展示身份尚未优化。


## Session 247: 修复红果合并状态筛选性能并归档
<!-- trellis-session: v=2 fp=fa7755c51b8c439c -->

**Date**: 2026-10-14
**Task**: 修复红果合并状态筛选性能并归档
**Branch**: `main`

### Summary

活动作品先去重再筛选，修复合并状态在大库上反复扫描导致下载空间加载缓慢；补充统计过时和空活动状态回归及查询规范。

### Main Changes

- 红果 active 筛选使用一次性活动作品集合，保留搜索、排序、分页和完整分集统计。

### Git Commits

| Hash | Message |
|------|---------|
| `f5974c6` | fix(downloads): 修复红果合并状态筛选查询缓慢 |

### Testing

- [OK] 隔离 PostgreSQL 上30万分集计划测试、候选结果对照、两来源搜索与活动筛选回归全部通过，独立复核通过。
- [OK] 实际库只读执行计划约53ms普通/0.9ms通用，活动索引只执行一次；git diff --check通过。

### Status

[OK] **Completed**

### Next Steps

- 用户运行中的后端尚未重启，重启后的实际页面未验证；已有core文件未触碰。


## Session 248: 发现榜单刷新与资料任务衔接
<!-- trellis-session: v=2 fp=39cb5f60970b36d3 -->

**Date**: 2026-10-14
**Task**: 发现榜单刷新与资料任务衔接
**Branch**: `main`

### Summary

榜单移除搜索并增加手动刷新与成功时间，新增 TMDB/豆瓣定时榜单任务；沿用摘要占位、资料维护及独立图片任务。数据库针对性测试、并发检查、前端 lint/build 与浏览器交互通过，独立审查无剩余实质问题。未真实上游联调或部署，原 core 未触碰。

### Git Commits

| Hash | Message |
|------|---------|
| `98c7ae5` | feat(discover): 支持榜单手动和定时刷新及更新时间 |

### Status

[OK] **Completed**
