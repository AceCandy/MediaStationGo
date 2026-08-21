# 实施计划

1. 统一 scheduler 手动入口
   - 更新定时任务定义的 action 与触发文案。
   - 让人物补齐、人物翻译、账号清理根据 scheduler manual context 记录触发来源。
   - 删除任务中心人物补齐专用分支并保留白名单映射。
   - 验证：针对任务定义和 scheduler job 的 Go 单元测试。

2. 移除人物补齐事件 worker
   - 删除启动唤醒、worker 生命周期及无用状态。
   - 将人物补齐兼容 HTTP 接口接入 scheduler 手动执行。
   - 验证：启动不产生 event 记录，兼容入口仍返回异步接受且生成 manual 记录。

3. 增加账号清理确认保护
   - 在任务中心复用 `confirmAction`，仅保护 `account_cleanup`。
   - 验证：取消不请求、确认只请求一次；其他任务不弹确认。

4. 独立复核与质量检查
   - 检查所有定时定义均支持手动执行，所有实际任务记录来源正确。
   - 运行最小相关 Go 测试、前端相关测试/静态检查及 `git diff --check`。
   - 确认未覆盖 `web/src/index.css` 和 `web/src/pages/TasksPage.tsx` 的既有用户改动。

## Rollback Points

- 后端可整体回退任务定义、scheduler job 与人物补齐生命周期改动，无数据库回滚。
- 前端确认是单文件局部调用，可独立回退。

## Planned Validation

```bash
go test ./internal/service ./internal/handler -count=1
npm --prefix web run lint
npm --prefix web run build
git diff --check
```
