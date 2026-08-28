# 豆瓣移动端详情快照与来源状态

## Goal

用豆瓣移动端详情 JSON 替代信息明显不足的 `subject_abstract` 作为主快照来源，使历史豆瓣作品能保存简介、海报及其他完整详情；同时让详情页以简洁的 provider 图标和三态标识呈现本地数据状态。

## Background

- 当前主详情请求为 `https://movie.douban.com/j/subject_abstract?subject_id={id}`。
- 运行库有 864 个豆瓣 Movie ID 和 864 个豆瓣快照，但快照中可识别的海报 URL 为 0，豆瓣海报候选为 0；当前示例作品没有任何其他来源海报。
- `https://m.douban.com/rexxar/api/v2/movie/{id}` 对同一豆瓣 ID 返回 `intro`、`cover_url`、`pic`、`original_title`、语言、国家、类型、上映日期和嵌套评分等更完整字段。
- 现有详情页显示 provider 名称、明文 ID 和 `✅`；`✅` 只能表示快照存在，无法区分未获取、不完整和完整。

## Requirements

- R1：`DoubanProvider.GetMatchByID` 以移动端 `rexxar` Movie JSON 为主详情来源，并将完整原始响应保存为豆瓣快照；正常刮削、手动刮削和历史补齐共用该详情入口。
- R2：解析移动端响应中的标题、原名、简介、海报、年份、上映日期、评分、语言、国家和类型；支持 `cover_url` / `pic.large` 与 `rating.value` 等嵌套结构。
- R3：`subject_abstract` 仅作为主接口不可用时的降级来源；降级结果不得伪装成完整移动端快照。
- R4：现有旧格式豆瓣快照逐批重新获取移动端详情；保持唯一豆瓣 Movie ID、metadata-ID 游标、批次、串行节流、单项失败隔离及成功后 24 小时冷却。
- R5：豆瓣图片保存为本地 provider 候选；只有当前没有有效海报选择时才提升，不覆盖 TMDb、本地或人工选择。
- R6：详情响应为每个存在 canonical ID 的 provider 返回缓存三态：`missing`（无快照）、`partial`（有快照但缺本地 provider 图片，或豆瓣仍是旧/降级快照）、`complete`（当前格式快照和本地 provider 图片均存在）。评分或个别文本字段缺失不阻止完整状态。
- R7：详情页用紧凑的豆瓣/TMDb 图标按钮跳转对应网站，不显示明文 ID；三态使用灰色空心、琥珀色警示、绿色勾选图标并提供中文 tooltip/无障碍标签。
- R8：评分徽章始终显示；评分大于 0 时显示一位小数，否则显示 `-`。
- R9：不新增依赖，不修改数据库 schema，不按标题猜测或绑定 provider ID。
- R10：`GetEpisodeCountByID` 同样使用移动端详情中的 `episodes_count`；关键词搜索和分页发现继续使用各自现有接口。

## Acceptance Criteria

- [ ] AC1：示例豆瓣 ID `1432701` 能从移动端详情解析简介、评分和海报，并保存完整原始 JSON 快照。
- [ ] AC2：主接口失败时降级行为可识别；失败请求不推进成功冷却。
- [ ] AC3：旧 `subject_abstract` 快照会进入逐批刷新，24 小时内成功检查过的移动端快照不重复请求。
- [ ] AC4：豆瓣海报成为本地候选；无选择时提升，有现有选择时不覆盖。
- [ ] AC5：详情页 provider 按钮不显示 ID，但点击使用 canonical ID 生成正确豆瓣/TMDb URL。
- [ ] AC6：provider 状态能区分“未缓存 / 仅详情或旧快照 / 详情与图片均已缓存”，且颜色不是唯一提示方式。
- [ ] AC7：评分为 0 或缺失时显示 `-`，有评分时保持一位小数。
- [ ] AC8：相关 Go 测试、前端 lint/build 和 `git diff --check` 通过。
- [ ] AC9：正常刮削、手动按 ID 刮削、历史补齐和集数查询均不再以 `subject_abstract` 作为主请求；关键词搜索和分页发现行为不变。

## Out of Scope

- 按标题搜索豆瓣或自动修复缺失/歧义豆瓣 ID。
- 用豆瓣图片覆盖现有 TMDb、本地或人工海报选择。
- 引入新的图标库、数据库表或迁移。
