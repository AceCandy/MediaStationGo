# Design

Use a container-owned, mutex-protected startup snapshot with fixed stage labels, elapsed times, directory counters, and sanitized warnings. Expose an administrator-only lightweight endpoint independently of history. A missing tracker in explicitly constructed containers preserves existing standalone behavior.

Boot remains sequential and Scheduler.Start remains last. Task recovery and path normalization failures prevent readiness; existing recoverable initialization failures stay visible as warnings. Record stage durations. Coordinate Boot/Close so cancellation finishes startup before resources are released.

Task-center run/configuration handlers and shared scheduler-trigger helpers check readiness; validate unknown definitions first. The frontend hides actions until readiness is confirmed and displays initialization/unknown state instead of idle. Polls are independent, non-overlapping, and canceled on unmount.

Replace watcher-only discovery with filepath.WalkDir, ignoring ordinary files without Info and preserving hidden-directory exclusion, real errors, and failed-root recovery. Report bounded discovery/registration progress. Keep scanner walk unchanged; no cache or parallel walk.

Retire migratePendingDownloadDirectories and orphan helpers/tests. Existing downloads continue using persisted placements; new directory generation is unchanged. Remove main's duplicate runtime-setting load and apply the CPU limit after service construction loads settings.

Files: startup/container/builder/watcher/download services; task/scheduler handlers/routes; task page/API; focused tests/browser script; background-task contract. No schema/dependency changes.
