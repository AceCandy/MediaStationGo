# Journal - AceCandy (Part 2)

> Continuation from `journal-1.md` (archived at ~2000 lines)
> Started: 2026-09-04

---



## Session 92: Refine media library management UI
<!-- trellis-session: v=2 fp=6094551d052f33ef -->

**Date**: 2026-09-04
**Task**: Refine media library management UI
**Branch**: `main`

### Summary

Refined media library cover cards and management dialog: 16:9 hover overlays, denser responsive grid, direct cover upload/clear, read-only paths, confirmed enable/disable, and direct confirmed deletion.

### Git Commits

| Hash | Message |
|------|---------|
| `fd2198b` | feat: refine media library management UI |

### Status

[OK] **Completed**


## Session 93: 集中媒体刮入库失败手动匹配入口
<!-- trellis-session: v=2 fp=95a782fb190e016b -->

**Date**: 2026-09-05
**Task**: 集中媒体刮入库失败手动匹配入口
**Branch**: `main`

### Summary

任务中心为非 NFO 刮削失败和未匹配媒体记录提供手动匹配，并移除 metadata 详情页和电视剧详情页的旧入口。

### Git Commits

| Hash | Message |
|------|---------|
| `41ce3fe` | fix: centralize manual scrape recovery |

### Status

[OK] **Completed**


## Session 94: 修复待处理媒体手动匹配加载失败
<!-- trellis-session: v=2 fp=f72092c28786a3db -->

**Date**: 2026-09-05
**Task**: 修复待处理媒体手动匹配加载失败
**Branch**: `main`

### Summary

任务中心手动匹配直接使用待处理记录的媒体 ID 与标题，避免依赖 metadata 详情加载。

### Git Commits

| Hash | Message |
|------|---------|
| `d8bb6f8` | fix: open manual scrape without metadata view |

### Status

[OK] **Completed**


## Session 95: 详情页显示选中媒体版本信息
<!-- trellis-session: v=2 fp=4e6c7ee80bebb152 -->

**Date**: 2026-09-05
**Task**: 详情页显示选中媒体版本信息
**Branch**: `main`

### Summary

详情页在国家地区与语言下方显示当前选中媒体版本的 Media ID 和本地路径，并在版本切换期间保持信息准确。

### Git Commits

| Hash | Message |
|------|---------|
| `895df25` | feat: show selected media details |

### Status

[OK] **Completed**


## Session 96: 详情页显示 STRM 真实路径
<!-- trellis-session: v=2 fp=f345a9fb18efb78a -->

**Date**: 2026-09-05
**Task**: 详情页显示 STRM 真实路径
**Branch**: `main`

### Summary

新增独立 STRM 目标读取接口并校验媒体可见性；详情页仅对当前选中的 STRM 版本实时请求和展示目标路径。

### Git Commits

| Hash | Message |
|------|---------|
| `e32ac25` | feat: show STRM target on media details |

### Status

[OK] **Completed**


## Session 97: 删除 STRM 本地目标
<!-- trellis-session: v=2 fp=3b305b34df11a833 -->

**Date**: 2026-09-05
**Task**: 删除 STRM 本地目标
**Branch**: `main`

### Summary

在详情页增加管理员 STRM 本地目标删除；支持 URL 路径映射、动态父目录确认与安全根校验，保留 sidecar 和 Media 记录。

### Git Commits

| Hash | Message |
|------|---------|
| `9904a7a` | feat: add safe STRM target deletion |

### Status

[OK] **Completed**


## Session 98: 详情页未关联豆瓣入口
<!-- trellis-session: v=2 fp=5f238629fc1e576c -->

**Date**: 2026-09-05
**Task**: 详情页未关联豆瓣入口
**Branch**: `main`

### Summary

未关联豆瓣时展示提示态图标；管理员点击复用元数据编辑弹窗设置豆瓣 ID，普通用户只读；保留已关联外链行为。

### Git Commits

| Hash | Message |
|------|---------|
| `b1b88f9` | feat: add missing Douban link entry |

### Status

[OK] **Completed**


## Session 99: 外部 ID 搜索与豆瓣状态区分
<!-- trellis-session: v=2 fp=3b53609268804ca4 -->

**Date**: 2026-09-05
**Task**: 外部 ID 搜索与豆瓣状态区分
**Branch**: `main`

### Summary

元数据编辑弹窗为 TMDb、Bangumi、豆瓣和 TheTVDB 增加按当前标题搜索入口；豆瓣未关联改用灰色断链图标，与黄色不完整状态区分。

### Git Commits

| Hash | Message |
|------|---------|
| `99b012b` | feat: add external metadata search links |

### Status

[OK] **Completed**


## Session 100: 豆瓣候选搜索并回填 ID
<!-- trellis-session: v=2 fp=13b148c299e05b60 -->

**Date**: 2026-09-05
**Task**: 豆瓣候选搜索并回填 ID
**Branch**: `main`

### Summary

复用手动刮削搜索链路返回多个豆瓣候选，在元数据编辑弹窗中选择并回填豆瓣 ID，保留原保存与详情外链行为。

### Git Commits

| Hash | Message |
|------|---------|
| `2b92b97` | feat: add Douban candidate ID picker |

### Status

[OK] **Completed**


## Session 101: 保留媒体列表筛选状态
<!-- trellis-session: v=2 fp=c3437c1b16e3e236 -->

**Date**: 2026-09-05
**Task**: 保留媒体列表筛选状态
**Branch**: `main`

### Summary

将无海报和无中文名筛选保存到媒体库 URL，并让详情页返回恢复完整媒体库来源地址；完成前端 lint、构建及独立复核。

### Git Commits

| Hash | Message |
|------|---------|
| `56f371a` | fix: preserve media library filters on return |

### Status

[OK] **Completed**


## Session 102: 修复纯数字豆瓣搜索回退
<!-- trellis-session: v=2 fp=af45d7119e0845c5 -->

**Date**: 2026-09-05
**Task**: 修复纯数字豆瓣搜索回退
**Branch**: `main`

### Summary

拒绝无标题豆瓣详情匹配，使无效裸数字 ID 回退到关键词候选搜索，并补充回归测试。

### Git Commits

| Hash | Message |
|------|---------|
| `6c44adc` | fix: fall back from invalid Douban numeric IDs |

### Status

[OK] **Completed**


## Session 103: 信任 /mnt/all STRM 删除根目录
<!-- trellis-session: v=2 fp=f579a7fd6cc5b4e8 -->

**Date**: 2026-09-05
**Task**: 信任 /mnt/all STRM 删除根目录
**Branch**: `main`

### Summary

将固定目录 /mnt/all 纳入 FileManager 可信根，保留现有路径、符号链接与普通文件安全校验，并完成后端测试与独立复核。

### Git Commits

| Hash | Message |
|------|---------|
| `087e65a` | fix: trust /mnt/all for STRM deletion |

### Status

[OK] **Completed**
