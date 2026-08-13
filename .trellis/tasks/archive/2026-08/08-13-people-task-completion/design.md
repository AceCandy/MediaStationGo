# Design

## Boundaries

- `ScraperService` 继续拥有人物翻译的筛选、缓存和写回。
- `TaskTrackerService` 继续作为唯一执行摘要与任务日志入口。
- 任务中心复用现有认证 API，增加一个全局人物补齐触发端点；不在前端选择媒体库。

## Translation Flow

1. Worker 完成现有启用条件检查并查询待处理分组。
2. 分组为空则直接返回，不创建任务。
3. 分组非空时创建 `TaskKindPeople`、事件触发的“人物翻译”任务。
4. 处理窗口时更新阶段、指标和必要的日志详情。
5. pass 返回时完成任务；错误沿现有 retry 机制再次唤醒，后续重试形成新的执行记录。

任务创建失败时不执行该 pass，避免实际后台操作没有可追踪记录；worker 按现有失败退避重试。

## Task Center Flow

任务中心工具栏增加“补齐人物”操作。调用全局端点后由后台 worker 按缺失状态持续处理；按钮提交期间禁用并提示已加入后台队列。媒体库详情页旧入口保留并转向同一全局语义。

## Single Object Refresh

单个 TMDB 对象的手动 metadata 重刮削沿现有同步服务调用，完成 metadata 主体写入后立即拉取并持久化 credits/person；随后再唤醒全局翻译 worker。该路径不创建额外的全局补齐范围任务。

## Compatibility And Rollback

- 不迁移或重写既有任务记录。
- 不改变人物翻译 pending 判定和翻译缓存合同。
- 回滚仅需移除 worker 的任务跟踪注入及任务中心入口；原业务入口仍存在。
