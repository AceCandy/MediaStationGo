# 修复全局扫描互斥与并发入库

## Goal

同一时间只运行一个扫描任务，其他请求直接拒绝；同路径并发入库安全复用并保留来源隔离。

## Requirements

- One scan task globally per application instance, across manual, scheduled, root and STRM refresh entry points. Reject concurrent requests immediately; no waiting, queue or new execution.
- A multi-library/root scan owns the slot for its complete sequential run. Always release on completion, error or cancellation.
- File-watcher ingestion remains independent. Concurrent writes for the same path reuse one media row and retain valid catalog bindings.
- Preserve ordinary/HongGuo/NFO isolation, existing update rules, files and user state. Keep path uniqueness; no schema change.

## Acceptance Criteria

- [x] Busy manual/task-center scan APIs reject without creating an execution; scheduler rejection occurs before goroutine launch.
- [x] One admitted multi-target run processes every target sequentially and holds the slot throughout.
- [x] Completion, failure, cancellation and task-creation failure release the slot.
- [x] Real PostgreSQL independent connections reproduce the old path-insert race; fixed writers return the same saved ID, one row and valid bindings.
- [x] Cross-catalog conflicts and unrelated database failures remain errors without partial writes.
- [x] Focused regressions and race checks pass; independent read-only review has no confirmed unresolved finding.

## Scope and approval

User approved the revised global-single-scan proposal with “OK 那你做吧”. Earlier per-library/per-directory locking was explicitly superseded. No deployment, production rescan/data writes, automatic commit or push. Logs showed an HTTP scan starting at 00:21 and a batch scan entering the same HongGuo directory at 01:14; both reported path conflicts at 03:05.
