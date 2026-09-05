# Implementation Plan

1. 扩展 STRM 服务解析删除目标
   - 复用实时 sidecar 读取、`mapRemoteProbePath` 和文件管理 roots。
   - 返回文件路径、父目录和可信根；在预览与删除时执行相同安全校验。
   - 增加本地目标、远程未映射、远程映射、根目录与符号链接边界测试。

2. 增加管理员预览/删除接口
   - GET 只返回服务端解析出的本地绝对路径。
   - DELETE 只接收 `delete_parent`，删除前重新解析。
   - 覆盖管理员权限、请求绑定和错误映射。

3. 增加详情页删除确认 UI
   - 仅管理员显示删除按钮。
   - 点击后加载预览并打开专用弹窗。
   - 复选框实时在文件路径和父目录间切换“最终将删除”的路径。
   - 确认成功后关闭弹窗并更新当前 STRM 删除状态。

4. 验证与独立复核
   - 运行相关 Go 单元测试。
   - 在 `web/` 运行 `npm run lint` 与 `npm run build`。
   - 运行 `git diff --check`。
   - 使用 `trellis-check` 独立核查跨层契约、删除边界及未请求的数据变更。

## Expected Files

- `internal/service/scanner_strm.go`：删除目标解析与执行逻辑。
- `internal/service/*_test.go`：危险删除路径的最小回归测试。
- `internal/handler/routes_admin.go` 与对应 handler：管理员接口。
- `web/src/api/library.ts`：预览与删除调用。
- `web/src/pages/MediaDetailMetadata.tsx`：按钮及动态确认弹窗。

## Rollback Points

- 不含数据库迁移；任一步均可按新增接口和 UI diff 单独回退。
- 若映射可信根校验无法可靠复用现有解析规则，则停止实现，不放宽到任意绝对路径删除。
