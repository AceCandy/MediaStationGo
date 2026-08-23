# 媒体入库刮削策略与失败处置

## Goal

让已入库媒体的元数据处理默认自动、可观测、可手动补救，并按媒体库类型明确选择网络刮削或本地 NFO，避免非常规内容被网络源误匹配。

## Confirmed Facts

- 当前“媒体入库刮削”任务定义是事件触发，任务中心没有手动执行入口。
- 当前 worker 会处理 `pending`/空状态，失败和未匹配分别持久化为 `error` 和 `no_match`，二者都不会被 worker 自动重试。
- 当前任务日志会记录失败或未匹配，但 Web 没有结构化的失败列表、状态筛选或从失败记录直接处理的入口。
- 当前扫描后是否唤醒自动刮削受 `scrape.auto_on_scan` 开关控制，Web 默认值为关闭。
- 当前扫描/刮削已有本地 NFO 读取与本地 metadata 持久化链路，且不得修改、移动或删除 NFO 与可播放文件。
- 当前电影 NFO 支持同名 `.nfo`、`movie.nfo`、目录同名 `.nfo` 等既有规则；剧集支持 show 层 NFO 与单集同名 NFO 合并，不需要新增 NFO 解析器。
- 当前缺失 NFO 会退化为本地图片/空 metadata，损坏 NFO 会返回解析错误；非常规类型将在这两个边界上分别映射为 `no_match` 和 `error`。
- 精确 provider ID 命中已有 canonical metadata 时可跳过网络请求；扫描直接绑定和 scraper 快速命中目前都没有明确的“命中已有元数据”业务日志。
- 已有整库/单媒体重新入队、统一 worker、手动搜索与应用匹配接口以及 `ManualScrapeDialog`，本任务应复用这些能力。
- 当前系列库判断散落于 scanner、scraper、organizer、Emby 输出和 Web；“非常规剧集”必须在这些边界保持一致，否则会读错 NFO 层级或生成错误 canonical 类型。

## Requirements

### R1. Manual pending scrape

- 任务中心的“媒体入库刮削”支持手动执行。
- 操作者可选择单个媒体库或“全部媒体库”；全部模式仍逐库复用现有重新入队逻辑，再由统一 worker 消费，不新增全局队列或执行器。
- 手动执行只处理已经扫描入库、但 metadata 尚未完成的媒体；不扫描尚未入库的文件，也不清空或重刮已匹配 metadata。
- `pending`/空状态只需唤醒 worker；`error`/`no_match` 清理旧错误后重新设为 `pending`。这里的状态变更不影响媒体入库记录、文件或已有匹配。
- 执行使用现有持久状态和统一 worker，不新建第二套刮削队列。

### R2. Metadata-match observability

- 入库过程命中已有 canonical metadata 时必须有操作者可见日志。
- 日志必须明确区分“命中已有元数据”、“网络刮削匹配”和“本地 NFO 入库”。
- 三种来源统一归入“媒体入库刮削”执行历史和日志，不因实际命中发生在扫描或 worker 阶段而分开展示。
- 每条成功日志直接带来源标签。
- 日志不得暴露请求 URL、查询参数、密钥或未经处理的敏感错误。

### R3. Automatic policy by library type

- 取消 Web 上的“扫描后自动刮削”设置；扫描产生待处理媒体后默认自动进入 metadata 处理。
- 普通电影/剧集类媒体库默认使用现有网络 provider 链路。
- 新增用户可见的媒体库类型“非常规电影”和“非常规剧集”，用于个人录制、短片和 B 站收藏等内容。
- 非常规类型使用本地 NFO 作为 metadata 来源，不将目录/文件名自动送往常规网络 provider。
- 非常规类型严格使用 NFO-only：缺失 NFO 标记为 `no_match`，NFO 解析失败标记为 `error`，均禁止网络回退并进入 Web 待处理列表。
- 非常规类型不开放基于网络 provider 的手动匹配；用户补充或修复 NFO 后可重新处理。
- “非常规电影”沿用 movie canonical 层级；“非常规剧集”沿用 series/season/episode canonical 层级，不新增 metadata kind。

### R4. Web remediation

- Web 必须提供结构化的刮削待处理列表，至少覆盖 `error` 和 `no_match`。
- 每条显示状态、媒体、所属媒体库和可安全展示的失败原因。
- `error` 支持重新进入自动处理；普通媒体库的 `no_match` 支持打开现有手动匹配流程；非常规媒体提示补充或修复 NFO 后重试。
- 支持按媒体库筛选，并在待处理列表内完成重试或手动匹配。

### R5. Failure-state ownership

- 重试与恢复仍以 `media.scrape_status`/业务状态为准，不从任务日志反解析待处理媒体。
- 真实失败与确定未匹配保持不同状态和 Web 处置动作。
- 重试操作必须清理旧错误、设为 `pending` 并唤醒现有 worker。

## Acceptance Criteria

- [ ] 管理员可从任务中心选择单个媒体库或全部媒体库，手动处理已扫描入库但 metadata 未完成的媒体；已匹配记录保持不变，尚未扫描入库的文件不在此操作范围内。
- [ ] 普通媒体库扫描变化会自动唤醒网络刮削，Web 不再展示自动刮削开关。
- [ ] 非常规电影/剧集只按本地 NFO 生成或复用 canonical metadata，不请求常规网络 provider。
- [ ] 非常规媒体缺失 NFO 时进入 `no_match`，NFO 损坏时进入 `error`，补好 NFO 后可从 Web 重新处理。
- [ ] 已有 metadata 快速命中、网络匹配和 NFO 入库统一出现在“媒体入库刮削”历史和日志中，并显示对应来源标签。
- [ ] Web 可列出且筛选 `error`/`no_match` 媒体，显示可用原因并提供状态对应的处理动作。
- [ ] 重试后的媒体进入现有三 worker 队列，不创建旁路队列或从日志恢复状态。
- [ ] 扫描、刮削和失败处置不修改媒体文件、媒体库归属或 NFO/图片边车文件。

## Out of Scope

- 不创建第二套刮削 worker、队列或失败记录表。
- 不新增任务来源数据库字段、来源筛选或未匹配媒体详情页。
- 不为非常规内容自动生成 NFO。
- 不在本任务中修改媒体文件布局、整理命名或播放逻辑。
- 不改变现有三媒体 worker 和串行 catalog worker 的并发上限。
