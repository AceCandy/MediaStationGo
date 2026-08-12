# 移除系统更新功能

## Goal

完整移除应用内的系统更新能力，避免继续暴露 Docker Compose 镜像检查、拉取和重启入口。

## Background

- 当前功能同时存在于系统设置侧栏、用户菜单快捷入口、前端 API、管理员 HTTP 接口和后端更新服务。
- 更新服务会探测 Docker/Compose、执行 shell 命令并将更新过程登记到共享任务中心。
- Docker Compose 模板包含只为该功能提供的 Docker socket 挂载说明和更新镜像环境变量。

## Requirements

1. 删除“系统更新”侧栏页面及用户菜单“一键更新系统”快捷入口。
2. 删除前端系统更新状态类型、API 调用、页面组件、设置组和路由。
3. 删除后端 `/admin/system/update`、`/check`、`/apply` 接口及其专属 handler、service、Docker/Compose 探测、shell 执行代码和测试。
4. 删除系统更新设置元数据、`TaskKindUpdate` 以及 Compose 模板中的专属 Docker socket 说明和 `MEDIASTATION_UPDATE_IMAGE` 变量。
5. 保留被其他功能使用的设置存储、任务中心、Docker/命令执行相关独立能力以及其他系统设置页面。
6. 在启动迁移中删除 `system.update.image`、`system.update.watchtower_image`、`system.update.command` 和 `system.update.compose_dir` 历史设置，不影响共享 `settings` 表中的其他数据。

## Acceptance Criteria

- [x] 导航、设置页面和用户菜单中不再出现系统更新入口。
- [x] 旧前端路由 `/admin/settings/system-update` 不再注册。
- [x] 后端不再注册 `/admin/system/update*` 路由，也不再构建系统更新服务。
- [x] 除退役迁移及其回归测试外，业务代码和 Compose 模板中不再存在系统更新专属符号、配置键、环境变量或文案。
- [x] 共享设置、任务中心及其他管理功能保持原有结构。
- [x] 启动迁移会幂等删除 4 个系统更新历史设置键，并保留其他设置。
- [x] 前端 lint/build、后端定向测试或编译检查以及 `git diff --check` 通过。

## Out of Scope

- 改造应用版本注入、发布流程或容器部署方式。
- 修改任务中心中与整理、扫描、刮削等其他任务有关的逻辑。

## Technical Notes

- 旧 URL 和 API 直接消失，不提供兼容重定向或废弃响应，因为目标是完整剔除功能。
- `TaskTrackerService`、`TaskUpdate` 等为共享基础设施，仅删除系统更新专属的 `TaskKindUpdate`。
- `HardDrive` 图标仍被存储导航使用，不能随系统更新路由一起删除。
