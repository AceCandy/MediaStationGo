# TMDb 集信息补全与复查

## Goal

新增独立、可手动和定时运行的“TMDb 集信息补全/复查”任务，只处理已有可播放 media 的 Episode，复用 TMDb 完整单集详情能力补齐或刷新集级元数据；同时把 Episode 从“TMDb 无图复查”移出，避免同一 TMDb Episode 接口由两个周期任务重复请求。

## Confirmed Facts

- `TMDbProvider.GetTVEpisodeDetails` 一次响应已经包含标题、简介、播出日期、评分、时长、外部 ID、演职员和 still 图片。
- `fetchAndSaveTMDbEpisodeDetails` 已能写入标题、简介、评分、年份、演职员和可选 still，但尚未写入 provider 已解析的播出日期，且当前没有独立任务稳定遍历历史 Episode。
- 任务中心的普通“媒体入库刮削”worker 会设置 `DeferEpisodeDetails=true`，不能视为历史集信息补全任务。
- Episode 仍需保留在“TMDb 图片本地化修复”中，以恢复已有链接对应的丢失本地 still 文件。

## Requirements

- 新任务只扫描具有 media、有效 Season/Episode 层级且可解析 Series TMDb identity 的 Episode；无 media 的 Episode 不进入任务，Episode 自身没有 TMDb identifier 不阻止按 Series/季/集号查询。
- 标题为空或属于“第 1 集”“Episode 1”等项目已有规则可识别的生成标题时，均视为标题缺失。
- 复用现有 TMDb provider、Episode 详情解析、metadata/credits/artwork 保存、Scheduler 和 TaskTracker；不新增队列或下载器。
- 一次 Episode 只调用一次 TMDb 完整详情，并由该响应处理文字信息、演职员和 still，避免再由“TMDb 无图复查”请求 Episode。
- TMDb 返回的非空标题、简介、播出日期、评分或年份与当前值不同时允许覆盖；该任务是明确的 TMDb 同步入口，可能覆盖手工编辑的对应字段。
- 候选仅限标题、简介、播出日期或 still 图片至少一项缺失的 Episode；四个字段均完整的 Episode 不进入任务。
- 不保存跨任务游标或遗留剩余量；一次执行通过内存内 keyset 分页处理完当时的全部候选，避免一次加载全表。
- 新增 Episode 详情检查时间。TMDb 成功响应后记录该时间，即使缺失字段仍为空也要冷却 3 天；网络、限流、服务端或解析失败不记录检查时间，可在下次任务重试。
- 任务默认关闭，默认周期 24 小时，支持手动运行。
- 详情日志只记录实际更新、仍缺失、跳过或失败，不输出完整 URL、凭证或正常无变化项。
- “TMDb 无图复查”只处理 Movie、Series、Season；“TMDb 图片本地化修复”继续处理 Episode。
- 不修改 Movie、Series、Season 的文字元数据，不重新匹配作品，不复用发现目录刮削任务。

## Acceptance Criteria

- [x] 任务中心出现独立的“TMDb 集信息补全/复查”，默认关闭、24 小时周期并可手动执行。
- [x] 候选只包含有 media 且标题、简介、播出日期或 still 缺失的 Episode；四项完整的 Episode 不请求 TMDb。
- [x] 空标题和生成标题均属于缺失；正常真实标题不单独触发任务。
- [x] 一次执行分批处理完启动时的全部候选，不保存跨任务游标；成功请求后的 Episode 在 3 天内不再命中。
- [x] 每个候选最多调用一次 Episode 详情接口，并可写入非空标题、简介、播出日期、评分、年份、演职员和 still。
- [x] 无变化不产生逐条详情；更新、无数据、并发跳过和失败可定位到 Series/Season/Episode 与 TMDb ID。
- [x] Episode 不再进入“TMDb 无图复查”，但本地 still 丢失仍可由“TMDb 图片本地化修复”恢复。
- [x] 现有首次刮削、手工重刮和 Movie/Series/Season 图片任务行为保持不变。

## Out of Scope

- 不扫描没有 media 的 Episode。
- 不周期刷新标题、简介、播出日期和 still 均完整的 Episode。
- 不重新匹配 Series、Season 或 Episode identity。
- 不修改 Movie、Series、Season 的文字信息。
- 不新增 Episode 时长或外部 ID 的周期补全条件；现有首次完整刮削行为保持不变。
- 不新增跨任务游标、通用队列、常驻 worker 或前端专用页面。
