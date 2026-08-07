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
