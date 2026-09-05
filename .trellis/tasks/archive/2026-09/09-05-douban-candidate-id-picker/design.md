# 豆瓣候选搜索并回填 ID：技术设计

## 边界

本次只扩展现有手动刮削搜索链路，不新增路由、不改变保存接口，也不修改自动刮削的豆瓣单结果语义。

## 数据流

```text
元数据编辑弹窗中的当前标题
  → GET /media/:id/scrape/search?provider=douban&query=...
  → ScraperService.ManualSearch
  → DoubanProvider.SearchCandidates
  → { items: ManualScrapeCandidate[] }
  → 候选弹窗
  → 点击候选，仅 set('douban_id', candidate.douban_id)
  → 用户点击原“保存”按钮后 PATCH /media/:id/metadata
```

## 后端设计

- 在 `DoubanProvider` 增加返回候选数组的方法，复用现有 `subject_suggest` 请求和解析逻辑。
- 保留 `Search` / `SearchMatch` 的单结果契约，使自动刮削与外部搜索服务行为不变。
- 手动豆瓣搜索分支改为追加全部豆瓣候选，继续由现有 `ManualSearch` 的去重逻辑处理重复项。
- 现有 Handler 和响应结构不变。

## 前端设计

- 豆瓣搜索按钮不再外跳，改为调用现有 `mediaAPI.manualScrapeSearch`。
- 复用 `ManualScrapeCandidateList` 展示候选，不复用其完整刮削 `apply` 动作。
- 使用现有 `ModalShell` 承载加载、空结果、错误与候选状态。
- 候选点击只回填当前表单的 `douban_id` 并关闭候选层；编辑表单其他字段和保存流程保持不变。
- TMDb、Bangumi、TheTVDB 的外部搜索链接不变。

## 兼容性与回滚

- API 路由和 JSON 外层结构保持兼容；仅豆瓣手动搜索从最多一项扩展为多项。
- 不涉及数据库迁移。
- 如需回滚，可独立撤销 Provider 多候选方法、手动搜索分支和前端候选层，不影响元数据保存链路。

## 风险控制

- 豆瓣候选解析需覆盖数组、空数组和异常响应，避免索引越界。
- 候选缺少 `douban_id` 时不可执行回填。
- 弹窗叠层需使用高于编辑弹窗的 `zIndex`，并保持 Escape/关闭按钮可用。
