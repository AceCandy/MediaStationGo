# 外部 API 代理池实施计划

## 1. 持久化与配置契约

- [ ] 在 `internal/model/api_config_legacy.go` 增加 `UseProxyPool`，在 `internal/model/model.go` 注册 `ProxyPoolEntry`。
- [ ] 在 `internal/service/api_config.go` 贯通 `PublicView`、`Resolved`、`APIConfigPatch` 和更新投影；成功更新/删除豆瓣配置时递增进程内单调 revision，并通过 `Resolved` 提供给运行时。
- [ ] 在 `internal/database/schema_migration.go` 的 APIConfig 兼容迁移中幂等补充 `UseProxyPool`。
- [ ] 增加数据库/API 配置回归，证明旧行默认关闭、更新/读取往返一致。

验证：

```bash
go test ./internal/database ./internal/service -run 'Test.*(APIConfig|ProxyPool)'
```

回滚点：仅包含新增表/列和字段贯通；失败时尚未改变任何请求路径。

## 2. 安全代理池服务与管理 API

- [ ] 新增最小 `ProxyPoolService`：URL 规范化/校验、整条 URL 加密、事务式有序替换与物理删除、安全投影、运行时快照和 generation 重建。
- [ ] 加入 `service.Container` 和 builder；旧 Transport 更新后只关闭空闲连接。
- [ ] 在现有管理员路由组注册 `GET/PUT /api-proxy-pool`，对外完整路径为 `/api/admin/api-proxy-pool`。
- [ ] 测试新增、保留、替换、删除、重排、无效输入原子失败和未知/重复 ID。
- [ ] 测试数据库仅保存密文，响应 JSON 不含用户名、密码、完整认证 URL 或密文。
- [ ] 测试 `http`、`https`、`socks5`、`socks5h` 均能由 Go 1.25 标准 Transport 构建，不新增代理依赖。
- [ ] 捕获校验失败、解密失败和 handler 错误输出，断言错误与日志不包含输入 URL、Userinfo、密文或 Cookie。

验证：

```bash
go test ./internal/service ./internal/handler -run 'Test.*ProxyPool'
```

回滚点：路由可单独移除；数据库表保留不会影响旧代码。

## 3. 豆瓣 400 选路状态机

- [ ] 为 `DoubanProvider` 注入代理池服务，保留原有 client，并新增 `Proxy=nil` 的真正直连 client。
- [ ] 将 Search、Discover、Detail 的 JSON GET 收敛到同一个可重放 helper；保持现有请求头、Cookie、响应解析、错误文本和详情降级分类。
- [ ] 实现豆瓣独立的 sticky route + generation 状态，只在精确 HTTP 400 后串行重选。
- [ ] 配置关闭时继续使用旧 client；列表变更、关闭或重新开启时重置为真正直连。
- [ ] 用可控 RoundTripper/测试服务覆盖：
  - 直连成功并保持直连；
  - 直连 400 后按序命中代理并粘性复用；
  - 当前代理 400 后从直连重新开始；
  - 全代理 400 后最终直连成功/失败；
  - 空代理池的两次直连；
  - 网络错误及 403/404/429/5xx 不切换、不改线路；
  - 配置 generation/revision 变化和并发 400 不产生越序线路；
  - 连续保存关闭→开启且中间没有豆瓣请求时，下一次仍从真正直连开始。

验证：

```bash
go test ./internal/service -run 'TestDouban.*ProxyPool|TestDouban.*Detail|TestDouban.*Discover'
```

回滚点：关闭 `use_proxy_pool` 即时恢复原请求路径；代码回滚不需要删除代理数据。

## 4. 外部 API 页面

- [ ] 在 `web/src/api/api_configs.ts` 增加 `use_proxy_pool` 和代理池 GET/PUT 类型契约。
- [ ] 在 `APIConfigsPanel.tsx` 的豆瓣高级设置中增加“使用代理池”开关。
- [ ] 在 Provider 表格下方增加有序代理池面板，支持新增、替换、删除、上移、下移与保存。
- [ ] 认证代理只显示无 Userinfo 的 `display_url` 和认证状态；新增/替换输入使用 password 类型，未修改节点只提交 ID。
- [ ] 检查 loading、空列表、保存失败、禁用按钮以及窄屏下无横向操作遮挡。

验证：

```bash
cd web && npm run lint && npm run build
```

## 5. 独立复核与收尾检查

- [ ] 按 PRD 状态机逐项核对数据库 → Service → Handler → TypeScript → UI → Douban 请求的数据流。
- [ ] 搜索新增字段和路由的所有生产调用点，确认未接入图片、AI 或其他 Provider。
- [ ] 检查所有错误与日志参数，确认无 Cookie、Userinfo、完整代理 URL 或密文。
- [ ] 检查旧环境/系统代理路径仅在开关关闭时保留，启用态真正直连的 `Transport.Proxy == nil`。
- [ ] 执行格式与静态检查；不启动服务。

验证：

```bash
gofmt -w <本任务修改的 Go 文件>
go test ./internal/service ./internal/handler ./internal/database
go vet ./internal/service ./internal/handler ./internal/database
cd web && npm run lint && npm run build
git diff --check
```

未配置 `MEDIASTATION_TEST_POSTGRES_DSN` 时，数据库集成测试会明确跳过；最终交付必须单独说明这一项未被真实 PostgreSQL 验证。
