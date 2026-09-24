# 启动状态展示与初始化优化

## Goal

展示后台初始化进度并限制未就绪任务，优化监听遍历，移除已退役的红果目录启动迁移与重复配置加载

## Requirements

- Show current background startup stage, elapsed time, directory discovery/registration counts, and sanitized warnings independently of task-history queries.
- Hide execution/configuration controls until startup is ready; reject premature task-center actions and scheduler aliases with HTTP 503. Preserve unknown-definition 404 responses.
- Keep Scheduler.Start last. Do not parallelize startup or move schema/probe-summary migrations into the background.
- Preserve watcher directory coverage, hidden-directory exclusion, recovery, and scanner fingerprints while avoiding ordinary-file metadata reads.
- Remove the retired HongGuo directory startup migration and orphan helpers/tests; preserve stored paths and current directory generation.
- Load runtime settings once and preserve the CPU-thread setting before expensive startup work.

## Acceptance Criteria

- [x] Progress remains available with delayed task history; controls follow readiness automatically, including status-request failure.
- [x] Premature actions return 503; unknown definitions remain 404; historical logs remain readable.
- [x] Watcher coverage/recovery tests pass, cancellation is honored, and discovery needs no ordinary-file Info.
- [x] Scheduler registers last; shutdown waits for Boot before releasing resources.
- [x] Existing download placements and new directory generation remain usable without migration.
- [x] Focused Go tests/race checks, Web lint/build, and isolated browser checks pass; unmeasured performance remains explicit.

## Authorization and Scope

The user approved the preceding analysis and implementation, explicitly retiring the HongGuo startup migration and retaining scheduler-last ordering. No production restart/database mutation, migration framework, directory cache, or concurrency redesign. Migration completion is not independently measured; retirement follows the user's instruction and never rewrites existing paths.
