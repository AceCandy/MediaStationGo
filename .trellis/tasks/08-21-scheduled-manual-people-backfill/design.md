# 统一定时任务手动触发并简化人物补齐：设计

## Boundary

复用 `SchedulerService.RunNowAsync` 作为所有定时任务的唯一手动执行入口。任务中心只根据服务端 `TaskDefinition.Action` 展示执行按钮；前端仅为账号清理巡检增加确认保护。

## Data Flow

```text
任务中心执行按钮
  → 账号清理巡检先调用现有 confirmAction
  → POST /api/tasks/definitions/:key/run
  → TaskDefinitionSchedulerJob 解析白名单 job
  → SchedulerService.RunNowAsync 注入 manual context
  → 对应 scheduler job 复用原业务逻辑并记录 manual trigger
```

现有人物补齐兼容接口也改为进入同一 scheduler job，不再依赖独立常驻 worker。

## Backend Changes

- 将人物信息补齐、人物翻译和账号清理巡检定义为 `Action: "scheduler"`，触发文案统一为“定时 / 手动”。
- 删除任务中心处理器中人物补齐的专用分支，统一按定义映射调用 scheduler job。
- 人物补齐、人物翻译和账号清理 scheduler job 从现有 manual context 决定 `manual` 或 `scheduled`，业务主体保持一份。
- 删除人物补齐启动事件 worker、Boot/Close 生命周期调用及仅为该 worker 服务的状态；人物补齐定时执行继续由 scheduler 驱动。
- 现有人物补齐兼容接口调用相同的 scheduler 手动入口，保留原 HTTP 路径和异步语义。

## Frontend Changes

- 复用 `confirmAction`，仅当 `definition.key === "account_cleanup"` 时在 `tasksAPI.run` 前显示危险确认。
- 取消不发请求；确认后沿用现有运行中状态、提示和刷新逻辑。
- 不新增对话框组件、API 字段或可配置确认体系。

## Compatibility and Rollback

- 现有任务定义响应结构、任务执行 API 和 scheduler API 不变，只扩展已有定义的 `action` 与触发文案。
- 人物补齐旧接口继续保留；回滚时可恢复 worker 生命周期和专用处理分支，不涉及数据迁移。
- 工作区现有 `web/src/index.css` 与 `web/src/pages/PlaybackStatsPage.tsx` 改动不在本任务范围；实施时只对 `TasksPage.tsx` 做确认逻辑所需的局部补丁。

## Risks

- 账号清理是有副作用的操作；任务中心通过危险确认降低误触风险，直接 API 调用仍由现有鉴权与调用方负责。
- 手动触发必须绕过计划关闭状态，但仍受 scheduler 的同任务并发互斥保护。
