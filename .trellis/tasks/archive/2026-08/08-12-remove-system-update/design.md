# Design

## Boundary

按入口到实现的完整链路删除：

```text
导航/用户菜单 -> 设置页/更新面板 -> adminAPI
              -> /admin/system/update* -> SystemUpdateService
              -> Docker/Compose 探测与 shell 更新命令
```

同时清理只为该链路存在的设置元数据、任务类型、测试和 Compose 配置。

## Compatibility

- 旧页面路由不再匹配，按现有路由兜底行为处理。
- 旧 API 不再注册，由 Gin 返回未匹配路由的响应。
- 启动迁移按精确键名删除 4 条历史系统更新设置，不删除共享 `settings` 表或其他设置记录。

## Shared Code

保留通用 `SettingsPage`、`adminAPI` 其他方法、设置仓储、任务追踪服务以及被其他功能使用的 Docker/进程能力。删除后通过全仓引用检查确认没有孤立专属符号。

## Rollback

源码与模板删除可通过恢复单次变更回滚；迁移删除的历史系统更新设置不会自动恢复。
