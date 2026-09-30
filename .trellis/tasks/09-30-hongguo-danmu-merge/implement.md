# 实施与验证计划

## 当前阶段

- 2026-09-30 用户已明确批准最终隔离方案，进入实现阶段。
- PRD、设计与执行计划已审阅，后续按范围实施并验证。
- 已实现业务代码；迁移与回归仅使用独立 PostgreSQL 测试容器，未读取登录凭据、迁移现有数据库或部署。

## 顺序

1. **新表和合并规则**：新模型单独注册，按作品/集号读取及批量冲突忽略插入，纯函数合并排序。
   - 验证 100 旧＋95 旧/20 新→120、幂等、同文不同 ID、并发写唯一、跨作品/季/集隔离、源视频 ID 变化。
   - 真实 PostgreSQL 隔离 schema 上重复迁移，比较已迁移旧库的哨兵行。禁止向生产库执行验证迁移。
2. **App 配置**：新 provider 的类型化参数包、加密/脱敏、有效参数校验、清除与匿名默认；现有 Web 页面只加红果编辑分支。
   - 验证空配置、保存/更新/清除/禁用、下一次请求生效、无敏感回显或日志；旧 provider 的 round-trip 和代理行为不变。
   - 检查 `CryptoService` 降级及解密失败，不能将明文保存或密文外发。
3. **弹幕客户端**：新的固定来源请求/完整签名、真实时长、窗口解析、限速和有界退出。
   - 用确定性签名向量和 HTTP transport fixture 验证 Header、Cookie 参与签名、aid/body/query 一致、分页、长数字 ID、业务码和错误脱敏。
   - 覆盖成功空响应、非法 JSON、超大响应、重定向、超时、循环 cursor、时间不前进和分段失败。
   - 保持旧 `download_app.go` 请求与签名不变，重跑 App 下载/合集测试。
4. **服务/Emby**：目标可见性解析、历史与实时合并、异步新条目保存、关闭等待；只接现有 raw 出口，生成插件 XML。
   - 测试权限先于任何历史返回/上游调用；同合集同集号不同源不串；非红果与现有占位路由回归保持一致。
   - 异步保存受控阻塞时响应已返回；失败不撤销响应；关闭等待结束且无 WaitGroup Add/Wait 竞态。
   - XML 解码检查时间单位、毫秒精度、字符转义、非法字符、ID 精度和空文档。
5. **Web 与文档**：更新 Emby API 目录、红果配置说明和必要规格；不添加新管理路由。
   - Web lint/build；浏览器检查配置保存/清除/空参数、旧 provider 未回归、管理员权限、移动端布局和敏感值不回显。
6. **独立复核**：执行 `trellis-check`，审查所有共享交点及 diff，再报告已验证/未验证项。
   - 登录态对照需要用户自行在本机配置，不读取其他服务/浏览器凭据。
   - YAMBy 实机、生产迁移及部署未完成时明确列出，不以源码/模拟结果冒充。

## 验证命令

新测试名称在实现时确定，下面按命名约定收敛范围：

```sh
go test ./internal/hongguo -run 'Danmu|App|Album' -count=1
go test ./internal/repository ./internal/database -run 'Danmu|HongGuoDetailIsolation|HongGuoBinding' -count=1
go test ./internal/service ./internal/handler -run 'Danmu|APIConfig|ApiConfig|HongGuo.*(Favorite|Played|Playback|Download)|EmbyDiscovery' -count=1
go test -race ./internal/service ./internal/repository -run 'Danmu' -count=1
go vet ./internal/hongguo ./internal/service ./internal/handler
npm --prefix web run lint
npm --prefix web run build
git diff --check
```

数据库断言必须实际配置 `MEDIASTATION_TEST_POSTGRES_DSN` 并运行，跳过不算通过。使用项目隔离 schema 测试工具；并发测试连接也必须固定 search_path。只查看环境变量是否配置，禁止打印 DSN 或其他秘密。是否能安全启动独立调试服务在实施时确认；若启动，结束前关闭并清理产物。

## 执行前检查

- 读取剩余与具体实现相关的前端路由/设计规范、生命周期规范；主代理亲自读取即将修改的确切代码。
- 确認参考协议版本、许可证和签名算法所需依赖，不将最新版未验证接口套到旧请求中。
- 最终方案获得后续明确批准后再运行 `task.py start`；若改变公共行为或扩大范围，重新确认。
- 不自动提交、归档、部署或运行生产迁移。
