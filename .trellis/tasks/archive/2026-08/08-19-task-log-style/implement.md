# 统一任务中心日志样式：实施计划

## 1. 收口生命周期日志

- 修改 `TaskTrackerService` 的 start/update/finish/detail 日志写入：统一无 level，并补 `🔻/🔄/🔺/❌/ℹ️`。
- 保留 `taskLogStore`、任务摘要和 API 数据结构。
- 更新任务日志测试，覆盖无方括号级别、倒序所需的开始/结束语义、详情兜底和最终错误。

验证：

```bash
go test ./internal/service -run 'TestTask(Log|Tracker)' -count=1
```

## 2. 补齐七类任务详情语义

- 保留扫描和轨道回填已有标记。
- 在现有整理、扫描摘要、刮削、人物补齐、人物翻译详情格式化点补最小标记。
- 检索所有生产环境 `TaskUpdate.Details`，确认没有裸文本详情遗漏。
- 更新对应已有单元测试；不新增通用抽象或配置。

验证：

```bash
go test ./internal/service ./internal/handler -run 'Test(Task|Scan|Organize|Scrape|People|Probe)' -count=1
```

## 3. 统一任务日志前端渲染

- 扩展 `TasksPage` 日志行解析，兼容历史 `[INFO]/[DETAIL]/[ERROR]`。
- 为全部状态标记绘制固定颜色、可访问的 CSS 徽标。
- 保持倒序、换行、日期选择和刷新请求序号机制。
- 不新增前端依赖或测试框架。

验证：

```bash
cd web
npm run lint
npm run build
```

## 4. 规范与最终复核

- 更新后台任务执行规范中的无 level、生命周期箭头和详情标记契约。
- 独立复核七类任务、历史日志兼容、脱敏和工作树范围。
- 运行最终差异检查。

验证：

```bash
git diff --check
git status --short
```

## 风险与回滚点

- 风险：遗漏某个裸文本详情生产者。控制：实施后再次检索全部 `TaskUpdate.Details` 与详情格式化函数。
- 风险：历史日志标签解析误伤正文。控制：只解析时间戳后的标准 `[INFO]/[DETAIL]/[ERROR]` 位置，不做全局替换。
- 风险：数据库相关 Go 测试可能因缺少 `MEDIASTATION_TEST_POSTGRES_DSN` 跳过。报告必须明确区分“编译通过”和“行为已执行”。
- 回滚：本任务无迁移，按文件回退即可；不得使用破坏性 Git 命令覆盖用户工作树。
