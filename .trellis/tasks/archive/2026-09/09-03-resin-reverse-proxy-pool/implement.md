# 实施计划

1. 扩展 `ProxyPoolService` 的全局配置读写、加密、旧字段兼容和 revision。
   - 验证：配置 round-trip、Token 密文与公共投影、旧配置 fallback、留空 Token 保留、模式切换不改普通条目。
2. 增加独立的代理池配置 GET/PUT handler 和管理路由，保持现有条目接口不变。
   - 验证：路由存在，非法模式/地址被拒绝，响应与错误不泄漏 Token。
3. 将豆瓣 Resin route 从 `http.ProxyURL` 改为集中式 URL 反向代理改写。
   - 验证：空/非空 Account、escaped path、query、HTTP 400 与 `unexpected EOF` 切换、配置 revision 重置、错误 URL 脱敏。
4. 将 Resin 配置 UI 移入 `ProxyPoolPanel`，豆瓣编辑区只保留使用代理开关。
   - 验证：普通/Resin 条件展示、刷新 round-trip、Token 留空保留、前端 lint/build。
5. 更新豆瓣代理规范，将上一版正向代理契约替换为全局 Resin 反向代理契约。
6. 独立复核跨层契约、安全边界和兼容路径。
   - 验证：聚焦 Go 测试、`go vet ./internal/service ./internal/handler`、前端 lint/build、`git diff --check`。

## 风险文件与回滚点

- `internal/service/proxy_pool.go`：全局配置所有权与旧数据 fallback。
- `internal/service/douban.go`：所有豆瓣元数据请求的共同路由入口。
- `internal/handler/api_config.go`、`internal/handler/routes_admin.go`：管理 API 合约。
- `web/src/components/APIConfigsPanel.tsx`：豆瓣编辑与代理池面板的职责迁移。
- 不删除数据库列和普通代理条目；每一步都可通过回滚代码恢复上一版行为。
