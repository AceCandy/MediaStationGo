# 接入 Resin 代理池：实施计划

## Implementation Checklist

1. 扩展 `APIConfig`、兼容迁移和服务投影，加入代理类型及 Resin 地址、加密 Token、可选 Account。
2. 在配置更新边界完成类型、地址和 Account 校验，保证公共响应与错误不泄露凭据。
3. 为 `DoubanProvider` 增加按 revision 缓存的 Resin HTTP 代理客户端，并复用现有 direct-first、失败切换和 sticky route 逻辑。
4. 扩展前端 API 类型和 Douban 高级设置：总开关、普通/Resin `Select`、Resin 地址、Token 与可选粘性标识。
5. 更新豆瓣代理契约，记录 Resin V1 接入、兼容和安全边界。
6. 独立复核 diff，确认普通代理池、Cookie、图片和其他 Provider 未被改变。

## Validation

- 后端聚焦测试：API 配置加密/公共投影/迁移、Resin URL 与 Account 校验、无 Account 与固定 Account 认证、普通/Resin 路由切换、配置 revision 重置、失败不误回退。
- 按项目 Java 约束不适用；Go 测试按既有代理池契约执行聚焦范围，不启动长期服务。
- 前端执行 `npm run lint`、`npm run build`。
- 执行 `go vet ./internal/service ./internal/handler` 与 `git diff --check`。
- PostgreSQL 迁移集成测试仅在 `MEDIASTATION_TEST_POSTGRES_DSN` 已配置时执行；未配置则明确记录未验证。

## Risky Files and Rollback Points

- `internal/service/api_config.go`：敏感字段加密与公共投影边界；发现泄漏立即回滚对应字段输出。
- `internal/service/douban.go`：请求路由核心；普通代理池测试不通过则不继续。
- `internal/database/schema_migration.go`：仅做幂等补列，不做删除或数据重写。
- `web/src/components/APIConfigsPanel.tsx`：只修改 Douban 编辑区域，不改普通代理池面板。

## Completion Gate

- PRD 中所有验收项有对应测试或人工证据。
- 独立复核无凭据、完整代理 URL、Cookie 或本地调试产物进入 diff。
- 用户批准本规划后才运行 `task.py start` 并修改产品代码。
