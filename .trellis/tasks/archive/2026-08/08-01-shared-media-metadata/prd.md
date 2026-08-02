# 共享媒体元数据与持久化图片

## Goal

将电影、剧集、季、单集元数据从媒体文件记录中拆出并共享，所有媒体库、搜索、播放器和 Emby 展示都读取共享元数据；选中的封面保存到系统持久化图片库，不依赖远程 URL 或媒体目录边车文件。

## Background

- 当前 `media` 同时保存文件事实、展示元数据、外部 ID 和图片 URL，同一作品的不同路径会重复保存元数据。
- 当前刮削在 provider 前读取本地 NFO，并可能用本地字段覆盖 provider 结果；刮削完成后还会向媒体目录写 NFO。
- 当前远程图片只进入可清理的 `CacheDir/images`，不能作为权威图片存储。
- 项目尚未上线，本次不迁移或回填旧数据，开发数据库可以重建。

## Requirements

### R1. 共享元数据

- `Media` 只保存文件事实、媒体库归属、扫描识别提示、刮削状态及共享元数据关联。
- 电影、剧集、季和单集都使用内部 metadata UUID；TMDb、豆瓣、Bangumi、TheTVDB 仅作为带 provider 和实体类型的外部标识。
- 外部标识写入前必须规范化 provider、实体类型和 ID；同一 provider ID 不得因大小写、空白或数字格式差异产生重复记录。
- 剧集层级固定为 `series -> season -> episode -> media`；Season 必须是可被单独收藏的真实 metadata 行，不能由 Emby 或查询层临时虚构。
- Season 身份为父 Series metadata ID 和季号；Episode 身份为父 Season metadata ID 和集号，不依赖 TMDb episode external ID。
- 每条 `media` 必须关联一个真实的 `metadata_items.id`，`media.metadata_id` 在数据库中非空；不存在未绑定 metadata 的待刮削媒体。
- 同一 metadata 可以关联多个不同路径的媒体文件；Emby 播放历史、收藏和播放列表关联 metadata ID，最后播放版本、具体播放源和重复文件关系保留 media ID。
- `NSFW`、标题、简介、评分、年份、类型、国家、语言和外部 ID 属于共享元数据。

### R2. 来源优先级

- provider 明确匹配成功时，只保存 provider 元数据，不混入本地 NFO 或边车封面字段。
- provider 明确返回无匹配时，才只读导入已有本地 NFO 和边车封面。
- provider 请求错误、超时或解析失败不是无匹配；应记录错误并等待重试，不能静默切换成本地来源。
- 本地 NFO 中明确的 provider ID 可以作为 provider 查询提示，但其其他字段不能覆盖成功的 provider 结果。
- 没有 provider ID 的本地电影创建独立 metadata；本地剧集单集按本地系列身份、季号和集号共享。

### R3. 持久化图片

- provider 或本地边车提供的选中图片必须复制到 `App.DataDir` 下的持久化图片库。
- 图片按内容 SHA-256 去重；数据库保存资产 ID、相对存储键、MIME、尺寸和大小。
- 远程 URL 或原始本地路径只记录来源，不作为播放器和前端的运行时依赖。
- 对外图片地址必须是系统内部 artwork 地址；缩略图等可重建派生物才允许放入 CacheDir。

### R4. 统一读取

- 媒体列表、详情、搜索、最近媒体、版本分组、系列/季/单集、历史、收藏、播放器和 Emby 必须通过统一 MediaView 读取共享元数据和持久化图片。
- MediaView 对外尽量保持当前平铺 JSON 字段名，避免无关的客户端协议变更。
- 列表过滤、NSFW 权限、排序、分页和搜索必须在数据库查询或搜索索引阶段使用共享元数据，不能分页后补齐。
- 播放路径、STRMURL、时长、编码和容器等文件事实继续来自 Media。

### R5. 媒体目录只读

- 元数据子系统不得创建、覆盖、移动或删除媒体目录中的 NFO、poster、fanart、thumb 等边车文件。
- 删除自动 NFO 写入、管理员 NFO 导出接口及整理器对 NFO 边车的搬移/删除行为。
- 现有本地 NFO 和封面仅作为 provider 无匹配时的只读导入来源。

### R6. 新安装模型

- 不实现历史回填、旧字段双写或旧数据库升级兼容。
- 新建数据库必须直接得到新结构；现有开发数据库由使用者自行重建。
- SQLite 和 PostgreSQL 都必须支持模型、约束和主要查询。

### R7. 基础 metadata 与媒体入库

- `metadata_items` 表示系统内部身份，不以 provider 刮削成功为创建前提；基础 metadata 可以只包含类型、标题、来源及剧集层级等最小字段，后续再由 provider 或用户补全。
- 发现媒体文件后，必须先创建或复用基础 metadata，再在同一事务中创建关联该 metadata 的 media；任一步失败时不得留下孤立 media。
- 路径、NFO 或用户输入包含可靠 provider ID 时，先按 `(provider, entity_kind, external_id)` 复用 metadata；未找到时创建带该标识的基础 metadata。
- TMDb 不是必需标识。只有豆瓣 ID 的电影以 `douban/movie/<id>` 作为外部身份，创建或复用 movie metadata，后续可在同一 metadata 上补充其他 provider 标识。
- 完全没有 provider ID 的扫描媒体也必须创建 `source=local` 的基础 metadata；不能仅凭标题和年份与既有作品自动合并，只有用户明确确认后才能归并。
- 手工创建作品时，先创建 `source=manual` 的 metadata 和用户提供的可选 provider 标识，再绑定媒体；没有任何外部 ID 也合法。
- provider 后续匹配到外部标识时：若该标识尚未属于其他 metadata，则直接补充到当前 metadata；若该标识已经属于既有 canonical metadata，则在同一事务中将 media、收藏、播放列表等引用迁移到既有 metadata，处理唯一键重复后，再删除已无引用的临时 metadata。
- metadata 归并不得先删除源 metadata；电影归并完成后只能保留一个 canonical metadata。Series 归并还必须按层级归并其 Season、Episode 和 media，不能产生断开的父子关系。
- provider 明确返回的跨 provider 外部 ID 映射可以自动触发上述归并；仅由标题、年份或相似度推测出的候选不得自动归并，必须由用户确认。

## Acceptance Criteria

- [ ] 两个不同路径的同一电影关联同一 metadata，但保留独立的路径、大小、编码、播放状态和 media ID。
- [ ] Emby 中同一 metadata 的多个版本共享收藏、已看和续播状态，每个 MediaSource 仍使用独立 media ID。
- [ ] 两个路径的同一单集共享同一 episode metadata；同剧不同集不共享 episode metadata。
- [ ] 每个 Series 的 Season 都是可查询、可收藏的真实 metadata；Emby 不生成虚拟 Series 或 Season ID。
- [ ] 任意已持久化 media 的 `metadata_id` 均非空且指向现存 metadata；扫描或手工入库失败不会留下孤立 media。
- [ ] 只有豆瓣 ID、没有 TMDb ID 的电影可正常创建、展示和绑定媒体，且豆瓣标识可用于复用同一 metadata。
- [ ] provider 中不存在的作品可先手工创建 metadata，再绑定媒体，并在没有外部 ID 时正常使用。
- [ ] 本地 metadata 后续匹配到一个已存在的 TMDb metadata 时，全部媒体及用户关系迁移到既有 metadata，临时 metadata 被删除且没有悬空引用或重复用户关系。
- [ ] 豆瓣 metadata 获得 provider 明确返回的 TMDb ID 后：TMDb ID 未占用时补到原 metadata；已属于其他 metadata 时自动安全归并。
- [ ] 标题、年份或相似度命中的候选只生成待确认结果，未经用户确认不会改变 metadata 绑定。
- [ ] 相同数字 ID 的 TMDb movie 和 series 不冲突；不同 provider 的相同数字 ID 不冲突。
- [ ] 同一 metadata 可以同时关联 TMDb、豆瓣、Bangumi、TheTVDB 标识；重复写入等价标识保持幂等，冲突标识不会被静默改绑。
- [ ] provider 成功时本地 NFO 的标题、简介和封面不会覆盖 provider 结果。
- [ ] provider 明确无匹配时，本地 NFO 元数据和封面会进入共享表与持久化图片库。
- [ ] provider 错误时不会导入本地资料，也不会错误标记为 no-match。
- [ ] 选中图片存在于 DataDir artwork 目录；清空 CacheDir 或远程图片失效后仍能展示。
- [ ] 媒体列表、搜索、播放器和 Emby 对同一 metadata 的不同媒体展示相同共享标题、NSFW 和 artwork。
- [ ] 修改共享 metadata 后，所有关联媒体视图、搜索索引和 Emby 输出同步更新。
- [ ] 媒体目录不可写时，扫描、provider 刮削、本地回退、展示和播放仍可完成。
- [ ] 全流程不会创建、修改、移动或删除媒体目录中的 NFO 和边车图片。
- [ ] SQLite 聚焦测试通过；PostgreSQL 至少完成模型/SQL 静态核验，若环境存在测试 DSN 则执行集成测试。

## Out of Scope

- 历史数据库迁移和旧数据回填。
- 每种 artwork 保存多个候选、多语言海报或图片编辑功能。
- 演职员、工作室等当前系统尚未建模的新元数据类型。
- 向媒体目录导出或同步 Emby/Kodi 边车文件。
- 修改媒体文件整理、转码和播放协议本身。
