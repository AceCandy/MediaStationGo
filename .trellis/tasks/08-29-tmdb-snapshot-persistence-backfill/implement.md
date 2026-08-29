# TMDB 快照全链路写入与一次性历史回填实施计划

## 1. 成功标准

- 普通自动刮削、手动匹配和 catalog hydration 获取的 Movie/Series/Season/Episode TMDB 详情都幂等保存 RawJSON snapshot。
- 升级后只自动执行一轮历史回填；中断可续，完整结束后重启不再扫描。
- 任务中心存在独立“TMDB 快照回填”，显示已处理/总数、成功、失败、剩余并支持手动重试。
- 历史回填只写 snapshot，不覆盖 canonical metadata、图片或人工修改。
- 测试、前端 lint/build、格式和 diff 检查通过，现有详情页未提交改动不被覆盖。

## 2. 实施顺序

### Phase A. 固化行为测试

- [ ] 为普通 TMDB Movie/Series 持久化补充 snapshot 断言，覆盖合法 RawJSON、空/非法 JSON、重复 upsert。
- [ ] 覆盖搜索候选后详情路径，证明接受结果保留完整 RawJSON，且详情失败不破坏已接受匹配。
- [ ] 覆盖普通 Season/Episode 快照写入，不影响 catalog hydration 现有测试。
- [ ] 覆盖手工编辑 TMDB ID：成功时 ID/snapshot 同时更新，详情或 snapshot 失败时两者均保持原值。
- [ ] 为历史候选查询准备 Movie/Series/Season/Episode、已有 snapshot、无 TMDB ID、未关联 media 和层级异常夹具。
- [ ] 为一次性状态覆盖：首次自动启动、完整结束后不再启动、中断/致命错误后续跑、单项失败后只允许手动重试。

验证门：新增测试应先准确暴露当前缺口，失败原因与 PRD 一致。

### Phase B. 前向写入

- [ ] 在共享 provider metadata 持久化点为 TMDB match 写入现有 snapshot repository。
- [ ] 将搜索后扩展详情切到可返回完整 RawJSON 的现有 TMDB match 方法，删除因本次修改失去调用方的薄封装。
- [ ] 在普通 Season/Episode 详情成功持久化后写 snapshot；保持 catalog hydration 已有逻辑不退化。
- [ ] 将手工 TMDB ID 编辑接入完整详情获取，并以 repository 事务保证 identifier/snapshot 原子更新。
- [ ] 仅移除本次变化产生的未使用函数/import，不顺手重构 scraper。

验证门：四种 kind 的前向路径都有测试，snapshot 写入失败不回滚 canonical metadata。

### Phase C. 一次性回填服务

- [ ] 在 metadata repository 增加总数和 keyset 候选查询，使用 snapshot `NOT EXISTS` 作为单条成功检查点。
- [ ] 新增最小回填方法，串行获取完整详情、逐条 upsert、隔离失败并通过回调报告 metrics。
- [ ] 使用内部 setting 保存“一轮完整枚举结束”；取消或致命查询错误不保存，单项失败不阻止保存。
- [ ] 在 `Container.Boot()` 中仅对未完成迁移异步启动；不注册 scheduler 或周期配置。

验证门：重复执行不请求已有 snapshot；服务重启仅在未完成时续跑；完整结束后无自动 provider 流量。

### Phase D. 任务中心接入

- [ ] 增加独立 task kind、稳定 task definition 和 server-owned 手动 action。
- [ ] 自动/手动执行复用同一回填服务和互斥，先持久化 TaskExecution 再启动后台工作。
- [ ] 持续更新 `processed/total/succeeded/failed/remaining`，结束后保留最终 metrics 和脱敏日志。
- [ ] 在任务定义行通用展示活动或最近执行 metrics，复用现有 3 秒刷新和任务 API。

验证门：并发点击只产生一个运行任务；完成后可查看最终数字和历史；手动空跑也记录 0 结果。

### Phase E. 独立复核与回归

- [ ] 逐项核对 PRD AC，检查 Movie/Series/Season/Episode 数据流和失败语义。
- [ ] 复核一次性标记只在完整枚举结束时写入，任务历史/日志不参与候选恢复。
- [ ] 复核媒体、媒体库和库根删除路径未新增 metadata/identifier/snapshot 清理，metadata merge 现有迁移/去重语义不退化。
- [ ] 复核日志和测试夹具不包含 API key、完整远程 URL、DSN 或隐私路径。
- [ ] 检查工作树，仅保留本任务改动和用户已有详情页改动；删除本次产生的临时产物。

## 3. 验证命令

仅运行与改动相关的 Go 测试；若测试需要 PostgreSQL 但环境未配置，记录未验证项，不打印 DSN。

```bash
gofmt -w <本任务修改的 Go 文件>
go test ./internal/repository -run 'ProviderSnapshot|TMDbSnapshotBackfill'
go test ./internal/service -run 'TMDb.*Snapshot|SnapshotBackfill|TaskDefinition'
go test ./internal/handler -run 'Task.*Snapshot'
cd web && npm run lint && npm run build
git diff --check
```

## 4. 风险与回滚点

- provider 请求量：保持串行、小批和现有 client 限流；不引入新并发池。
- 一次性标记：只有完整枚举结束才写；实现前后分别测试中断和单项失败。
- Season/Episode identity：必须复用现有 Series ID + 季/集坐标请求，不把 Season/Episode 自身 TMDB ID误当 Series ID。
- 前端任务页：只增加通用 metrics 展示，不改任务 API 或页面结构。
- 快照写入可幂等回滚；已写快照保留不会影响旧代码。
