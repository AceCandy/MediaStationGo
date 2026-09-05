# 实施清单

1. 扩展豆瓣 Provider 的多候选搜索
   - 复用现有请求与响应模型，新增候选数组返回能力。
   - 保留 `Search` / `SearchMatch` 的单结果行为。
   - 增加最小回归测试，覆盖两个候选和空结果。

2. 接入手动搜索链路
   - 让 `provider=douban` 分支追加全部候选。
   - 保持现有路由、参数和响应结构不变。

3. 在元数据编辑弹窗增加豆瓣候选选择层
   - 豆瓣搜索按钮使用当前表单标题调用现有 API。
   - 复用现有候选列表与 `ModalShell`。
   - 点击有效候选只回填 `douban_id`，不调用保存或完整刮削接口。
   - 保持其他来源按钮和详情页已关联豆瓣外链不变。

4. 验证与独立复核
   - 运行针对豆瓣候选及手动搜索的 Go 测试。
   - 运行 `cd web && npm run lint`。
   - 运行 `cd web && npm run build`。
   - 运行 `git diff --check`。
   - 独立检查数据流、权限、空结果/错误状态、候选缺少 ID、叠层关闭行为和无关功能兼容性。

## 风险文件与回滚点

- `internal/service/douban.go`：不得改变自动刮削依赖的单结果方法契约。
- `internal/service/manual_scrape_providers.go`、`internal/service/manual_scrape_search.go`：仅改变豆瓣手动搜索的候选数量。
- `web/src/components/MetadataEditDialog.tsx`：候选选择不得触发 `updateMetadata` 或 `/scrape/apply`。
- 如验证失败，按后端候选扩展与前端选择层两个独立边界回滚。
