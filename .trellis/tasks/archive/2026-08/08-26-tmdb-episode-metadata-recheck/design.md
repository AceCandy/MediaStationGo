# TMDb 集信息补全与复查技术设计

## 1. 边界

新增一个普通 Scheduler 任务，不新增队列或 worker：

```text
Scheduler / Task Center
        -> 分页查询有 media 且字段缺失的 Episode
        -> 每集调用一次 GetTVEpisodeDetails
        -> 更新 Episode metadata / credits / still
        -> 写入 3 天冷却 checkpoint
```

“TMDb 无图复查”删除 Episode 候选，只保留 Movie、Series、Season。“TMDb 图片本地化修复”继续扫描 Episode 的 TMDb still selection，以便本地文件丢失时按旧链接恢复。

## 2. 候选与状态

在 `MetadataItem` 增加 nullable `TMDbEpisodeCheckedAt`，数据库列为 `tmdb_episode_checked_at`。它只用于 Episode 完整详情成功同步后的 72 小时冷却，不代表三个目标字段已经齐全。

Repository 按 `metadata_items.id` 分页返回候选，必须同时满足：

1. `kind='episode'`。
2. 至少一个 `media.metadata_id` 指向该 Episode。
3. 可从 Episode -> Season -> Series 层级取得 Series TMDb identifier；Episode 自身 TMDb identifier 仅用于日志，不是调用详情接口的必要条件。
4. `tmdb_episode_checked_at IS NULL` 或早于本次任务开始时间 72 小时。
5. 下列任一缺失：
   - 标题为空，或 `tmdbEntityTitleIsGenerated(title, episode)` 为真；
   - 简介为空；
   - 播出日期为空；
   - 不存在 still 的有效 `metadata_artworks -> artwork_assets` 关系。

多个 media 版本只产生一个 Episode 候选。查询页固定为 200 行；任务内部以本地 `afterID` 继续直到查询为空，但不把 `afterID` 保存到 Setting。下一次执行始终从头重新判断候选。

## 3. 同步契约

复用 `GetTVEpisodeDetails(seriesTMDbID, seasonNum, episodeNum)`，一次响应消费：

- 非空标题、简介、播出日期、评分、年份覆盖 Episode 当前值；生成标题也可被真实标题替换。
- 已加载的 Episode credit 类型沿用现有替换语义。
- still 仅通过现有 ArtworkStore 保存；自动写入不得覆盖并发产生的有效选择。

将现有 Episode 详情保存逻辑整理为一个返回明确结果的内部同步函数，使任务能区分：

- provider/保存全流程成功：事务性或按现有安全顺序写入业务字段，最后更新 `tmdb_episode_checked_at=now`；即使 TMDb 仍缺标题、简介、播出日期或 still，也进入 72 小时冷却。
- provider、metadata、credits、图片下载或数据库保存失败：不更新 checkpoint，记录脱敏失败，下一次任务可重试。

首次入库和手工重刮继续复用同一同步函数，避免形成两套 Episode 字段融合规则。

## 4. 任务与日志

新增 definition/job：

| Key | 名称 | 默认开关 | 默认周期 |
| --- | --- | --- | --- |
| `tmdb_episode_metadata_recheck` | TMDb 集信息补全/复查 | 关闭 | 24 小时 |

任务支持 Scheduler 通用手动执行。一次执行处理全部当时符合条件的候选，不设持久化 cursor 或请求上限；请求保持串行并沿用现有 TMDb 重试、超时和任务并发保护。

标题、简介、播出日期和 still 已经完整的 Episode 不会成为候选。详情只输出实际更新、TMDb 仍缺字段、并发跳过或失败，包含 Series/Season/Episode 定位信息，不包含远程 URL、query、密钥或本地路径。

## 5. 兼容与风险

- 已有非空字段允许被 TMDb 覆盖，这是用户确认的同步语义。
- 一次执行时长随缺失 Episode 数线性增长；后台任务可取消，下一次会从头筛选，已成功项因 checkpoint 不会重复请求。
- checkpoint 合并时保留 source/target 中较新的时间，避免 metadata graph merge 后立刻重复请求。
- 不删除旧执行历史或图片状态；回滚时任务 definition 可移除，新增 nullable 列可保留而不影响旧代码。
