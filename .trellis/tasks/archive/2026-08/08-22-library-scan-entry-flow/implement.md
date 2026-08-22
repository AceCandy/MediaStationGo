# 调整媒体库扫描入口与自动触发：实施计划

## 1. 保存结果与共享扫描启动逻辑

- 扩充媒体库创建结果，明确 `created` 与本次 `added_roots`。
- 在现有扫描 Handler 文件中抽取整库/单路径后台任务启动逻辑，复用任务持久化、进程内去重、指标和详情写入。
- 保持现有扫描 HTTP API 的状态码与响应含义。
- 验证：服务测试覆盖真正新建与合并追加路径的结果。

## 2. 任务中心按库手动扫描

- 为 scheduler 的 `library_scan` 手动运行上下文增加单库目标；定时上下文保持无目标。
- `taskDefinitionRunHandler` 对 `library_scan` 解析并校验必填 `library_id`，调用带目标的 scheduler 异步入口。
- `jobScanLibraries` 在存在目标时只扫描目标库，否则保持全库循环。
- 更新任务定义文案，体现新增后自动触发。
- 验证：Handler 测试覆盖空值/不存在媒体库，service 测试覆盖手动单库与定时全库。

## 3. 保存后自动扫描

- 真正创建新库后启动 `event` 整库扫描任务。
- 合并到已有库或通过新增路径 API 添加路径后，只为本次新增且启用的路径启动 `event` 单路径任务。
- 自动扫描启动错误只记录日志，保存响应照常成功。
- 验证：Handler 测试确认任务可见、范围正确，保存失败时不启动扫描。

## 4. 前端入口收敛

- 任务页为 `library_scan` 增加独立的必选媒体库状态、下拉框和 `library_id` 请求参数。
- 删除媒体库管理弹窗整库/路径扫描按钮、回调和 Hook 方法。
- 删除媒体库详情页立即扫描按钮、扫描进度 props、`useLibraryScanStatus` 调用及孤立文件。
- 清理仅由这些入口使用的 API helper、类型和 import。
- 验证：`npm run lint`、`npm run build`。

## 5. 独立复核与质量门

- 独立检查手动/定时/event 三种触发归属、单库范围和任务日志详情。
- 运行针对性 Go 测试：
  - `go test ./internal/handler -run 'TestScanLibraryHandler|TestTaskDefinition|TestCreateLibrary|TestCreateLibraryRoot'`
  - `go test ./internal/service -run 'TestScheduler.*Scan|TestScanLibrary|TestCreateLibrary'`
- 运行 `git diff --check`，检查工作树中无临时文件或无关改动。
- 若针对性测试暴露共享编译问题，再扩大到相关 package 测试；不默认执行全仓测试。

## 回滚点

- 前端选择器和按钮删除可独立回滚，不影响保留的扫描 API。
- scheduler 单库目标只影响显式携带 `library_id` 的手动任务；删除该分支即可恢复旧的全库手动行为。
- 保存后自动触发位于持久化成功之后，移除触发调用即可回滚，不涉及数据迁移。
