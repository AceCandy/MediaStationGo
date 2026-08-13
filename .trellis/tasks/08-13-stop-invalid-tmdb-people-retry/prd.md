# 停止人物同步重试失效 TMDB 标识

## Goal

人物同步只处理仍然有效的 TMDB 作品标识。确认作品级 TMDB 标识失效后，停止人物补齐重复请求，并把关联媒体交回完整刮削链路，以便本地 NFO 或其他刮削器重新匹配。

## Background

- 人物同步目前只支持 TMDB，并直接请求作品 credits。
- 当前候选只检查 TMDB identifier，不检查 metadata 当前权威来源；因此已经降级为 `source=douban` 的对象仍会使用残留 TMDB 标识请求人物。
- credits 404 当前只计入失败，不更新人物完成状态，也不移除失效标识，因此每十分钟重复失败。
- 完整媒体刮削已有本地 NFO、TMDB 标题搜索、豆瓣、Bangumi 和 TheTVDB 的处理链路；人物同步不应重复实现这些匹配策略。
- 单条候选失败只增加 `failed` 指标，整轮任务仍被标记为普通完成，无法准确表达部分失败。

## Requirements

1. 人物同步只选择 `metadata.source = 'tmdb'` 且 TMDB identifier 的 `entity_kind` 与 metadata kind 一致的对象。
2. `source=douban/local/其他` 的 metadata 即使保留 TMDB 交叉标识也不得进入人物同步；不删除其跨源标识，不重置已经完成的媒体刮削。
3. 对仍由 TMDB 负责的 metadata，credits 明确返回 HTTP 404 时，直接确认当前作品类型与 TMDB ID 的组合失效；超时、限流、鉴权错误和 5xx 不删除标识，保留后续重试。
4. 确认失效后，软删除该 metadata 对应且类型一致的 TMDB identifier，并清除所有关联 media 上相同的 `lookup_tmdb_id`。
5. 存在关联 media 时，将同一电影或电视剧最小处理单元重置为 `pending`，标记事件触发并唤醒统一刮削 worker；由现有完整刮削链路处理 NFO 和其他 provider fallback。
6. 没有关联 media 的目录 metadata 只移除失效 TMDB identifier，不创建恢复任务，也不写 `people_hydrated_at`；人物补齐后续不再选中它。
7. 只要一轮存在对象失败或失效修复，任务摘要显示有异常并保留详情；不把部分失败伪装为全量成功。
8. 不新增失效状态表，不在人物 worker 内实现标题搜索或其他 provider 匹配。

## Acceptance Criteria

- [ ] `source=douban/local/其他` 的 metadata 不进入人物同步，已有跨源 TMDB identifier 保持不变。
- [ ] credits 404 后不再请求作品详情，失效 TMDB identifier 不再出现在人物补齐候选中。
- [ ] 关联 media 的同值 TMDB ID 被清除并重置为 `pending`，统一刮削 worker 被唤醒。
- [ ] 429、5xx、超时等临时错误不删除标识、不重置 media。
- [ ] metadata source 不是 TMDB，或 kind 与 identifier entity kind 不一致时，不发起人物请求。
- [ ] 一轮包含失败或失效修复时任务中心显示“有异常”，而不是普通“完成”。
- [ ] 聚焦服务测试与 `git diff --check` 通过。

## Out of Scope

- 为人物同步增加豆瓣、Bangumi、TheTVDB 人物来源。
- 新增独立的失效标识状态表或后台修复队列。
- 批量清理所有历史孤立 metadata。
- 修改完整媒体刮削的 provider 优先级。

## Technical Notes

- 复用现有 metadata identifier 软删除、media `pending` 状态和统一 scrape worker。
- 404 判定应保留可检查的 HTTP 状态类型，避免依赖错误字符串解析。
- 数据修复必须在事务中完成，避免标识已删除但 media 尚未重置。
- 正常 TMDB 刮削和发现入库已经读取作品详情；本地 NFO 也可能直接提供 TMDB ID，但 credits 404 同样足以判定当前类型与 ID 组合不可用于人物同步。
