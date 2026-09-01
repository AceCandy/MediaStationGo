# Journal - AceCandy (Part 1)

> AI development session journal
> Started: 2026-08-01

---



## Session 1: Complete shared media metadata

**Date**: 2026-08-03
**Task**: Complete shared media metadata
**Branch**: `main`

### Summary

Implemented shared metadata identities, MediaView reads, persistent artwork, Emby/search/playback projections, read-only sidecar behavior, and manual scrape response refresh. Full Go tests, frontend build/lint, and diff checks passed; archived task 08-01-shared-media-metadata.

### Git Commits

| Hash | Message |
|------|---------|
| `ddef5cc` | (see git log) |

### Status

[OK] **Completed**


## Session 2: 修复 STRM 多版本媒体信息

**Date**: 2026-08-04
**Task**: 修复 STRM 多版本媒体信息
**Branch**: `main`

### Summary

修复多版本本地 STRM 异步探测、真实源版本名、容器路径与平均码率，并更新共享媒体元数据规范。

### Git Commits

| Hash | Message |
|------|---------|
| `106aa36` | (see git log) |

### Status

[OK] **Completed**


## Session 3: 完整媒体轨道探测与播放选择

**Date**: 2026-08-04
**Task**: 完整媒体轨道探测与播放选择
**Branch**: `main`

### Summary

新增一对一完整媒体探测文档、统一本地与云媒体探测持久化、Emby 全轨道与实时字幕展示、播放选轨和 HLS 音轨隔离，并提供管理员按库回填任务。

### Git Commits

| Hash | Message |
|------|---------|
| `9d8e605` | (see git log) |
| `150ca66` | (see git log) |

### Status

[OK] **Completed**


## Session 4: 媒体详情轨道展示

**Date**: 2026-08-05
**Task**: 媒体详情轨道展示
**Branch**: `main`

### Summary

完成媒体详情轨道展示：详情接口通过安全白名单返回视频、音频和内嵌字幕轨道，列表接口保持轻量；补充 Emby HDR、语言、声道与绝对索引映射，修正合法扩展视频枚举、10/12-bit 像素格式推导和 44.1 kHz 显示，并同步共享媒体元数据规范。Go 全仓测试、vet、build，前端 lint/build 与 diff 检查均通过。

### Git Commits

| Hash | Message |
|------|---------|
| `70244e2` | (see git log) |

### Status

[OK] **Completed**


## Session 5: Cached playback redirects

**Date**: 2026-08-06
**Task**: Cached playback redirects
**Branch**: `main`

### Summary

Added one-hour cloud direct-link caching, playback source/cache diagnostics, configurable local-path to OpenList mappings, and server-side resolution of mapped OpenList URLs to final CDN redirects.

### Git Commits

| Hash | Message |
|------|---------|
| `407f620` | (see git log) |

### Status

[OK] **Completed**


## Session 6: 将成人刮削移出常规工作链

**Date**: 2026-08-06
**Task**: 将成人刮削移出常规工作链
**Branch**: `main`

### Summary

常规扫描、重刮、all provider 手动搜索及非成人整理不再调用成人源；保留显式成人手动搜索和成人整理，并补充回归测试与共享元数据规范。

### Git Commits

| Hash | Message |
|------|---------|
| `745a5e7` | (see git log) |

### Status

[OK] **Completed**


## Session 7: 复用 canonical metadata 扫描流程

**Date**: 2026-08-07
**Task**: 复用 canonical metadata 扫描流程
**Branch**: `main`

### Summary

完成方案 B：扫描按精确 provider ID 复用已有 canonical metadata；未命中保持 media.metadata_id 为 NULL，provider 成功或明确无匹配的 local fallback 后再绑定；补充 nullable schema、扫描与 enrichment 回归测试并归档任务。

### Git Commits

| Hash | Message |
|------|---------|
| `ac9ac54` | (see git log) |

### Status

[OK] **Completed**


## Session 8: Emby 人物元数据与 AI 翻译

**Date**: 2026-08-07
**Task**: Emby 人物元数据与 AI 翻译
**Branch**: `main`

### Summary

实现共享人物与演职员关系、Emby People 和人物回填、上下文缓存异步翻译、管理端 AI 模型与联网配置，并修复 PostgreSQL 迁移及长角色字段限制；补充可执行人物元数据规范。

### Git Commits

| Hash | Message |
|------|---------|
| `f22f311` | (see git log) |
| `7cf369e` | (see git log) |

### Status

[OK] **Completed**


## Session 9: Persist artwork and refactor Emby metadata scope

**Date**: 2026-08-08
**Task**: Persist artwork and refactor Emby metadata scope
**Branch**: `main`

### Summary

Persist cloud artwork and Emby people images under DataDir, and query Emby logical metadata scopes with version-aware visibility and pagination. Verified with go test ./..., go vet ./..., and git diff --check.

### Git Commits

| Hash | Message |
|------|---------|
| `26b4738` | (see git log) |

### Status

[OK] **Completed**


## Session 10: Finalize Season and Episode metadata ownership

**Date**: 2026-08-09
**Task**: Finalize Season and Episode metadata ownership
**Branch**: `main`

### Summary

Unified Episode title storage, kept Season and Episode metadata entity-owned, restored TMDb snapshot localization, and aligned Emby and Web consumers.

### Git Commits

| Hash | Message |
|------|---------|
| `e6b5b6b` | (see git log) |

### Testing

- [OK] go vet ./internal/service ./internal/repository ./internal/model ./internal/database
- [OK] go test ./internal/model ./internal/database ./internal/repository ./internal/service -count=1
- [OK] web npm run lint and npm run build

### Status

[OK] **Completed**

### Next Steps

- Run PostgreSQL integration tests when MEDIASTATION_TEST_POSTGRES_DSN is available.


## Session 11: Reorganize Web UI Around Emby Core

**Date**: 2026-08-10
**Task**: Reorganize Web UI Around Emby Core
**Branch**: `main`

### Summary

Reorganized viewer and admin navigation, unified route access, added bounded poster loading, and verified responsive UI.

### Git Commits

| Hash | Message |
|------|---------|
| `8c07aa5` | (see git log) |
| `2af58c4` | (see git log) |

### Status

[OK] **Completed**


## Session 12: Refine Web Information Architecture

**Date**: 2026-08-11
**Task**: Refine Web Information Architecture
**Branch**: `main`

### Summary

Reorganized management navigation around retained capabilities, merged related operational views, routed settings sections, removed duplicate UI, and verified responsive behavior.

### Git Commits

| Hash | Message |
|------|---------|
| `783d2ef` | (see git log) |
| `35ce448` | (see git log) |

### Status

[OK] **Completed**


## Session 13: Complete Emby core focus and risk closure

**Date**: 2026-08-12
**Task**: Complete Emby core focus and risk closure
**Branch**: `main`

### Summary

Completed the focused Emby direct-play verification, fixed empty catalog scheduling, removed Web dependency vulnerabilities, and recorded user acceptance of the three-client validation.

### Git Commits

| Hash | Message |
|------|---------|
| `ba77919` | (see git log) |
| `143f475` | (see git log) |
| `d8f0b2e` | (see git log) |
| `91ea66f` | (see git log) |
| `f0124e4` | (see git log) |
| `afe8124` | (see git log) |

### Status

[OK] **Completed**


## Session 14: 移除登录页社区链接配置

**Date**: 2026-08-12
**Task**: 移除登录页社区链接配置
**Branch**: `main`

### Summary

删除登录页社区链接 footer、无用设置项及其前后端专用接口，并增加退役路由回归断言；完成 handler 测试、前端 lint/build 和浏览器 smoke。

### Git Commits

| Hash | Message |
|------|---------|
| `3f1e80d` | (see git log) |

### Status

[OK] **Completed**


## Session 15: 移除系统更新功能

**Date**: 2026-08-12
**Task**: 移除系统更新功能
**Branch**: `main`

### Summary

完整移除系统更新前后端链路、Docker Compose 更新配置及专属任务类型；启动迁移精确清理四个历史设置键，并补充 PostgreSQL 回归测试和数据库退役配置规范。

### Git Commits

| Hash | Message |
|------|---------|
| `8c203ae` | (see git log) |

### Status

[OK] **Completed**


## Session 16: 统一任务执行列表

**Date**: 2026-08-13
**Task**: 统一任务执行列表
**Branch**: `main`

### Summary

持久化统一后台任务执行记录与分日任务日志，统一媒体和目录刮削调度，并更新任务中心页面。

### Git Commits

| Hash | Message |
|------|---------|
| `1a74908` | (see git log) |

### Status

[OK] **Completed**


## Session 17: Complete people background tasks

**Date**: 2026-08-13
**Task**: Complete people background tasks
**Branch**: `main`

### Summary

Added global people backfill and tracked people translation in the task center, with manual triggering, hydration state, cancellation semantics, tests, and UI verification.

### Git Commits

| Hash | Message |
|------|---------|
| `096ba7e` | (see git log) |

### Status

[OK] **Completed**


## Session 18: Stop invalid TMDB people retries

**Date**: 2026-08-13
**Task**: Stop invalid TMDB people retries
**Branch**: `main`

### Summary

人物补齐仅处理 TMDB 来源；credits 404 时清除失效标识、重置媒体刮削状态并唤醒统一 worker，避免重复请求。

### Git Commits

| Hash | Message |
|------|---------|
| `b5ded94` | (see git log) |

### Status

[OK] **Completed**


## Session 19: Task center definition view

**Date**: 2026-08-13
**Task**: Task center definition view
**Branch**: `main`

### Summary

任务中心改为稳定任务定义列表，分离当前状态与最近结果，合并周期和人物补齐操作，并提供任务级执行历史与日志。

### Git Commits

| Hash | Message |
|------|---------|
| `1d42906` | (see git log) |

### Status

[OK] **Completed**


## Session 20: Refine web UI and detailed task logs

**Date**: 2026-08-13
**Task**: Refine web UI and detailed task logs
**Branch**: `main`

### Summary

Committed the existing web UI redesign together with detailed background task logs, task-log redaction rules, focused service tests, and Trellis specs.

### Git Commits

| Hash | Message |
|------|---------|
| `dc29dcf` | (see git log) |

### Status

[OK] **Completed**


## Session 21: 按日期聚合任务日志

**Date**: 2026-08-13
**Task**: 按日期聚合任务日志
**Branch**: `main`

### Summary

将任务日志改为按稳定任务定义每日一个文件，新增按日期读取接口与日历选择界面，并补充后端测试、竞态防护和后台任务日志规范。

### Git Commits

| Hash | Message |
|------|---------|
| `afe4cd4` | (see git log) |

### Status

[OK] **Completed**


## Session 22: 新增 Emby 播放器接口目录

**Date**: 2026-08-14
**Task**: 新增 Emby 播放器接口目录
**Branch**: `main`

### Summary

新增管理员可见的 Emby 播放器接口目录，入口位于观看空间的我的之后；保留旧路由重定向，并记录后端路由与静态目录同步规范。

### Git Commits

| Hash | Message |
|------|---------|
| `cf06d0c` | (see git log) |

### Status

[OK] **Completed**


## Session 23: 恢复页面文本选择

**Date**: 2026-08-15
**Task**: 恢复页面文本选择
**Branch**: `main`

### Summary

移除根布局的 select-none，使认证区域中的页面文本恢复鼠标框选与复制。

### Git Commits

| Hash | Message |
|------|---------|
| `a7d5063` | (see git log) |

### Status

[OK] **Completed**


## Session 24: 优化媒体详情与媒体库加载

**Date**: 2026-08-15
**Task**: 优化媒体详情与媒体库加载
**Branch**: `main`

### Summary

将媒体详情探测改为局部异步流程，新增可见版本与展示型轨道选择器，并将媒体库改为每页 50 条下滑加载。

### Git Commits

| Hash | Message |
|------|---------|
| `274ee4c` | (see git log) |

### Status

[OK] **Completed**


## Session 25: 优化 Emby 列表查询性能

**Date**: 2026-08-15
**Task**: 优化 Emby 列表查询性能
**Branch**: `main`

### Summary

批量化 Emby 列表关系查询，支持 Fields 按需返回，并增加播放器请求脱敏日志。

### Git Commits

| Hash | Message |
|------|---------|
| `6d96ea3` | (see git log) |
| `eaeefb6` | (see git log) |

### Status

[OK] **Completed**


## Session 26: 播放器请求日志实时查看

**Date**: 2026-08-15
**Task**: 播放器请求日志实时查看
**Branch**: `main`

### Summary

新增按 UTC 月分区的脱敏播放器请求日志、管理员查询页面与 SSE 实时刷新，并修复移动底栏拥挤和非法月份 URL 规范化。

### Git Commits

| Hash | Message |
|------|---------|
| `b798612` | (see git log) |

### Status

[OK] **Completed**


## Session 27: 本地化常见影视职务

**Date**: 2026-08-16
**Task**: 本地化常见影视职务
**Branch**: `main`

### Summary

在 Emby 人物输出阶段统一本地化导演与编剧相关职务，保留协议 Type 英文枚举，并增加定向回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `324ff15` | (see git log) |

### Status

[OK] **Completed**


## Session 28: 复用剧集角色翻译

**Date**: 2026-08-16
**Task**: 复用剧集角色翻译
**Branch**: `main`

### Summary

将单集角色翻译缓存上下文归并至所属季，避免同季重复 AI 调用；新增同季复用和跨季隔离回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `8c7081f` | (see git log) |

### Status

[OK] **Completed**


## Session 29: 重组空间导航并合并系统监控

**Date**: 2026-08-17
**Task**: 重组空间导航并合并系统监控
**Branch**: `main`

### Summary

新增文件空间并打平管理空间导航，合并存储概览与运行监控，完成构建、响应式界面和故障隔离验证。

### Git Commits

| Hash | Message |
|------|---------|
| `26a480b64979a4c33eeb4e7fa47a01ee4f5f2d07` | (see git log) |

### Status

[OK] **Completed**


## Session 30: 记录空人物补齐执行

**Date**: 2026-08-17
**Task**: 记录空人物补齐执行
**Branch**: `main`

### Summary

手动人物信息补齐在零候选时仍记录完成时间和任务日志，候选查询失败时记录失败；保留自动空扫描静默行为并补充回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `f5e382e` | (see git log) |

### Status

[OK] **Completed**


## Session 31: 修正媒体增量扫描与硬删除

**Date**: 2026-08-17
**Task**: 修正媒体增量扫描与硬删除
**Branch**: `main`

### Summary

持久化本地文件大小与纳秒 mtime，未变化扫描直接跳过；媒体删除统一硬删除并移除回收站；自动 STRM 生成跳过已有 STRM 源且保护源文件；同步提交当前管理界面改动。

### Git Commits

| Hash | Message |
|------|---------|
| `5b923b5` | (see git log) |

### Status

[OK] **Completed**


## Session 32: 记录媒体扫描变更文件

**Date**: 2026-08-17
**Task**: 记录媒体扫描变更文件
**Branch**: `main`

### Summary

媒体扫描任务日志新增图标化的新增、更新、删除文件路径，更新项记录文件指纹或元数据变化原因；扫描详情不再显示 DETAIL 标签，并补齐手动、计划任务和 STRM 刷新回归验证。

### Git Commits

| Hash | Message |
|------|---------|
| `18d64e5` | (see git log) |

### Status

[OK] **Completed**


## Session 33: 扫描仅按文件指纹判断变化

**Date**: 2026-08-17
**Task**: 扫描仅按文件指纹判断变化
**Branch**: `main`

### Summary

将本地媒体扫描更新判定收敛为文件大小与纳秒级 mtime；指纹一致时在读取 NFO、派生元数据和 STRM 目标前跳过，并补充回归测试与后端规范。

### Git Commits

| Hash | Message |
|------|---------|
| `23e8ff0` | (see git log) |

### Status

[OK] **Completed**


## Session 34: 复用已有元数据跳过重复刮削

**Date**: 2026-08-18
**Task**: 复用已有元数据跳过重复刮削
**Branch**: `main`

### Summary

新增精确 Provider 元数据复用快捷路径，按剧集层级校验 Episode，并对默认占位标题实施 7 天刷新规则；补充测试和共享媒体元数据规范。

### Git Commits

| Hash | Message |
|------|---------|
| `6c0ea0f` | (see git log) |

### Status

[OK] **Completed**


## Session 35: 优化页面请求与发现页调度

**Date**: 2026-08-18
**Task**: 优化页面请求与发现页调度
**Branch**: `main`

### Summary

完成首页、媒体库、发现页及个人数据页面的重复请求优化；发现页按页码合并请求，并实现同 Provider 串行、不同 Provider 最多两路并行调度，补充 Provider 锁与回归测试。归档 Bootstrap Guidelines 和统一观看历史与播放统计任务。

### Git Commits

| Hash | Message |
|------|---------|
| `a728e98` | (see git log) |

### Status

[OK] **Completed**


## Session 36: 媒体轨道探测与回填

**Date**: 2026-08-18
**Task**: 媒体轨道探测与回填
**Branch**: `main`

### Summary

任务中心支持按媒体库和数量执行条件回填；详情页统一强制探测提示，并补充单集与整剧入口。

### Git Commits

| Hash | Message |
|------|---------|
| `8fd4401` | (see git log) |

### Status

[OK] **Completed**


## Session 37: 统一任务中心日志样式

**Date**: 2026-08-19
**Task**: 统一任务中心日志样式
**Branch**: `main`

### Summary

统一七类任务日志语义标记与彩色徽标，补充日志刷新、轨道探测逐条结果和远程等待，并修正倒序箭头及重复扫描进度。

### Git Commits

| Hash | Message |
|------|---------|
| `f62a778` | (see git log) |

### Status

[OK] **Completed**


## Session 38: 统一任务中心调度与监听日志

**Date**: 2026-08-19
**Task**: 统一任务中心调度与监听日志
**Branch**: `main`

### Summary

将五类周期任务统一接入任务中心配置，新增媒体库变更监听批次执行与日志，并补齐前后端验证。

### Git Commits

| Hash | Message |
|------|---------|
| `6be35eb` | (see git log) |

### Status

[OK] **Completed**


## Session 39: 精简事件触发任务日志

**Date**: 2026-08-20
**Task**: 精简事件触发任务日志
**Branch**: `main`

### Summary

事件触发任务不再写通用开始和结束生命周期日志，保留进度、业务明细与失败原因；手动和定时任务行为不变，并补充聚焦回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `11ea9b1` | (see git log) |

### Status

[OK] **Completed**


## Session 40: STRM 轨道本地路径映射

**Date**: 2026-08-20
**Task**: STRM 轨道本地路径映射
**Branch**: `main`

### Summary

新增远程 STRM 到本地 FFprobe 路径映射，补充安全边界、回退逻辑、配置界面、测试和媒体元数据规范。

### Git Commits

| Hash | Message |
|------|---------|
| `4f92955` | (see git log) |

### Status

[OK] **Completed**


## Session 41: 统一 ffprobe 媒体技术元数据

**Date**: 2026-08-21
**Task**: 统一 ffprobe 媒体技术元数据
**Branch**: `main`

### Summary

将媒体时长、大小、容器、编解码等技术事实统一到 media_probe_metadata，删除 media 主表重复列并修复 PlaybackInfo 与进度兼容。

### Git Commits

| Hash | Message |
|------|---------|
| `a4ad1fb` | (see git log) |

### Status

[OK] **Completed**


## Session 42: 详情页影院化与媒体库网格美观度重构

**Date**: 2026-08-21
**Task**: 详情页影院化与媒体库网格美观度重构
**Branch**: `main`

### Summary

详情页影院化：新增 GET /media/:id/credits 演职员接口与通栏横滚、海报环境取色光晕、布局重构（左列海报+媒体信息紧跟、右列元信息+播放操作排）、管理功能收敛为「更多操作」下拉。媒体库：网格降为 2-6 列大卡片大间距，卡片移除立即观影/刮削/删除按钮改为单收藏角标（页面级 listFavourites id 集合 + toggleFavourite），轨道/版本下拉截断选项 hover 气泡全显（修复 details 收起时 clientWidth=0 测量失效）。卡片鼠标跟随高光与入场错落、空状态/骨架屏美化。顺带提交并归档 08-20-player-request-body-log（播放器请求 Body 日志：64KiB 截断、敏感字段脱敏、幂等迁移、日志详情页展示）。

### Git Commits

| Hash | Message |
|------|---------|
| `49404af` | (see git log) |

### Status

[OK] **Completed**


## Session 43: 完成播放统计明细与热榜

**Date**: 2026-08-21
**Task**: 完成播放统计明细与热榜
**Branch**: `main`

### Summary

实现播放统计明细、分页、每日/每周热榜及对应后端查询、前端页面和测试；完成质量检查并归档任务。

### Git Commits

| Hash | Message |
|------|---------|
| `9f80754` | (see git log) |

### Status

[OK] **Completed**


## Session 44: 完善人物翻译日志与定时调度

**Date**: 2026-08-21
**Task**: 完善人物翻译日志与定时调度
**Branch**: `main`

### Summary

人物翻译日志补充所属电影或电视剧；移除事件唤醒，仅保留定时执行；单次限制为1000个去重翻译项并补充回归测试与规范。

### Git Commits

| Hash | Message |
|------|---------|
| `26cfb2e` | (see git log) |

### Status

[OK] **Completed**


## Session 45: 统一定时任务手动触发

**Date**: 2026-08-21
**Task**: 统一定时任务手动触发
**Branch**: `main`

### Summary

统一任务中心定时任务的手动执行入口，移除人物补齐启动事件 worker，为账号清理增加危险确认，并补齐回归测试与后台任务规范。

### Git Commits

| Hash | Message |
|------|---------|
| `0030282` | (see git log) |

### Status

[OK] **Completed**


## Session 46: Emby Latest 默认隐藏已播放项

**Date**: 2026-08-22
**Task**: Emby Latest 默认隐藏已播放项
**Branch**: `main`

### Summary

为 Emby Latest 增加按用户和作品播放完成状态过滤，默认隐藏已播放项，显式支持 IsPlayed，并移除会产生陈旧播放状态的 Latest 缓存；补充电影、剧集和用户隔离测试及 API 目录说明。

### Git Commits

| Hash | Message |
|------|---------|
| `1ff4b8a` | (see git log) |

### Status

[OK] **Completed**


## Session 47: 优化播放器查询链并统一 Web 播放逻辑

**Date**: 2026-08-22
**Task**: 优化播放器查询链并统一 Web 播放逻辑
**Branch**: `main`

### Summary

复用 PlaybackInfo 请求内媒体、probe 与字幕数据，优化具体媒体 ID 和字幕交付查询链；Web 外部播放器改为单请求并复用返回路径逻辑。

### Git Commits

| Hash | Message |
|------|---------|
| `061e55b` | (see git log) |

### Status

[OK] **Completed**


## Session 48: 优化发现页加载性能

**Date**: 2026-08-22
**Task**: 优化发现页加载性能
**Branch**: `main`

### Summary

优化发现页缓存复用、按需图片加载、单栏翻页与请求取消，并完成真实浏览器性能复测

### Git Commits

| Hash | Message |
|------|---------|
| `2082b37` | (see git log) |

### Status

[OK] **Completed**


## Session 49: 优化播放重定向缓存与错误日志

**Date**: 2026-08-22
**Task**: 优化播放重定向缓存与错误日志
**Branch**: `main`

### Summary

完成播放重定向缓存键与本地回退、失败响应正文日志、取消请求 499、播放链路媒体复用及 Emby 播放进度查询优化

### Git Commits

| Hash | Message |
|------|---------|
| `15b80db` | (see git log) |
| `e00326e` | (see git log) |

### Status

[OK] **Completed**


## Session 50: 调整媒体库扫描入口与自动触发

**Date**: 2026-08-22
**Task**: 调整媒体库扫描入口与自动触发
**Branch**: `main`

### Summary

任务中心手动扫描改为必选单库；新增媒体库或路径后按最小范围自动创建 event 扫描任务；移除管理页与详情页重复扫描入口，并补齐后端回归测试与任务契约。

### Git Commits

| Hash | Message |
|------|---------|
| `9b43a70` | (see git log) |

### Status

[OK] **Completed**


## Session 51: 优化自动入库刮削吞吐

**Date**: 2026-08-22
**Task**: 优化自动入库刮削吞吐
**Branch**: `main`

### Summary

固定三个自动媒体 worker，保持 catalog 串行和媒体优先；复用完整 TMDB 详情并增加分段耗时观测与测试。

### Git Commits

| Hash | Message |
|------|---------|
| `0e3d44a` | (see git log) |

### Status

[OK] **Completed**


## Session 52: 完善媒体入库刮削策略与失败处置

**Date**: 2026-08-23
**Task**: 完善媒体入库刮削策略与失败处置
**Branch**: `main`

### Summary

新增单库及整库手动刮削、扫描后默认自动刮削、NFO-only 非常规媒体库、统一成功来源日志与失败处置页面，并兼容 legacy show 类型。

### Git Commits

| Hash | Message |
|------|---------|
| `6413e49` | (see git log) |

### Status

[OK] **Completed**


## Session 53: 统一元数据搜索与后台任务修复

**Date**: 2026-08-24
**Task**: 统一元数据搜索与后台任务修复
**Branch**: `main`

### Summary

统一 Web 与 Emby 顶层 Metadata 搜索及 OpenSearch 同步；修复未入库媒体删除、刮削路径展示和人物翻译无效结果重复请求。

### Git Commits

| Hash | Message |
|------|---------|
| `dd76a84` | (see git log) |

### Status

[OK] **Completed**


## Session 54: 完善搜索匹配与统一排序

**Date**: 2026-08-24
**Task**: 完善搜索匹配与统一排序
**Branch**: `main`

### Summary

统一 OpenSearch/PostgreSQL 的 100 条候选召回与 Go 排序，支持 0–100 数字双向等价、完整数字段匹配和内存分页。

### Git Commits

| Hash | Message |
|------|---------|
| `d9cffcc` | (see git log) |

### Status

[OK] **Completed**


## Session 55: Emby 人物与媒体并列搜索

**Date**: 2026-08-24
**Task**: Emby 人物与媒体并列搜索
**Branch**: `main`

### Summary

支持非空搜索中 Person、Movie、Series 按 OR 合并；人物仅走 PostgreSQL，媒体保留 OpenSearch 优先与 PostgreSQL 回退；统一排序、100 条上限和内存分页，并补充类型组合、SearchHints、去重及分页测试。

### Git Commits

| Hash | Message |
|------|---------|
| `565d7d8` | (see git log) |

### Status

[OK] **Completed**


## Session 56: 完成 Emby Jellyfin 多 Part 支持

**Date**: 2026-08-24
**Task**: 完成 Emby Jellyfin 多 Part 支持
**Branch**: `main`

### Summary

实现多 Part 文件识别与扫描调和、版本折叠、PartCount 和 AdditionalParts API，并确保后续 Part 使用独立媒体 ID、轨道、播放地址和进度；补齐测试、API 目录及后端规范。

### Git Commits

| Hash | Message |
|------|---------|
| `ac24bf956b8bd856da3a00f8e942f65017753ee0` | (see git log) |

### Status

[OK] **Completed**


## Session 57: Metadata 图片与豆瓣电影补齐

**Date**: 2026-08-26
**Task**: Metadata 图片与豆瓣电影补齐
**Branch**: `main`

### Summary

完成 metadata 图硬删除迁移、TMDb 缺图巡检、豆瓣电影第二数据源与慢速历史补齐，并移除元数据编辑中的图片 URL。Go 回归、前端构建和 lint 已通过；PostgreSQL 集成测试与生产规模 EXPLAIN 留作部署验证。

### Git Commits

| Hash | Message |
|------|---------|
| `b04862a` | (see git log) |

### Status

[OK] **Completed**


## Session 58: 拆分 TMDb 图片修复与复查任务

**Date**: 2026-08-26
**Task**: 拆分 TMDb 图片修复与复查任务
**Branch**: `main`

### Summary

完成 TMDb 图片本地化修复与无图复查拆分、全量 keyset 扫描、豆瓣无快照候选收缩和媒体轨道失败路径日志；归档已完成任务，保留 Episode 信息复查规划。

### Main Changes

- TMDb 图片本地化与无图复查改为独立任务并单次扫描全部候选
- 豆瓣补齐只处理已有唯一豆瓣 ID 且无详情快照的电影
- 媒体轨道回填失败详情增加 STRM 路径

### Git Commits

| Hash | Message |
|------|---------|
| `05f9021` | (see git log) |
| `f8abafd` | (see git log) |

### Testing

- [OK] 聚焦 Go 测试与 git diff --check 通过；PostgreSQL 集成用例因缺少 MEDIASTATION_TEST_POSTGRES_DSN 跳过

### Status

[OK] **Completed**

### Next Steps

- 确认后实施 TMDb 集信息补全/复查规划


## Session 59: 修复 STRM 保留字符探测失败

**Date**: 2026-08-27
**Task**: 修复 STRM 保留字符探测失败
**Branch**: `main`

### Summary

统一规范化 STRM HTTP(S) URL 中的 raw #，拒绝目录探测源，并补充安全的 ffprobe stderr 摘要与回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `ff48b40` | (see git log) |

### Status

[OK] **Completed**


## Session 60: TMDb 集信息补全复查

**Date**: 2026-08-27
**Task**: TMDb 集信息补全复查
**Branch**: `main`

### Summary

新增仅针对有媒体 Episode 的 TMDb 集信息补全/复查任务，覆盖标题、简介、播出日期、评分、年份、演职员和 still，加入 72 小时冷却、全量 keyset 扫描、独立调度与脱敏日志，并从无图复查移除 Episode。

### Git Commits

| Hash | Message |
|------|---------|
| `8f27aa1` | (see git log) |

### Status

[OK] **Completed**


## Session 61: 统一 Web 自定义下拉框

**Date**: 2026-08-27
**Task**: 统一 Web 自定义下拉框
**Branch**: `main`

### Summary

新增主题化共享 Select，替换 Web 产品代码中 29 处原生下拉框，并完成 lint、build、浏览器交互验证与独立复核。

### Git Commits

| Hash | Message |
|------|---------|
| `6a70b92` | (see git log) |

### Status

[OK] **Completed**


## Session 62: 修复 TMDb 集复查列名迁移

**Date**: 2026-08-27
**Task**: 修复 TMDb 集复查列名迁移
**Branch**: `main`

### Summary

显式固定 TMDb 集复查检查点列名，迁移并清理旧列，补充 PostgreSQL 回归测试与规范。

### Git Commits

| Hash | Message |
|------|---------|
| `f05a144` | (see git log) |

### Status

[OK] **Completed**


## Session 63: 媒体库缺失元数据筛选与入口精简

**Date**: 2026-08-28
**Task**: 媒体库缺失元数据筛选与入口精简
**Branch**: `main`

### Summary

完成单库缺失海报/中文名筛选，移除失效或危险的批量入口，并统一每集图片默认开启行为；相关 Go 测试、前端 lint/build 与静态检查通过。

### Git Commits

| Hash | Message |
|------|---------|
| `14d706b` | (see git log) |

### Status

[OK] **Completed**


## Session 64: 详情页来源链接与豆瓣缺失补齐

**Date**: 2026-08-28
**Task**: 详情页来源链接与豆瓣缺失补齐
**Branch**: `main`

### Summary

详情页展示 canonical 豆瓣/TMDb 外链与本地快照标识；豆瓣电影补齐支持刷新超过 24 小时且缺海报、简介或中文标题的快照，失败不推进冷却，并区分更新与无变化指标。

### Git Commits

| Hash | Message |
|------|---------|
| `e09696b` | (see git log) |

### Status

[OK] **Completed**


## Session 65: 豆瓣移动端详情与来源状态

**Date**: 2026-08-28
**Task**: 豆瓣移动端详情与来源状态
**Branch**: `main`

### Summary

豆瓣按 ID 详情和集数切换到移动端 JSON 并保留摘要降级；旧或缺字段快照按 24 小时冷却刷新；详情页展示 provider 三态图标和评分占位。

### Git Commits

| Hash | Message |
|------|---------|
| `b4bcf62` | (see git log) |

### Status

[OK] **Completed**


## Session 66: 完善媒体详情标签图标和发布日期
<!-- trellis-session: v=2 fp=7dd8c08245dcbc20 -->

**Date**: 2026-08-28
**Task**: 完善媒体详情标签图标和发布日期
**Branch**: `main`

### Summary

为媒体详情的分辨率、时长、大小和格式补充图标，加入豆瓣与 TMDB 品牌资源，并优先显示完整发布日期。

### Git Commits

| Hash | Message |
|------|---------|
| `ce2432b` | feat(media): add metadata badge icons and release dates |

### Status

[OK] **Completed**


## Session 67: TMDB snapshot persistence and backfill
<!-- trellis-session: v=2 fp=65051ee1bb576e69 -->

**Date**: 2026-08-29
**Task**: TMDB snapshot persistence and backfill
**Branch**: `main`

### Summary

Persist complete TMDB detail snapshots across scrape and manual-ID flows, add one-time resumable historical backfill with task-center progress and manual retry, and document the executable contract.

### Git Commits

| Hash | Message |
|------|---------|
| `677d2b9` | feat(metadata): persist and backfill TMDB snapshots |
| `ed94099` | docs(spec): record TMDB snapshot persistence contract |

### Status

[OK] **Completed**


## Session 68: 精简豆瓣电影补齐日志
<!-- trellis-session: v=2 fp=31c309b11d3c9704 -->

**Date**: 2026-08-29
**Task**: 精简豆瓣电影补齐日志
**Branch**: `main`

### Summary

豆瓣电影信息补齐日志改为仅展示实际新增、更新和失败详情，空跑显示本次无变更；补充回归测试并通过 service 包测试。

### Git Commits

| Hash | Message |
|------|---------|
| `facec9f` | fix(metadata): show actionable Douban enrichment details |

### Status

[OK] **Completed**


## Session 69: 优化媒体库扫描与轨道回填
<!-- trellis-session: v=2 fp=c3bf7ace4f2ee807 -->

**Date**: 2026-08-29
**Task**: 优化媒体库扫描与轨道回填
**Branch**: `main`

### Summary

解耦扫描入库与 ffprobe，统一可见轨道回填任务，补充多路径进度、路径级提交、有界详情和唤醒合并，并完成定向验证。

### Git Commits

| Hash | Message |
|------|---------|
| `0a648eb` | feat(media): decouple library scan and track backfill |

### Status

[OK] **Completed**


## Session 70: 修复电影库剧集误判
<!-- trellis-session: v=2 fp=bbd98eef02f83a2c -->

**Date**: 2026-08-30
**Task**: 修复电影库剧集误判
**Branch**: `main`

### Summary

按媒体库类型判定电影与剧集，电视剧扫描仅识别 SxxExx，并在电影库重扫时纠正历史季集脏数据。

### Git Commits

| Hash | Message |
|------|---------|
| `b5f1cf1` | fix(media): prevent movie episode misclassification |

### Status

[OK] **Completed**


## Session 71: Fix PostgreSQL parameter overflow
<!-- trellis-session: v=2 fp=001f28d8cce6619d -->

**Date**: 2026-08-30
**Task**: Fix PostgreSQL parameter overflow
**Branch**: `main`

### Summary

Audited unbounded PostgreSQL collection bindings, replaced risky slice expansion with array parameters, batched metadata identifier inserts, and added regression coverage.

### Git Commits

| Hash | Message |
|------|---------|
| `edcd134` | fix(database): prevent PostgreSQL parameter overflow |

### Status

[OK] **Completed**


## Session 72: 豆瓣 Cookie 与信息补齐
<!-- trellis-session: v=2 fp=37317d7a7e35e8f3 -->

**Date**: 2026-08-31
**Task**: 豆瓣 Cookie 与信息补齐
**Branch**: `main`

### Summary

将豆瓣 Cookie 收口到外部 API 配置，扩宽凭据列，并新增只走移动详情的单媒体与批量补齐、限流停批和同条重试。

### Git Commits

| Hash | Message |
|------|---------|
| `07cabd1` | feat(douban): centralize cookie and make enrichment retry-safe |

### Status

[OK] **Completed**


## Session 73: 归档电影扫描 deleted_at 修复任务
<!-- trellis-session: v=2 fp=3578dfc3edc2f603 -->

**Date**: 2026-08-31
**Task**: 归档电影扫描 deleted_at 修复任务
**Branch**: `main`

### Summary

电影扫描 deleted_at 根因修复已完成，用户确认 PostgreSQL 专项验收通过，任务已归档。

### Git Commits

| Hash | Message |
|------|---------|
| `345a100` | fix(scanner): remove retired metadata soft-delete filter |

### Status

[OK] **Completed**


## Session 74: 豆瓣大图与图片本地化修复
<!-- trellis-session: v=2 fp=79282238d7c28c34 -->

**Date**: 2026-08-31
**Task**: 豆瓣大图与图片本地化修复
**Branch**: `main`

### Summary

支持豆瓣图片 origin 配置、大图字段优先级和安全的历史候选本地化修复任务；聚焦测试与 go vet 通过，PostgreSQL 集成测试因无独立测试 DSN 跳过。

### Git Commits

| Hash | Message |
|------|---------|
| `60bd0cc` | feat: support configurable Douban artwork repair |

### Status

[OK] **Completed**


## Session 75: 豆瓣大图与 WebP 统一
<!-- trellis-session: v=2 fp=66a2edd37a13756c -->

**Date**: 2026-08-31
**Task**: 豆瓣大图与 WebP 统一
**Branch**: `main`

### Summary

统一豆瓣搜索、详情、发现、补齐和历史修复的大图 URL；配置图片域名时改用 WebP，并让发现缓存即时应用最新配置。

### Git Commits

| Hash | Message |
|------|---------|
| `e8ae4d0` | feat: use large WebP images for Douban artwork |

### Status

[OK] **Completed**


## Session 76: 豆瓣图片直连与 curl 兜底
<!-- trellis-session: v=2 fp=ef23f679fd3c4659 -->

**Date**: 2026-09-01
**Task**: 豆瓣图片直连与 curl 兜底
**Branch**: `main`

### Summary

在外部 API 豆瓣配置中增加图片直连开关；开启后官方或当前自定义图片域名按直连到 curl 获取，保留非豆瓣旧行为并隔离失败缓存。补齐迁移、配置、请求顺序、域名隔离和缓存回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `ce1f956` | feat: add Douban image direct fallback |

### Status

[OK] **Completed**


## Session 77: Douban detail degradation fallback
<!-- trellis-session: v=2 fp=cc2bf6ecd99b2320 -->

**Date**: 2026-09-01
**Task**: Douban detail degradation fallback
**Branch**: `main`

### Summary

Added movie/tv permission fallback to subject, persisted degraded status, skipped periodic retries, and exposed manual recovery in media detail.

### Git Commits

| Hash | Message |
|------|---------|
| `bf69f83` | feat: add Douban detail degradation fallback |

### Status

[OK] **Completed**


## Session 78: 豆瓣补齐实时输出明细
<!-- trellis-session: v=2 fp=52b4930846dfa738 -->

**Date**: 2026-09-01
**Task**: 豆瓣补齐实时输出明细
**Branch**: `main`

### Summary

豆瓣电影信息补齐改为逐条增量写入任务日志，完成时仅写汇总，并增加实时可见与去重回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `7d6240d` | fix: stream Douban enrichment task details |

### Status

[OK] **Completed**


## Session 79: 豆瓣原图与 CDN 回源
<!-- trellis-session: v=2 fp=d7d179bc187181b9 -->

**Date**: 2026-09-01
**Task**: 豆瓣原图与 CDN 回源
**Branch**: `main`

### Summary

豆瓣海报改存官方原图 URL，下载时临时使用当前 CDN 并失败回源；仅最终官方 404 写负缓存，清理已确认的失败标记。

### Git Commits

| Hash | Message |
|------|---------|
| `3a4a3b5f0d7d25fb78726b1320e607c4f4c81e80` | fix: preserve original Douban artwork URLs |

### Status

[OK] **Completed**


## Session 80: 外部 API 代理池
<!-- trellis-session: v=2 fp=04067b05b0809283 -->

**Date**: 2026-09-01
**Task**: 外部 API 代理池
**Branch**: `main`

### Summary

新增加密静态代理池与管理界面，豆瓣 JSON 请求仅在 HTTP 400 时按顺序切换并粘性复用；完成针对性测试、静态检查和规格同步。

### Git Commits

| Hash | Message |
|------|---------|
| `5b1edcf` | feat: add Douban proxy pool |

### Status

[OK] **Completed**


## Session 81: 豆瓣电影信息补齐持续执行
<!-- trellis-session: v=2 fp=af9131a6443bdc54 -->

**Date**: 2026-09-02
**Task**: 豆瓣电影信息补齐持续执行
**Branch**: `main`

### Summary

豆瓣电影信息补齐改为每批 100 条持续分页至耗尽，并在临时异常日志中保留脱敏后的具体原因。

### Git Commits

| Hash | Message |
|------|---------|
| `0dbef6b` | fix: complete Douban enrichment runs |

### Status

[OK] **Completed**


## Session 82: 豆瓣 unexpected EOF 自动切换代理
<!-- trellis-session: v=2 fp=ec750fa950b0f4f1 -->

**Date**: 2026-09-02
**Task**: 豆瓣 unexpected EOF 自动切换代理
**Branch**: `main`

### Summary

豆瓣代理路由在 exact HTTP 400 之外，新增 unexpected EOF 自动重选，并同步代理规范与边界测试。

### Git Commits

| Hash | Message |
|------|---------|
| `e410f1f` | fix: retry Douban routes on unexpected EOF |

### Status

[OK] **Completed**


## Session 83: 代理池健康检测与清理
<!-- trellis-session: v=2 fp=f83dabac9b07b4b1 -->

**Date**: 2026-09-02
**Task**: 代理池健康检测与清理
**Branch**: `main`

### Summary

新增已保存代理的豆瓣健康检测、汇总确认和一次性令牌安全清理；补齐分类、取消、并发更新、凭据保护与前端交互验证。

### Git Commits

| Hash | Message |
|------|---------|
| `19ef3a1` | feat: add proxy pool health cleanup |

### Status

[OK] **Completed**
