# 技术设计

## 边界与假设

本任务只扩展现有豆瓣 provider、豆瓣补齐流程、provider 快照状态和媒体详情展示。电影映射 `/movie/{id}`，canonical series 映射 `/tv/{id}`；season/episode 不直接请求豆瓣。现有定时任务仍只扫描电影，不新增电视剧定时任务、数据库表、第三方依赖或抓取方式。

## Provider 请求与错误分类

完整详情请求显式接收实体类型并选择 movie/tv 路径，继续复用现有 HTTP client、Cookie 和请求头。请求结果分为三类：

1. HTTP 403 或有效 JSON `code=1000`：请求 `/rexxar/api/v2/subject/{id}`；subject 成功时返回解析后的 `Match`、原始 JSON 和降级标记。
2. HTTP 404 或明确不存在响应：返回现有 `ErrDoubanSubjectNotFound`，不降级。
3. HTTP 429、5xx、网络/读取错误、无效 JSON及其他错误响应：返回现有临时异常语义，不请求 subject。

subject 请求仍走同一错误分类，但不再递归降级；失败时不保存新快照。`GetMatchByID` 的搜索兼容行为不扩大，本任务只让需要持久化详情的 enrichment 路径使用上述严格规则。

## 快照状态

在现有 `MetadataProviderSnapshot` 增加非空、默认 `false` 的 `Degraded` 布尔字段。AutoMigrate 只做加列；历史快照保持 `false`，不根据 payload 猜测或回填。

现有完整快照写入口保持默认写入 `degraded=false`，另提供豆瓣降级快照的显式写入路径。冲突更新必须同时更新 payload、`fetched_at` 和降级标记，因此管理员手动获取完整详情成功后会原子清除旧降级状态。

详情投影把 `douban_status` 扩展为 `missing | partial | degraded | complete`：显式降级优先于现有 artwork/payload 完整性判断；非降级快照继续沿用当前 partial/complete 规则。`douban_snapshot` 保持兼容。

## 补齐与字段保护

现有补齐核心复用于 movie 和 series：按 metadata kind 校验唯一豆瓣标识、选择 movie/tv 完整路径、解析并保存海报候选及快照。定时电影任务和详情页手动重试都经过同一 provider 权限降级分支，避免两套错误规则。

降级 subject 只填充 canonical 当前为空的字段并保存可用海报候选，空值不参与更新；不覆盖已有非空字段。完整响应继续保持现有安全写入语义。快照作为字段和图片处理成功后的最后一步写入，避免部分失败却被标记为已降级或完整。

## 定时任务与手动恢复

`ListDoubanMovieEnrichmentAfter` 在现有候选 SQL 中排除显式 `degraded=true` 的豆瓣快照。首次权限降级并成功保存 subject 后按成功结果处理、推进游标并继续本批；429、5xx、网络错误仍暂停本批并留待下次重试。

现有 `POST /api/media/:id/douban-enrichment` 路由继续由 `AdminRequired` 保护，handler 放宽为具有唯一豆瓣标识的 movie/series。响应返回 `complete` 或 `degraded`，前端刷新详情并给出相符提示；完整重试成功清除降级标记，再次受限则替换/刷新降级快照并保持降级。

## 前端展示

前端类型增加 `degraded` 状态。现有豆瓣 provider 徽章复用 amber 告警图标，但将文案明确为“豆瓣接口受限，当前为降级数据”；管理员菜单继续复用“补齐豆瓣信息”操作，降级时显示为“重试完整豆瓣信息”。不新增页面、弹窗或权限逻辑。

## 兼容、回滚与风险

- 新数据库列和 API 枚举值均为增量变化；旧客户端仍可依赖 `douban_snapshot`，但可能不识别新枚举。
- 回滚代码时可保留新增列和已保存 subject payload，不删除数据；旧代码会把这些快照视作 partial，并可能重新进入定时候选。
- 历史 `subject_abstract` 快照不会自动标记降级，仍按现有刷新逻辑处理。
- `/subject` 字段远少于完整详情，降级状态只保证保留实际返回的信息，不承诺补齐简介、年份等缺失字段。
