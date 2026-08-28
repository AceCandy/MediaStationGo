# 实施计划

1. 切换豆瓣主详情接口
   - 将移动端 rexxar 请求作为 `GetMatchByID` 主路径，保留 abstract 降级。
   - 增加 parser/provider 测试，覆盖简介、嵌套评分、嵌套海报、完整 RawJSON 和双接口失败。
   - 将 `GetEpisodeCountByID` 切到移动端详情的 `episodes_count`，并覆盖 TV 集数解析。
   - 核对正常刮削、手动按 ID 刮削、持久化补抓和历史补齐均通过共享详情入口；保持 `subject_suggest` 与 `search_subjects` 不变。

2. 刷新旧豆瓣快照
   - 候选 SQL 增加过期 legacy wrapper 快照条件。
   - 测试旧快照、冷却内旧快照、移动端完整/缺字段快照、豆瓣本地图片及 canonical 缺失条件。
   - 保持快照最后写入、失败不推进冷却及图片不覆盖选择。

3. 投影 provider 缓存三态
   - 增加 provider artwork existence repository 查询。
   - 单媒体详情计算 missing/partial/complete，并保留旧 snapshot 布尔字段。
   - 测试快照格式、selection/candidate 和无快照组合；列表保持不变。

4. 更新详情 UI
   - provider 外链改为无明文 ID 的紧凑图标按钮与状态图标。
   - 评分始终显示，缺失显示 `-`。
   - 更新 TypeScript 类型；使用主题变量、title/aria-label、安全外链属性。

5. 验证与复核
   - 运行相关 Go repository/service/handler 测试。
   - 运行 `npm run lint`、`npm run build`、`git diff --check`。
   - 使用 `trellis-check` 独立核对 endpoint 降级、旧快照冷却、图片选择安全和跨层状态语义。

## 回滚点

- endpoint/候选刷新、详情状态和 Web 展示可分别回滚；均不涉及 schema 或不可逆数据迁移。
