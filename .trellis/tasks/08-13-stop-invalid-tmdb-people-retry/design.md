# Design

## Error Contract

TMDB HTTP 调用返回带状态码的错误类型。人物补齐只识别 `404`，其他错误继续沿用现有失败与重试行为，不解析错误文本。

## Invalid Identifier Flow

1. 人物补齐仅对 `metadata.source=tmdb` 且 metadata kind 与 TMDB identifier entity kind 一致的候选调用 credits。其他来源保留 TMDB 交叉标识，但不用于人物同步。
2. credits 返回 404 时，在一个数据库事务内软删除该 identifier，清除关联 media 上相同的 `lookup_tmdb_id`，并把关联的电影或电视剧处理单元重置为 `pending/event`。
3. 事务提交后唤醒统一媒体刮削 worker；没有关联 media 时不唤醒。
4. 完整刮削链路继续负责读取 NFO 和搜索其他 provider。人物 worker 不做匹配。

## Task Result

永久失效修复计入失败/异常详情，但不终止同一轮其他候选。任务执行仍可完成，前端通过已有 `failed` 指标显示“有异常”。

## Rollback

代码回滚不会自动恢复已软删除的失效 identifier。关联 media 已转为 pending，可由现有刮削链路重新建立有效标识；这是有意的数据修复结果。
