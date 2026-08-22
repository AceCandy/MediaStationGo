# 优化自动入库刮削吞吐

## Goal

缩短自动入库后批量媒体完成刮削的总墙钟时间，同时保持同一媒体组只处理一次、共享元数据一致、手动刮削与发现目录任务行为稳定。

## Background

- 当前自动入库刮削只有一个 worker，且完整流程受全局 `scrapeRunMu` 串行保护。
- 首次无 provider ID 的媒体通常需要串行执行 TMDB 搜索和详情请求；这是冷刮削的必要网络成本。
- 媒体已有 TMDB ID、但数据库没有可复用 metadata 时，完整详情匹配已经取得 languages、countries、genres，匹配保存后仍会再次请求相同详情 endpoint 补同一批字段。
- 自动刮削完成日志覆盖匹配、图片、metadata 持久化与扩展详情等阶段，现有日志无法区分主要耗时来源。

## Requirements

### R1 固定三个自动媒体 worker

- 自动入库媒体使用固定 3 worker 并行处理不同媒体组。
- 同一 series、同一已知 metadata 或同一 media 不得被多个 worker 重复处理。
- 单个媒体组内部的 provider 查询、持久化和组内同步继续保持串行。
- 发现目录任务继续由单 worker 串行处理；只要仍有 `pending` 或 `running` 入库媒体，发现目录不得领取新任务。
- 手动单项刮削、整库刮削、发现目录刮削和人员补全继续与自动媒体刮削互斥。
- worker 数量本期不增加配置项。

### R2 消除已有 TMDB ID 路径的重复详情请求

- `GetMovieMatch` / `GetTVMatch` 返回的完整详情必须明确标记为已包含扩展字段。
- 完整详情已包含 languages、countries、genres 时，保存匹配后不得再次调用 `GetDetails`。
- 名称搜索等未取得完整扩展字段的路径仍须执行现有 `GetDetails` 补全，不能改变 metadata 完整性。
- 剧集 episode details 请求不属于本次去重范围。

### R3 输出自动刮削分段耗时

- 每个自动媒体组完成后输出一条无敏感信息的结构化耗时日志。
- 至少包含候选生成、provider lookup、metadata persist、artwork、TMDB extended details 和 total 六个阶段。
- 日志不得包含 API key、完整请求 URL 或媒体文件路径。

## Acceptance Criteria

- [ ] AC1：存在 3 个专用自动媒体 worker，不同媒体组能够实际重叠执行，最大并发不超过 3。
- [ ] AC2：并发 claim 不会把同一 series/metadata/media 分配给多个 worker，组内全部媒体仍同步为一致状态。
- [ ] AC3：发现目录任务保持单 worker；有 `pending`/`running` 入库媒体时不领取 catalog job；手动/整库/发现目录/人员补全仍通过独占锁避免共享 metadata 图交错修改。
- [ ] AC4：已有 TMDB ID 的电影和电视剧完整匹配只产生一次对应详情 HTTP 请求，并完整保存 languages、countries、genres。
- [ ] AC5：名称搜索匹配仍会执行一次扩展详情补全，不因请求去重丢失字段。
- [ ] AC6：每个自动媒体组产生一条包含六类耗时且不含敏感字段的结构化日志。
- [ ] AC7：新增或调整的针对性 Go 测试通过；并发相关测试通过 race detector（若测试环境支持）。

## Out of Scope

- TMDB 搜索/详情 TTL 缓存或请求合并。
- 图片异步下载、图片队列或现有 artwork 行为调整。
- 修改 TMDB 超时、429 退避和重试次数。
- worker 数量配置、动态伸缩或按 CPU 自动计算。
- 前端、API 和数据库 schema 变更。
