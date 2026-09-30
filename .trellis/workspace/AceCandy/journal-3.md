# Journal - AceCandy (Part 3)

> Continuation from `journal-2.md` (archived at ~2000 lines)
> Started: 2026-09-30

---



## Session 168: 媒体库扫描性能优化完成
<!-- trellis-session: v=2 fp=a766e4a43e0e8f20 -->

**Date**: 2026-09-30
**Task**: 媒体库扫描性能优化完成
**Branch**: `main`

### Summary

完成扫描规则批复用、红果索引批刷新及 NFO 共享读取与依赖快照增量跳过，提交代码并归档任务。

### Main Changes

- 复用扫描识别规则，合并红果作品与合集搜索刷新，保留逐文件事务和来源隔离。
- NFO 共享文档与图片处理缓存，保存稳定依赖快照并提前跳过未变化文件，保留资料变更检测和失败恢复。

### Git Commits

| Hash | Message |
|------|---------|
| `a221959` | perf(scanner): 优化批次识别、红果索引与 NFO 增量扫描 |

### Testing

- [OK] 隔离 PostgreSQL 的 service/repository 定向回归及 race、go vet、gofmt、git diff --check 通过，独立复核完成。
- [OK] 101 文件未变化 NFO 重扫入库事务从 101 次降到 0 次；侧车、图片、候选、视频身份变化及资产修复均已覆盖。

### Status

[OK] **Completed**

### Next Steps

- 尚未部署或复扫生产大库，实际扫描耗时待部署后实测；旧记录首次需建立依赖快照。


## Session 169: 后台待办检查性能优化
<!-- trellis-session: v=2 fp=559c15335b7d8821 -->

**Date**: 2026-09-30
**Task**: 后台待办检查性能优化
**Branch**: `main`

### Summary

完成刮削与媒体探测两个后台待办查询优化并提交；本次无活动任务目录，按流程跳过目录归档。

### Main Changes

- 刮削检查拆分为 pending/running EXISTS，复用既有部分索引。
- 媒体探测改用共享 NOT EXISTS 排除当前文档，补充两个覆盖部分索引，保留自动与手动筛选及事件唤醒语义。
- 补充状态与范围回归、实际 SQL 通用预编译计划验证及后台任务规范。

### Git Commits

| Hash | Message |
|------|---------|
| `49211b6` | perf(background): 优化刮削与媒体探测待办检查 |

### Testing

- [OK] 真实 PostgreSQL 隔离 schema 相关回归和竞态检测通过，新增合法剧集层级用例已复测通过。
- [OK] 20,700 条混合媒体的空闲检查计划命中索引：刮削约 0.020ms，探测约 6.191ms；该耗时仅代表测试数据。
- [OK] go vet、gofmt、git diff --check 和独立只读复核通过。

### Status

[OK] **Completed**

### Next Steps

- 尚未迁移运行数据库或重启服务，线上吞吐效果未验证；首次索引创建可能延长启动并短暂阻塞写入。
