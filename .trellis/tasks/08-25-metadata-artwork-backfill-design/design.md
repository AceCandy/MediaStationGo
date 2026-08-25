# Metadata 图片补齐与硬删除技术设计

## 1. 设计摘要

本方案不创建新的图片抓取系统。现有能力已经具备持久化 catalog job、TMDB 详情获取、图片下载、内容哈希存储、失败退避和任务中心展示，缺少的是三段连接能力：

1. 有界检查已选择资产的本地文件，先把物理缺失恢复成数据库可见的缺图状态。
2. 从 canonical metadata 图分页发现真正缺图的实体，并按 Movie/Series root 去重。
3. 将历史 completed catalog job 安全重排，同时让 Series job 能重新访问仅缺图片的 Season/Episode。
4. 把本流程涉及的无回收站 metadata 表改成硬删除，移除查询中的 tombstone 分支。
5. 对已有唯一豆瓣标识的 Movie 低速补齐原始快照、缺失字段和本地候选海报，不影响 TMDB 主数据及当前图片选择。

核心边界如下：

```text
Scheduler / Task Center
        │
        ▼
metadata 缺图巡检（有界 pass）
        │  只读 canonical metadata graph
        ▼
catalog_hydration_jobs（复用、原子创建/重排）
        │
        ▼
现有 catalog hydration worker
        │
        ├─ TMDB details / image URL
        ├─ ArtworkStore / ImageProxy
        └─ metadata_artworks -> artwork_assets

media ──────────────────────────────── 不在核心链路中
```

## 2. 架构边界

### 2.1 核心拥有关系

- `metadata_items` 拥有作品、季、集的 canonical 状态和 catalog checkpoints。
- `metadata_identifiers` 提供 provider 身份；本任务只调度 TMDB root。
- `metadata_artworks` 表示每个 metadata、每种图片类型的当前选择。
- `metadata_artwork_candidates` 表示已本地化但尚未成为当前选择的 provider 候选图片。
- `artwork_assets` 表示按内容哈希共享的本地原图资产。
- `catalog_hydration_jobs` 是恢复和重试的唯一权威；TaskExecution 只负责可观察性。
- `media` 仅引用 metadata。它不参与候选发现、去重、下载或图片写入。

### 2.2 复用与不新增

复用：

- `SchedulerService` 的 configurable job。
- task definition、手动运行、周期配置、历史和日志接口。
- `ScraperService` 的 catalog hydration worker。
- `CatalogHydrationJob` 的 pending/running/retry/completed 状态和启动恢复。
- `ArtworkStore`、`ImageProxy`、内容哈希资产去重。
- TMDB Movie/TV/Season/Episode 获取能力。
- 豆瓣详情 provider、provider snapshot upsert、scheduler/task tracker 和内部 setting cursor。

不新增：

- 不新增第二张通用任务表或第二个 worker 池。
- 不新增专用 REST handler 或前端页面。
- 不新增豆瓣队列表或 worker 池；豆瓣 Movie 补齐由独立、默认关闭的低速 scheduler pass 串行执行。
- 不新增按图片类型的 suppression 表。
- 不新增孤儿文件 GC。

## 3. 图片完整性契约

### 3.1 实体与图片类型

| metadata kind | 实体自有图片 | TMDB 获取位置 |
| :--- | :--- | :--- |
| Movie | poster、backdrop | movie details |
| Series | poster、backdrop | TV details |
| Season | poster | season details |
| Episode | still | episode details |

父级 poster/backdrop 可以继续用于既有展示回退，但不得让子实体在完整性检查中被判定为“已有自有图片”。

### 3.2 “有效图片”和“缺图”

数据库完整性查询认为某图片类型有效，当且仅当：

```text
存在 metadata_artworks(metadata_id, artwork_type)
且其 asset_id 对应一条 artwork_assets 记录
```

硬删除迁移完成后两张表均无 `deleted_at`，因此不再需要软删除过滤。对外展示还要求 `artwork_assets.storage_key` 对应的 DataDir 文件存在且可读；数据库不能检查文件系统，因此补图 pass 在执行候选 SQL 前单独做有界资产巡检。

metadata 成为候选必须同时满足：

1. kind 在 Movie/Series/Season/Episode 范围内。
2. 自身存在 `provider='tmdb' AND entity_kind=kind` 的 identifier。
3. `catalog_artwork_hydrated_at IS NULL`。
4. 至少一种实体自有图片没有有效 selection + asset。

### 3.3 checkpoint 语义

`catalog_artwork_hydrated_at` 保持实体级 checkpoint，不增加逐类型状态：

- 所有适用图片已有有效选择：写 checkpoint。
- TMDB 对缺少的适用类型明确返回空路径：写 checkpoint。
- 已有本地/手工图片：视为该类型已满足，不覆盖；其他类型处理完成后写 checkpoint。
- 网络、429、下载、解码、磁盘或数据库失败：不写 checkpoint。
- 元数据编辑不再提供 poster/backdrop URL 修改能力；selection 为空即为缺图，不需要用户 suppression 状态。

这个 checkpoint 同时承担负结果和用户已处理语义。因此第一版不会在 TTL 到期后自动清空它，也不会周期复查 TMDB 后来新增的图片。

## 4. 缺图查询设计

### 4.0 本地资产完整性预检

Repository 按 `artwork_assets.id` keyset 分页列出仍被 `metadata_artworks` 引用的资产，一页 200，单次 pass 最多检查 1,000 个。内部 setting `internal.metadata_artwork_integrity_cursor` 保存跨 pass cursor，跑到末尾后归零，保证有界执行最终覆盖全部资产。ArtworkStore 使用现有 `pathForStorageKey` 解析受控路径并尝试打开文件：

- 文件存在且可读：保持 selection。
- `os.IsNotExist`：事务内收集所有引用该 asset 的 metadata ID，清空其 `catalog_artwork_hydrated_at`，硬删除这些 selection；保留 asset 行和其他磁盘文件。
- 路径非法、权限不足或其他 I/O 错误：终止本次 pass，数据库不变。

共享 asset 的文件只检查一次；缺失时全部引用同时失效。事务完成后，后续 root 缺图查询在同一次 pass 中自然发现具有 TMDB identity 的 metadata。图片内容解码验损和孤儿 asset/file GC 不属于此流程。

### 4.1 诊断查询：逐 metadata 分页

Repository 提供按 `metadata_items.id` 的 keyset 分页方法，返回 metadata ID、kind、TMDB ID 和各适用类型的 missing 布尔值。查询不连接 `media`，不使用 offset，不使用 `DISTINCT`。

SQL 形状：

```sql
SELECT
    mi.id,
    mi.kind,
    (
        SELECT mid.external_id
        FROM metadata_identifiers AS mid
        WHERE mid.metadata_id = mi.id
          AND mid.provider = 'tmdb'
          AND mid.entity_kind = mi.kind
        ORDER BY mid.id
        LIMIT 1
    ) AS tmdb_id,
    mi.kind IN ('movie', 'series', 'season')
      AND NOT EXISTS (
          SELECT 1
          FROM metadata_artworks AS ma
          JOIN artwork_assets AS aa ON aa.id = ma.asset_id
          WHERE ma.metadata_id = mi.id
            AND ma.artwork_type = 'poster'
      ) AS missing_poster,
    mi.kind IN ('movie', 'series')
      AND NOT EXISTS (
          SELECT 1
          FROM metadata_artworks AS ma
          JOIN artwork_assets AS aa ON aa.id = ma.asset_id
          WHERE ma.metadata_id = mi.id
            AND ma.artwork_type = 'backdrop'
      ) AS missing_backdrop,
    mi.kind = 'episode'
      AND NOT EXISTS (
          SELECT 1
          FROM metadata_artworks AS ma
          JOIN artwork_assets AS aa ON aa.id = ma.asset_id
          WHERE ma.metadata_id = mi.id
            AND ma.artwork_type = 'still'
      ) AS missing_still
FROM metadata_items AS mi
WHERE mi.id > $1
  AND mi.catalog_artwork_hydrated_at IS NULL
  AND mi.kind IN ('movie', 'series', 'season', 'episode')
  AND EXISTS (
      SELECT 1
      FROM metadata_identifiers AS mid
      WHERE mid.metadata_id = mi.id
        AND mid.provider = 'tmdb'
        AND mid.entity_kind = mi.kind
  )
  AND (
      (mi.kind IN ('movie', 'series', 'season') AND NOT EXISTS (... poster ...))
      OR (mi.kind IN ('movie', 'series') AND NOT EXISTS (... backdrop ...))
      OR (mi.kind = 'episode' AND NOT EXISTS (... still ...))
  )
ORDER BY mi.id
LIMIT $2;
```

实现时复用同一组 SQL predicate 构造 SELECT 布尔值和最终过滤条件，避免“列表显示缺图但调度不认”的语义漂移。方法返回一个 metadata 一行；因此同一类型不会重复，多个缺失类型也不会破坏 ID keyset。

### 4.2 调度查询：逐 Movie/Series root 分页

调度不能直接为 Season/Episode 创建 job，因为现有 job contract 只接受 Movie/Series。调度查询先用与诊断查询相同的 candidate predicate 得到缺图 metadata，再通过层级映射到 root：

```text
Movie   -> self
Series  -> self
Season  -> parent Series
Episode -> parent Season -> parent Series
```

外层从 `metadata_items AS root` 查询 Movie/Series，并使用 `EXISTS` 判断该 root 是否拥有至少一个候选 self/descendant。这样天然一 root 一行，不需要 `DISTINCT`。root 自身必须有有效 TMDB identifier。

查询还排除已有活动 job：

- scheduled pass 排除 pending、running、retry、failed；completed 可重新排队。
- manual pass 排除 pending、running、retry；failed 可由人工重新排队。

排序为 `root.id ASC`，固定 limit。每次 pass 从空 cursor 开始也不会被活动 job 长期挡住，因为活动/failed job 已在 SQL 中排除；下一次运行继续选择尚未入队的 root。

### 4.3 索引策略

先使用现有索引：

- `metadata_items` 主键和 `parent_id` 索引。
- `metadata_identifiers(metadata_id)`。
- `metadata_artworks(metadata_id, artwork_type)` 唯一索引。
- `artwork_assets(id)` 主键。
- `catalog_hydration_jobs(provider, entity_kind, external_id)` 唯一索引。

不先验新增索引。上线前在真实规模 PostgreSQL 数据上分别对诊断查询和 root 查询执行：

```sql
EXPLAIN (ANALYZE, BUFFERS, VERBOSE)
<query>;
```

重点检查 metadata 主扫描、identifier correlated lookup、artwork anti-join、后代 EXISTS 的循环次数和估算偏差。只有执行计划证明需要时，才考虑：

- `metadata_identifiers(metadata_id, provider, entity_kind)`。
- 以 `catalog_artwork_hydrated_at IS NULL` 为谓词的 metadata 部分索引。
- 面向 Series 后代查询的 `(parent_id, kind, id)`。

## 5. Scheduler 与任务中心

### 5.1 新任务定义

新增 scheduler job：

```text
job name: metadata_artwork_backfill
enabled setting: metadata.artwork_backfill_enabled
interval setting: metadata.artwork_backfill_interval_seconds
default enabled: false
default interval: 24h
task definition: 元数据图片补齐
action: scheduler
trigger: 定时 / 手动
```

现有任务中心会根据 task definition 和 `schedule_config` 通用渲染运行、周期配置、历史和日志；前端无需增加专用 action 或页面。

### 5.2 单次 pass 上限

使用代码常量限制单次扫描和入队数量，不新增配置项。初始建议：

- 每页扫描 200 个 root。
- 单次 pass 最多扫描 1,000 个 root。
- 单次 pass 最多创建/重排 200 个 job。
- 每页检查 200 个已选择 asset，单次 pass 最多检查 1,000 个。

这些值只控制数据库巡检和排队速度；真正 provider 请求仍由现有 catalog worker 串行处理。若真实数据证明吞吐不足，再调整常量，不为未证实需求增加配置。

### 5.3 pass 流程

```text
触发 scheduler job
      │
      ├─ 创建 TaskExecution（scheduled/manual）
      ▼
keyset 检查已选择 asset 的本地文件
      │  缺失 -> 清 selection + 清 artwork checkpoint
      ▼
keyset 查询缺图 root（排除活动 job）
      │
      ├─ 无候选 -> 完成任务
      │
      └─ 对每个 root 原子 ensure job
             ├─ absent -> pending
             ├─ completed -> pending
             ├─ failed + manual -> pending
             └─ active / failed + scheduled -> unchanged
      │
      ▼
只唤醒 catalog worker 一次
      │
      ▼
完成巡检 TaskExecution（不等待异步 job 全部结束）
```

巡检任务是“发现并排队”的观察记录，不假装同步完成图片下载。实际下载结果由每个 durable catalog job 和对应 catalog TaskExecution 记录。

## 6. Catalog job 状态机

### 6.1 状态扩展

在现有状态上增加 terminal `failed`：

```text
absent ── ensure ──> pending
pending/retry ── claim(SKIP LOCKED) ──> running
running ── success ──> completed
running ── failure, attempts < 8 ──> retry(next_attempt_at)
running ── failure, attempts >= 8 ──> failed
running ── process restart ──> retry(now)
completed ── 缺图巡检命中 ──> pending(root stage)
failed ── manual backfill ──> pending(root stage)
```

阶段从 root 进入 seasons 时保留 attempts；固定上限按从上次显式创建/重排开始的一次 job 生命周期计算。只有 backfill 明确重排 completed/failed 时 attempts 才归零。scheduled pass 不自动复活 failed，避免每天把 terminal failure 重新变成无限重试。

### 6.2 原子 ensure

Repository 新增 backfill 专用 ensure 操作，按唯一键锁定/更新 job，并返回 created、requeued 或 unchanged：

- 新行：创建 pending/root。
- completed：仅当本次查询已确认仍有空 artwork checkpoint 时重置为 pending/root。
- failed：只有 manual pass 允许重置。
- pending/running/retry：不修改 attempts、stage、next_attempt_at 或 started_at。

重排时清空 `completed_at`、`last_error`、`next_attempt_at`、`started_at`，attempts 归零；`metadata_id` 可保留用于观察，但 worker 仍以 provider identity 重新解析 canonical metadata。

## 7. Catalog worker 调整

### 7.1 Root 快速跳过保持不变

Movie/Series root 的 metadata 和 artwork checkpoints 都非空时，root details 仍可跳过。Series job 随后必须进入 seasons stage，以便检查后代，而不能因为 root 完整就直接完成整个 job。

### 7.2 Series 后代重新访问

现有 `FindIncompleteCatalogChild` 只检查 `catalog_hydrated_at IS NULL`，需按层级扩展：

- 选择 Season：Season 自身 `catalog_hydrated_at` 或 `catalog_artwork_hydrated_at` 为空，或者存在图片 checkpoint 为空的 Episode 后代。
- 选择 Episode：Episode 自身 `catalog_hydrated_at` 或 `catalog_artwork_hydrated_at` 为空。

当 Season 自身完整但 Episode 缺图时，`hydrateCatalogSeason` 不重复写 Season metadata/artwork，只进入 Episode 循环。完成全部后代后重新写 `catalog_hydrated_at`，保持原有 checkpoint contract。

### 7.3 不覆盖已有图片

当前 catalog 导入最终调用会 upsert selection，可能覆盖本地或手工图片。改为 catalog 专用的 insert-if-absent：

1. 下载前查询有效 selection（selection 能 JOIN 到 asset）；已存在则跳过网络请求。
2. 若不存在，正常下载、验证并按 SHA-256 保存/复用 asset。
3. 写 selection 使用数据库条件 upsert：冲突行仍指向有效 asset 时不更新；冲突行指向不存在的 asset 时才替换为新 asset。
4. 若步骤 1 后并发产生有效手工 selection，步骤 3 不覆盖它；刚下载的共享 asset 允许暂时成为孤儿，留待未来 GC。

手工导入和显式替换继续使用现有 overwrite/upsert 路径；只有 catalog 自动补图使用 insert-if-absent。

### 7.4 结果分类

在现有 worker 内累计轻量结果，不新增数据库表：

- `artwork_saved`：新 selection 写入成功。
- `artwork_existing`：已有 selection 或并发冲突保留。
- `artwork_source_missing`：TMDB 对适用类型没有路径。
- `retry`：本次失败并进入 retry。
- `failed`：达到上限进入 terminal failed。

这些计数写入现有 TaskExecution metrics 和 detail。job 状态、attempts、next_attempt_at、last_error 继续提供持久化事实。

## 7.5 豆瓣电影第二数据源

豆瓣第二数据源只处理 `kind='movie'` 且恰好存在一个
`provider='douban' AND entity_kind='movie'` identifier 的 canonical metadata。
Series、Season、Episode 以及同一 Movie 存在多个豆瓣 identifier 的情况均视为
存疑并跳过；不得从标题搜索或猜测新关联。豆瓣详情若明确返回与 canonical 不同的
TMDB ID，同样判为冲突并且不写快照、字段或图片。

搜索结果和发现页结果仍只是候选，不写数据库。可信 Movie 的详情响应完整保存到
`metadata_provider_snapshots(provider='douban')`，payload 原样写入 JSONB；canonical
只做固定的主从投影：空字符串、零值字段才由豆瓣填补，唯一覆盖例外是当前标题不含
中文且豆瓣标题含中文。`source`、已有非空字段和 TMDB identifier 保持不变。

在线刮削得到可信 Movie 豆瓣 identifier 后调用同一个 enrichment 函数；失败只记录
日志，不回滚已经成功的 TMDB 主数据。历史数据由独立 scheduler 调用该函数，避免
形成两套融合规则。

## 7.6 豆瓣候选海报

新增 `metadata_artwork_candidates`，只承担“本地资产属于哪个 metadata/provider，
但当前未选择”的关系：

```text
metadata_id + artwork_type + source_provider  唯一
asset_id                                    -> artwork_assets
source_url                                  保留原始来源
```

表使用 `PermanentBase`，不含 `deleted_at`。第一版只写
`artwork_type='poster' AND source_provider='douban'`，不扩展候选选择 UI。
ArtworkStore 继续复用 ImageProxy、图片验证、SHA-256 去重和 DataDir 存储；候选 upsert
只更新豆瓣候选关系，不修改 `metadata_artworks`。

若当前没有有效 poster selection，保存候选后使用 insert-if-absent 将候选原子提升到
`metadata_artworks`；若已有本地、手工、TMDB 或其他来源 selection，则保持原选择。
对外展示仍只读取 `metadata_artworks`，API 和 Emby/Jellyfin 不需要感知候选表。

本地资产完整性巡检扩展为同时扫描 selection 与 candidate 引用的去重 asset。文件缺失
时硬删相关失效关系：selection 继续按现有逻辑清 checkpoint；豆瓣 candidate 删除后由
下一次豆瓣补齐重新下载。提升候选前必须再次确认资产文件有效。

## 7.7 慢速历史补齐任务

注册独立 scheduler job `douban_movie_enrichment`：默认关闭，沿用任务中心的手动运行、
周期设置、历史与日志。任务不创建新队列表，按 metadata ID keyset 从 canonical 图有界
分页，每次只处理固定小批 Movie，并在 provider 请求之间使用固定间隔串行限速；cursor
写入内部 setting，跑到末尾后归零。

候选条件为：唯一豆瓣 Movie identifier，且豆瓣 snapshot、允许补齐的 canonical 字段或
豆瓣本地候选海报至少一项未完成。已有 snapshot 时先从原始 payload 复用详情，避免重复
网络请求；临时网络、限流或下载失败不写完成状态，cursor 周期回绕后可重试。多 identifier、
实体类型不支持或明确 identity 冲突计入 skipped，不形成热重试。

TaskExecution 至少记录：`scanned`、`enriched`、`snapshot_saved`、`fields_filled`、
`candidate_saved`、`candidate_promoted`、`ambiguous_skipped`、`failed`。

## 8. 硬删除设计

### 8.1 精确范围

| 表 | 删除语义 | 迁移处理 |
| :--- | :--- | :--- |
| metadata_items | 仅 merge 后硬删 source | 历史 tombstone 无外部引用才删除 |
| metadata_identifiers | 替换/失效即硬删 | 删除历史 tombstone |
| metadata_provider_snapshots | provider 快照替换/清除即硬删 | 删除历史 tombstone |
| metadata_artworks | selection 替换/清除即硬删 | 删除历史 tombstone |
| metadata_artwork_candidates | 候选替换/失效即硬删 | 新表，无历史 tombstone |
| artwork_assets | 仅未来零引用 GC 硬删 | 历史 tombstone 恢复为普通资产并保留文件 |
| catalog_hydration_jobs | 运行状态原地更新；未来清理硬删 | 删除历史 tombstone |

用户状态和人物实体表不在迁移范围内。metadata merge 迁移这些引用时仍可使用 `Unscoped` 访问保留软删除语义的外部表；`media` 与作品 credit 关系使用硬删除。

### 8.2 模型

新增不含 `gorm.DeletedAt` 的永久记录 base，供上述八个模型复用：

```text
PermanentBase = ID + CreatedAt + UpdatedAt + 与 Base 相同的 UUID BeforeCreate hook
```

不要修改全局 `Base`，避免意外改变用户、媒体库、收藏、历史等域模型。八个模型改嵌 `PermanentBase` 后，删除调用自然成为 GORM 硬删除。

### 8.3 Repository 语义

- 删除所有上述八表的 `deleted_at IS NULL` JOIN/WHERE。
- 删除 media、identifier、snapshot、selection、credit 的 `Unscoped` 恢复与 `deleted_at = NULL` 更新。
- credit snapshot 替换时保留来源原文未变的既有展示译文，只硬删除真正移除的关系。
- identifier 替换在事务中先硬删旧 provider/kind 行，再校验/创建新 identity。
- snapshot 和 selection 使用普通 upsert；不再恢复 tombstone。
- merge 仍在同一事务内先迁移所有引用，再硬删 source metadata；对范围内表不再需要 `Unscoped`。
- 外部软删除表的 merge 逻辑不改，避免扩大本任务。

### 8.4 PostgreSQL migration

在现有 `AutoMigrate` 流程增加一个幂等、事务化迁移，顺序位于 performance indexes 创建之前：

1. 若八表均无 `deleted_at`，直接返回。
2. 对历史软删除 metadata item 做引用预检：active child、media、favorite、playback history/event、playlist item、metadata credit 等任何未迁移引用存在时，返回包含表名和计数的错误。
3. 删除所有属于待删 metadata 的 owned 关系、其他历史软删除关系行；catalog job 对待删 metadata 的 `metadata_id` 先置空。
4. 对 selection 存在但 asset 行不存在的数据：清空对应 metadata 的 artwork checkpoint 并硬删无效 selection。
5. 按 Episode、Season、Movie/Series 顺序硬删通过预检的 metadata tombstone。
6. 将历史软删除 artwork asset 的 `deleted_at` 置空，保留数据库记录和磁盘文件。
7. 删除引用 metadata `deleted_at` 的旧部分索引/唯一索引。
8. 删除八表 `deleted_at` 列；media 与 credit 的历史 tombstone 物理删除，活动行保留。
9. 重建 Season/Episode、media 与 credit 业务索引，不再包含 deleted_at 谓词。
10. 事务提交；任一步失败全部回滚。

不能使用 `CASCADE` 绕过引用异常。数据库外的图片文件在该 migration 中完全不修改。

### 8.5 索引迁移

需要显式替换：

- `uidx_metadata_season`：`(parent_id, season_num) WHERE kind='season'`。
- `uidx_metadata_episode`：`(parent_id, episode_num) WHERE kind='episode'`。
- metadata kind/release、parent/season、parent/episode、title、original_name 的 `_active` 索引改为无 deleted predicate 的等价索引。

identifier、artwork selection、snapshot、catalog job 的业务唯一索引列不变。单列 `deleted_at` 索引随列删除。

## 9. 并发、幂等和失败边界

- 缺图查询只负责候选快照，最终 job ensure 和 selection insert 均由数据库唯一约束解决竞态。
- 多个 scheduler/manual 请求由现有 scheduler `beginRun` 防止同一 job 并发执行。
- 多进程/多 worker claim 继续依赖 `FOR UPDATE SKIP LOCKED`。
- running/retry 不被 backfill 重置，429/网络失败不会形成热循环。
- provider 空图片路径和下载失败分开处理；只有前者写 checkpoint。
- 图片文件先写、selection 后写的现有顺序保留。selection 竞态失败最多产生可回收孤儿 asset，不会覆盖用户图片或丢失有效 selection。
- schema migration 发现残留外部引用即阻止应用启动，不以数据丢失换取自动修复。

## 10. 可观察性

巡检 TaskExecution：

- `roots_scanned`
- `missing_roots`
- `jobs_created`
- `jobs_requeued`
- `jobs_unchanged`
- `assets_scanned`
- `assets_missing`
- `selections_invalidated`

catalog TaskExecution/job：

- `artwork_saved`
- `artwork_existing`
- `artwork_source_missing`
- `retry`
- `failed`
- job `status/attempts/next_attempt_at/last_error`

日志不得记录完整远程图片 URL；沿用现有 URL 脱敏。

## 11. 兼容性、上线与回滚

### 11.1 兼容性

- API 图片 URL 仍为 `/api/artwork/{assetID}`。
- 豆瓣候选图不会出现在 API、MediaView 或 Emby/Jellyfin 输出中，除非它已被提升为当前 selection。
- MediaView、搜索投影、Emby/Jellyfin 图片读取只移除 metadata/artwork 的 soft-delete predicate，返回结构不变。
- 手工图片导入仍可覆盖 selection；自动 catalog 只补空缺。
- 元数据编辑对话框只提交描述字段和 provider ID，不再展示或提交 poster/backdrop URL；后端元数据更新 DTO 同步移除图片字段与 selection 删除路径。
- 任务中心沿用通用 scheduler action，无前端行为变化。
- `media.deleted_at` 不再作为兼容列保留；升级前的 tombstone 先物理删除。

### 11.2 上线顺序

1. 备份 PostgreSQL。
2. 部署代码并让 startup migration 完成；迁移失败则保持服务未启动并处理残留引用。
3. 在生产规模只读副本或维护窗口执行两条候选 SQL 的 EXPLAIN。
4. 保持两个定时任务关闭，分别手动执行一个有界 pass。
5. 核对任务 metrics、catalog failed/retry、豆瓣 skipped/failed、当前图片与候选图片关系及 provider 请求量。
6. 确认稳定后由管理员按需启用各自周期。

### 11.3 回滚

- 功能回滚：先关闭 scheduler；已进入 catalog queue 的 job 可由现有 worker完成，或在维护窗口停止服务后处理。
- migration 事务内失败：PostgreSQL 自动回滚，不会留下半删列状态。
- migration 已成功但需回退旧二进制：先重新添加八表 nullable `deleted_at timestamptz` 及旧版需要的索引，再部署旧版本。
- 历史 tombstone 和新版本执行的硬删除不恢复；这是已确认的业务语义。活动 metadata、selection 和 asset 通过备份及引用预检保护。

## 12. 已知限制与后续触发条件

- TMDB 后来新增图片：只有产品明确要求周期复查负结果时，才增加逐类型 last-checked 状态；当前不直接按 TTL 清空实体 checkpoint。
- 孤儿 asset/file：只有磁盘占用或孤儿数量可观测地增长时，设计零引用、事务外文件删除和失败恢复的独立 GC。
- 图片内容损坏：只有存在实际损坏案例时，再把文件存在/可读检查升级为有界解码或哈希校验；当前不为推测性损坏增加全量 CPU/I/O。
- 豆瓣只处理已有唯一 identifier 的 Movie；若未来需要电视剧或自动搜索关联，必须另行设计可靠的季节边界与 identity 置信规则。
- 新索引：只有真实 EXPLAIN 显示现有索引不足时添加。
