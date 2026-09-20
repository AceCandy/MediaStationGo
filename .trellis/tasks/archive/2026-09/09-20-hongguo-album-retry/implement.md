# 实施与验证

1. 保留安全错误分类，撤销递增退避和全局部分失败状态改动。
2. 详情附带查询失败只记警告、交给独立补充任务，不设冷却；验证每轮只尝试一次、下一轮立即重试、历史未来时间兼容、详情与合集失败隔离、取消和检查点错误。
3. 运行相关 Go 测试、真实 PostgreSQL 隔离测试、diff 检查；独立复核后同步契约。最终没有 Web 修改，不需 Web 构建。

验证不得使用生产任务作为测试对象；测试用隔离 schema。明确报告未执行的真实上游与部署验证。

## 验证结果

- 移除冷却后重新验证：服务/仓库合集专项、详情失败重试与 404 暂缓测试在独立 PostgreSQL 上通过（含 `-race`）；覆盖每轮一次、失败项下一轮立即尝试、历史未来时间不再阻挡、取消和检查点错误。`go vet ./internal/service ./internal/repository` 与 `git diff --check` 通过；已独立复核游标和调用方，临时数据库已停止并删除。
- `go test ./internal/hongguo -count=1` 通过。
- 临时独立 PostgreSQL 下，合集失败续跑、详情失败交接（手动/批量）、保存/重试检查点失败、取消、导入隔离、详情失败重试、404 暂缓、事件唤醒专项测试通过；服务测试含 `-race`。
- `TestHongGuoOfficialAlbumsAndBackfill` 在真实 PostgreSQL 下通过。
- `go vet ./internal/hongguo ./internal/service`、`git diff --check` 通过；独立只读复核无阻断问题。
- `TestHongGuoDiscoveryDefersDetailsAndResumes` 失败：测试对请求序列的断言未隔离自动详情刷新。通过 Go overlay 使用修改前两个服务文件复现相同失败，保留原测试，不扩展本次范围。
- 未请求真实官方接口、未部署、未执行全量测试。临时 PostgreSQL 已关闭，测试数据库、基线 overlay 和日志已清理。
- 用户已确认提交并归档；只纳入本任务的红果代码、测试与契约，其他并行改动保留。
