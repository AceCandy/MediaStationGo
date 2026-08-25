# 设计 metadata 图片补齐与硬删除语义

## Goal

为 canonical metadata 建立可持续、可恢复的图片完整性机制：无需依赖单个 `media` 文件，能够快速分页发现带 TMDB 标识但缺少实体自有本地图片的 metadata，复用现有后台任务补齐并可靠重试；同时把本功能涉及且没有回收站价值的 metadata 图软删除改为硬删除，消除无效 `deleted_at` 状态和查询分支。

## Background

- 图片权威关联位于 `metadata_items -> metadata_artworks -> artwork_assets`，TMDB 身份位于 `metadata_identifiers`；`media` 只是可播放文件，不是补图主体。
- 当前首次刮削和发现页 catalog hydration 能下载图片，但没有面向全部 canonical metadata 的缺图巡检。
- 当前所有域模型统一嵌入带 `gorm.DeletedAt` 的 `Base`。`metadata_items` 合并重复实体时已经硬删除源记录，但 identifier、provider snapshot 和 artwork selection 仍存在软删除恢复逻辑。
- catalog hydration job 以 `(provider, entity_kind, external_id)` 唯一；completed job 当前不会重新入队，Series 子项也只按完整 catalog checkpoint 遍历，无法直接承担历史缺图回填。
- 用户确认 metadata 入库后应原地更新；身份纠正、图片选择清除和重复实体合并应使用硬删除，不需要回收站语义。

## Product Decisions

- 第一版补齐全部实体自有图片：Movie/Series 的 poster、backdrop，Season 的 poster，Episode 的 still；禁止使用父级图片冒充实体自有图片已完成。
- 高频缺图巡检只使用已有 TMDB catalog hydration 能力；豆瓣只通过独立、默认关闭的低速电影补齐任务运行，不新增第二套 provider 队列。
- `catalog_artwork_hydrated_at` 继续作为实体级负 checkpoint：非空表示要求的图片已存在或 TMDB 明确无图。定时任务只处理 checkpoint 为空且确实缺图的项。
- TMDB 后续新增图片的周期复查不进入第一版，不增加按图片类型的 suppression 状态。
- 定时任务默认关闭，默认周期 24 小时；管理员可在现有任务中心手动执行、启用并调整周期。
- 同一补图任务先有界巡检已被选择的本地资产；本地文件确实缺失时清除相关选择并重新开放图片 checkpoint，再进入现有 TMDB 缺图排队。
- 孤儿 `artwork_assets` 及磁盘文件 GC 不纳入本任务；共享资产仅在未来确认零引用后才能删除。

## Requirements

### R1. Metadata-owned artwork backfill

- 缺图发现、任务状态、provider 获取和图片落库必须以 canonical metadata 为单位。
- 核心补图流程不得读取或更新 `media` 才能成立。
- 同一 metadata 被多个媒体版本引用时只能产生一次 provider 请求和一次图片选择。
- 发现页与媒体库引用同一 metadata 时必须共享同一图片结果。

### R2. Accurate missing-artwork classification

- 诊断查询只返回具有自身有效 TMDB identifier、`catalog_artwork_hydrated_at IS NULL` 且缺少实体自有图片的 metadata。
- 调度按 Movie/Series 顶层 root 去重；Series 只要自身或具有 TMDB identifier 的 Season/Episode 后代存在候选，就只产生一个 root job。
- 有效图片必须同时存在 `metadata_artworks` 选择和对应 `artwork_assets` 资产。
- 后台巡检还必须验证已选择资产的 `storage_key` 对应本地文件存在且可读；文件缺失时，同一共享资产的全部 selection 均失效。
- TMDB 明确没有图片路径时写入负 checkpoint，不进入快速重试；网络、限流、下载或存储失败时不写 checkpoint，并进入持久化退避重试。
- 已有任意来源的有效图片选择不得被 catalog hydration 覆盖，包括并发发生的本地或手工选择。
- 元数据编辑不允许修改 poster/backdrop URL；图片 selection 为空即视为可由 provider 或本地候选补齐的缺图状态。

### R3. Bounded background execution

- 复用现有 scheduler、task tracker、catalog hydration job/worker、ArtworkStore 和 TMDB provider；不得新增第二套通用调度、队列表或图片存储框架。
- 巡检使用 metadata ID keyset 分页和固定批次上限，不得一次加载全部候选，不得按每个 `media` 版本重复扫描。
- completed job 只在候选仍缺图时原子重排为 pending；pending、running、retry job 不得被重置或绕过退避。
- provider 请求继续使用现有 catalog worker 的串行化、超时和指数退避；一次 job 生命周期内重试达到固定上限后进入 terminal failed，只有手动触发可重置。
- 进程重启后 running job 必须恢复为 retry，未完成任务继续执行。
- 后台运行必须可观察：巡检任务记录扫描 root、缺图候选、创建/重排和并发未变更数量；catalog task/job 记录成功补齐、已有图片、来源无图、重试和 terminal failure。
- 本地资产巡检使用 asset ID keyset 和固定上限，并记录扫描资产、缺失文件和清除 selection 数量；权限、路径或其他文件系统错误必须失败退出，不得误清选择。
- 定时任务之外必须保留手动触发能力，便于首次迁移和故障恢复。

### R4. Hard-delete metadata graph semantics

- 本任务移除 `deleted_at` 的精确范围为 `media`、`metadata_items`、`metadata_identifiers`、`metadata_provider_snapshots`、`catalog_hydration_jobs`、`metadata_artworks`、`artwork_assets` 和 `metadata_credits`。
- `metadata_items` 仅在重复实体合并并完成全部引用迁移后硬删除源记录；正常 provider 变化原地更新。
- 被替换或清除的 `metadata_identifiers`、`metadata_provider_snapshots` 和 `metadata_artworks` 必须硬删除，不得再恢复 tombstone。
- 图片选择替换或清除不得立即删除可能仍被其他 metadata 引用的 `artwork_assets`。
- 历史软删除 metadata/关系行必须在删除 `deleted_at` 列前安全处理；存在未迁移引用时迁移必须失败，不得盲目 cascade。
- 历史软删除 artwork asset 保留为普通资产，待未来零引用 GC 统一处理，避免数据库事务与文件删除不一致。
- 用户收藏/历史/播放列表、`people` 与人物 identifier 等不在上述精确范围内的表保持现有删除语义。
- 新增的豆瓣候选图片关系直接使用 `PermanentBase` 和硬删除语义，不引入新的 tombstone。

### R5. Compatibility and safety

- 不改变现有媒体展示 URL、Emby 图片接口和首次刮削行为；元数据编辑移除 poster/backdrop URL 输入与提交字段，后端元数据更新契约不再处理这两个字段。
- 可信电影关联的豆瓣详情必须按 metadata/provider 保存原始 JSONB 快照；搜索候选不保存。
- metadata merge、identifier 替换、图片清空与重新选择在迁移后必须继续满足现有唯一性约束。
- schema 迁移必须在事务内执行、可重复运行，并在任何引用异常时安全失败；失败不得删除当前有效图片选择或资产文件。
- 删除依赖 `deleted_at IS NULL` 的 metadata 索引后必须重建等价的普通/部分业务索引。
- 物理删列前必须具备数据库备份；代码回滚时可重新添加兼容列和旧索引后部署旧版本。

### R6. Douban secondary-source enrichment

- TMDB 仍作为主数据源；Movie canonical metadata 一旦存在可信的 Douban identifier，即可独立查询并幂等保存豆瓣详情快照，不要求豆瓣成为本次主匹配来源。
- 豆瓣原始详情响应必须完整保存在 `metadata_provider_snapshots.payload` JSONB 中；字段投影不得丢弃或改写原始快照。
- 豆瓣只补 canonical 当前缺失的标题、原名、简介、评分、年份、上映日期、语言、国家和类型；标题是例外：当前标题不含中文且豆瓣提供中文标题时，允许使用豆瓣标题。`source`、NSFW 和 provider identity 不由豆瓣辅助数据改写。
- 豆瓣海报立即下载为本地候选资产；已有有效 poster selection 时只保存候选、不改变当前选择，当前 selection 缺失时才原子提升豆瓣候选。
- 豆瓣详情或图片请求失败不得破坏已经成功保存的 TMDB canonical 数据；补齐流程必须可重试。
- 搜索候选、发现页展示结果仍不得仅因展示而写入快照或改变 canonical 数据。
- 豆瓣第二数据源只允许补齐 Movie；Series、Season、Episode 永不创建豆瓣快照或豆瓣图片关联。
- 一个 canonical metadata 同时存在多个豆瓣 identifier 时视为关联存疑，全部跳过，不选择其中任何一个。

### R7. Historical Douban enrichment scheduler

- 新增独立的慢速豆瓣补齐定时任务，默认关闭，并保留管理员手动触发能力。
- 任务只扫描已有可信 Douban identifier、尚无豆瓣快照或仍有允许补齐缺口的 Movie metadata；不得搜索或猜测新的豆瓣关联。
- 扫描必须按 metadata ID keyset 有界分页，provider 请求低速串行执行，并保留持久化 cursor；单次运行和单次请求速率必须有固定上限。
- 存疑关联必须记录跳过统计而不是重试；网络、限流和临时下载失败必须保留重试机会。
- 新入库或重新刮削获得可信 Douban identifier 时应走同一套幂等补齐逻辑，避免在线路径和历史任务产生两套融合规则。

## Acceptance Criteria

- [ ] AC1: 可在不连接 `media` 表的情况下按 metadata ID keyset 分页列出缺图项；每个 metadata 每种要求图片最多判定一次，且 Movie/Series/Season/Episode 使用各自图片类型。
- [ ] AC2: 调度查询按 Movie/Series root 返回，每个 root 最多一行；同一 metadata 即使被多个媒体版本引用，也只进入一个 catalog job。
- [ ] AC3: 已存在任意有效来源图片的类型不被自动覆盖；并发手工选择也由数据库原子条件保护。
- [ ] AC4: TMDB 返回图片时通过现有 ArtworkStore 落库；TMDB 明确无图时写 checkpoint 且不重试；网络/限流/存储失败时保留空 checkpoint 并按持久化任务退避。
- [ ] AC5: completed job 可安全重排；pending/running/retry 不被重置；重试耗尽进入 failed，进程重启可恢复 running，手动任务可恢复 failed。
- [ ] AC6: 任务支持默认关闭的 24 小时周期和手动触发，单次执行有扫描/入队上限，并在现有任务中心展示配置、状态、历史和统计。
- [ ] AC7: 精确范围内八张表不再包含或查询 `deleted_at`；media 删除及 identifier、provider snapshot、artwork selection、metadata credit 的替换/清除均为硬删除。
- [ ] AC8: metadata 合并仍先迁移 media、层级、收藏、播放历史、播放列表、图片和人物关系，再硬删除源 metadata；发现残留引用时迁移/合并失败。
- [ ] AC9: 图片选择替换或清除不会删除仍被引用的 artwork asset；本任务不删除任何孤儿图片文件。
- [ ] AC10: 现有首次刮削、发现页 hydration、手工图片编辑、媒体列表/详情和 Emby 图片读取相关测试通过。
- [ ] AC11: PostgreSQL 迁移测试覆盖 legacy tombstone 清理、引用保护、列/索引变化、活动数据保持和二次执行幂等。
- [ ] AC12: 生产规模数据上的候选查询提供 `EXPLAIN (ANALYZE, BUFFERS)` 结果；只有执行计划证明需要时才新增复合/部分索引。
- [ ] AC13: 已选择资产按 ID 有界检查本地文件；文件缺失会事务化清除所有相关 selection、清空对应 metadata 图片 checkpoint，并由同一次补图流程重新发现；其他文件系统错误不改变数据库。
- [ ] AC14: 可信电影关联的豆瓣详情保留完整原始响应并幂等写入 `metadata_provider_snapshots(provider='douban')`；搜索候选不入库，Series/Season/Episode 不入豆瓣快照。
- [ ] AC15: TMDB 主匹配的 Movie 关联到可信 Douban identifier 后也会保存豆瓣快照；允许字段的空值可由豆瓣补齐，非中文标题可替换为豆瓣中文标题，其他已有值保持不变。
- [ ] AC16: 豆瓣 poster 立即本地化并关联为候选；已有任意来源的有效 poster 不被覆盖，当前 selection 缺失时可原子提升候选，豆瓣失败不改变已成功入库的 TMDB 数据。
- [ ] AC17: 默认关闭的慢速豆瓣任务可有界补齐历史 Movie；不处理 Series/Season/Episode，不搜索新关联，一个实体存在多个豆瓣 ID 时统计为存疑并跳过。
- [ ] AC18: 在线入库和历史任务复用同一幂等补齐函数；重复执行不产生重复快照、identifier 或图片 selection，也不重复覆盖已经完整的 canonical 字段。
- [ ] AC19: 候选图片资产参与本地文件完整性巡检；候选文件缺失时移除失效候选关系并重新开放豆瓣补齐，不能把不存在的候选提升为当前 selection。
- [ ] AC20: 元数据编辑界面不展示或提交 poster/backdrop URL，后端元数据更新请求不能清空或替换图片 selection；文本字段和 provider ID 编辑保持可用。

## Out of Scope

- 不为 metadata、identifier 或 artwork selection 提供回收站/撤销界面。
- 不重新设计图片文件格式、内容哈希存储或图片代理。
- TMDB 缺图任务不因为缺图而重新匹配作品或修改非图片元数据；豆瓣电影任务只按 R6 的固定主从规则补齐。
- 不周期复查已经写入图片负 checkpoint 的 metadata。
- 不把没有 Douban identifier 的 metadata 重新搜索或猜测关联到豆瓣。
- 不新增字段来源表；补齐规则使用固定、可测试的主从优先级。
- 不清理孤儿 artwork asset 或磁盘文件。
- 不逐张解码校验图片内容；本次只验证本地文件存在且可读。
- 不改变人物实体、人物 identifier、收藏、历史和播放列表的删除模型。
- 不在规划阶段修改产品代码或启动任务实现。
