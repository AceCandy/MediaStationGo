# Metadata 搜索设计

## 目标边界

搜索以顶层逻辑作品为唯一实体：一条 Movie Metadata 或一条 Series Metadata
对应一个搜索文档、一个结果和一个分页计数。Season、Episode、未绑定
Metadata 的 Media 以及播放技术字段均不进入搜索索引。

Media 层只承担两项职责：

1. 在构建搜索投影时判断 Metadata 是否有可播放内容，并派生 `library_ids`。
2. 搜索命中 Metadata 后加载当前用户可见的播放版本。

搜索查询、搜索后端接口和 OpenSearch 文档均不暴露 Media ID 或 Media 字段。

## 文档模型

OpenSearch 每个文档使用 Metadata ID 作为 `_id`，只保存搜索和过滤需要的字段：

```text
id
kind                 movie | series
title
original_name
overview
genres                MetadataItem.genres 原始文本
nsfw
library_ids          派生权限投影，去重后的 keyword 数组
```

不保存 `media_id`、`path`、`scan_title`、播放地址、媒体流、清晰度或文件信息。
文档存在即表示该 Metadata 当前具备至少一个全局可播放内容，不再保存
`has_media` 或 `deleted` 标志。

## 索引资格与 Library 投影

### Movie

- 仅 `kind=movie` 且至少有一条未删除 Media 直接引用该 Metadata 时建立文档。
- `library_ids` 是这些未删除 Media 的去重 `library_id` 集合。
- 集合为空时删除或不建立文档。

### Series

- 仅 `kind=series` 且至少有一个后代 Episode 被未删除 Media 引用时建立文档。
- 关系固定为 `Series -> Season -> Episode -> Media`。
- `library_ids` 是全部可播放后代 Media 的去重 `library_id` 集合。
- 集合为空时删除或不建立文档。

Season 和 Episode 自身永不建立搜索文档。Series/Season/Episode 的无关键词浏览
仍使用现有 PostgreSQL 层级逻辑。

## 搜索后端契约

将现有返回 Media ID 的搜索契约改为返回 Metadata ID：

```text
SearchMetadataIDs(query, offset, limit, filter) -> metadata IDs, total
```

过滤条件至少包含：

- 字段范围：Web 或 Emby title-only。
- 顶层种类：Movie、Series 或两者。
- NSFW 可见性。
- 当前请求的有效可见媒体库 IDs。
- 是否启用媒体库限制；启用且有效 ID 集合为空时直接返回空结果。

Web 字段范围为 `title`、`original_name`、`overview`、`genres`；Emby 字段范围
仅为 `title`、`original_name`。两者都要求多关键词 AND。

用户存在媒体库限制时，服务层先计算“允许库与非隐藏库”的有效交集，再以
OpenSearch `terms library_ids` 过滤。数组匹配语义是至少一个可见库相交，因此
同一 Metadata 同时存在于可见库和隐藏库时仍可命中。

过滤结构必须区分“未限制媒体库”和“已限制但有效集合为空”；后者在
OpenSearch 与 PostgreSQL 都返回空，不能沿用现有 `len(ids)==0` 即跳过过滤的
语义。查询始终显式限制 `kind IN ('movie','series')`，防止旧或脏文档混入。

## 查询流程

```text
关键词
  -> 规范化与权限范围
  -> OpenSearch Metadata 查询
       -> 成功：Metadata IDs + 逻辑总数
       -> 未配置、未就绪或失败：PostgreSQL Metadata 回退
  -> PostgreSQL 按 Metadata ID 复核当前可见播放内容
  -> 按搜索 ID 顺序加载代表卡片/播放版本
  -> Web 或 Emby 各自组装响应
```

PostgreSQL 回退使用与索引资格相同的 Movie/Series EXISTS 关系和
`library_ids` 可见性语义，先按 Metadata 分组、排序和分页，再加载 Media，不能
先分页 Media 后去重。

OpenSearch 是最终一致投影。命中后数据库复核会阻止已失去可播放内容或已失去
权限的 Metadata 返回；发现陈旧文档时触发最佳努力修复。同步窗口内页数或总数
可能短暂偏大，但不会返回无权访问或无播放内容的作品。

Movie ID 通过直接 Media 绑定加载播放版本；Series ID 必须通过
`Series -> Season -> Episode -> Media` 加载后代播放版本。不能使用只匹配
`media.metadata_id IN (...)` 的直接加载方法。加载后按 OpenSearch/SQL 返回的
Metadata ID 顺序组装 Movie 或 Series 卡片，保持相关度顺序。

## Web 行为

- `/media?q=...` 的请求和响应 JSON 保持不变。
- 搜索只返回顶层 Movie/Series 卡片。
- OpenSearch 和 PostgreSQL 回退均按 Metadata 粒度分页。
- 不再通过 Media 路径或扫描标题命中作品。
- 命中后加载可见 Media 版本供现有卡片与播放流程使用。

## Emby 行为

- `/Items` 与 `/SearchHints` 的路由、参数和响应结构保持不变。
- 非空 `SearchTerm` 只搜索顶层 Movie/Series Metadata。
- `IncludeItemTypes` 只保留 Movie/Series 交集；只请求 Season/Episode 时返回空搜索
  结果，不把 `SearchTerm` 忽略为普通浏览。
- Library `ParentId` 转为 `library_ids` 范围；Series/Season `ParentId` 携带
  `SearchTerm` 时返回空结果。
- 无 `SearchTerm` 时继续使用现有 Series -> Season -> Episode 浏览路径。
- PersonIds、收藏、继续播放等无法完全在 OpenSearch 表达的组合条件直接使用
  PostgreSQL Metadata 搜索，避免先分页后过滤导致错误总数。

`Items` 必须在现有 `findSeasonGroup`、`findSeriesGroup` 和混合电影库分支之前
处理非空 `SearchTerm`：Library ParentId 进入顶层 Metadata 搜索；Series/Season
ParentId 或只请求 Season/Episode 时立即返回空。这样不会落回当前 Episode
内存过滤或按 Media/扫描标题搜索的混合电影库路径。

## 同步与重建

提供一个集中投影方法：输入任意受影响 Metadata ID，解析其顶层 Movie/Series，
重新查询文档；有资格则 upsert，无资格则从 OpenSearch 删除。

触发范围：

- Media 新增或首次绑定 Metadata：刷新新顶层 Metadata。
- Media 换绑：同时刷新旧、新顶层 Metadata。
- Media 删除或按库清理：删除前收集受影响顶层 Metadata，提交后刷新。
- Metadata 内容更新：刷新自身或所属顶层 Series。
- Metadata 父子关系变化/删除：刷新旧、新顶层 Series。

索引名称迁移到新的 metadata 专用 alias，避免旧 Media 文档与新 Metadata 文档
混用。全量回填写入新的版本化 concrete index；alias 不存在时业务自动走
PostgreSQL 回退。回填期间增量变更写入当前 active index，并记录受影响的顶层
Metadata ID；全量扫描后对新 index 重放这些 ID，再原子切换 alias。进程在切换前
退出时丢弃未挂 alias 的不完整 index，现有 alias 不受影响。

旧 Media 索引不自动删除。新 alias 首次成功切换表示持久化就绪；服务重启后通过
alias 存在和 mapping schema 版本判断是否可接管搜索。周期重建使用同一套新
concrete index + 原子切换流程，避免查询半成品索引。

## 回滚与风险

- 新旧索引使用不同名称，回滚代码即可重新使用旧配置；旧索引清理由运维确认后
  单独执行。
- Media 变更与 OpenSearch 写入不是同一事务，数据库复核和周期全量回填是安全
  兜底。
- Web 路径/扫描标题搜索被移除是明确行为变化，需要回归搜索页面和提示结果。
- 现有已实现的 Episode SearchTerm 过滤与新范围冲突，实施时应删除对应搜索分支
  和测试，只保留无关键词层级浏览测试。
