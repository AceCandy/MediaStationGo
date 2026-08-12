# Implementation Plan

1. 删除前端系统更新组件、设置组、路由、API 和用户菜单快捷入口。
2. 删除后端 handler、路由注册、服务构建、专属服务文件、任务类型、设置元数据和测试。
3. 删除三份 Docker Compose 模板中的系统更新 socket 说明与环境变量。
4. 增加退役迁移，按精确键名删除 4 个历史系统更新设置并保留其他设置。
5. 搜索系统更新专属符号、配置键、URL、环境变量和文案，清理迁移及其测试之外的遗漏引用。
6. 运行前端 lint/build、真实 PostgreSQL 下的后端定向测试和 `git diff --check`。
7. 独立复核改动范围、共享能力保留情况和残留风险。

## Risky Files and Rollback Points

- `service.go` / `service_builder.go`：只删除 `SystemUpdate` 字段和初始化，保留其他服务顺序。
- `routes_admin.go` / `appRoutes.tsx`：只删除系统更新路由，保留同组其他路由。
- `task_tracker.go`：只删除未再使用的 `TaskKindUpdate`。
- `docker-compose*.yml`：只删除系统更新专属注释、socket 示例和环境变量。
- `schema_migration.go`：只删除 4 个精确设置键；操作幂等，但已删历史值不可恢复。

## Validation

```bash
rg -n --glob '!docs/cankao/**' --glob '!.trellis/**' --glob '!internal/database/schema_migration.go' --glob '!internal/database/system_update_retirement_test.go' 'system\.update|MEDIASTATION_UPDATE|SystemUpdate|systemUpdate|system-update|系统更新|TaskKindUpdate|一键更新' .
cd web && npm run lint && npm run build
MEDIASTATION_TEST_POSTGRES_DSN='<postgres test dsn>' go test ./internal/database ./internal/handler ./internal/service
git diff --check
```
