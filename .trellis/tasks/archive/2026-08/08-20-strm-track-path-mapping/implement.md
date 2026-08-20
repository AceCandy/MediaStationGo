# STRM 远程轨道路径映射实施计划

1. 在播放与探测设置中新增 `ffprobe.path_mappings` 文本域，并同步后端 schema。
   - 验证：后端 schema 定向测试确认 key 存在且类型为 `textarea`。
2. 在 `MediaProbeService` 内实现最小 URL 前缀→本地路径映射，只供 `resolveSource` 和 `currentSourceIdentity` 复用。
   - 验证：定向单测覆盖 scheme/host 不匹配、URL path 边界、最长前缀、URL 编码路径、查询参数剔除和路径逃逸拒绝。
3. 将映射接入 HTTP(S) STRM 轨道探测：本地文件可用时走 `Probe`，不可用或未命中时走现有 `ProbeHTTP`。
   - 验证：服务定向测试断言本地/远程 runner 分支，并断言探测期间映射源变化时不写入结果。
4. 独立复核变更范围与兼容性。
   - 验证：运行 `go test ./internal/service ./internal/handler`、`npm --prefix web run lint`、`npm --prefix web run build` 和 `git diff --check`。

## Risky Files and Rollback Points

- `internal/service/media_probe.go`：轨道探测源选择和一致性校验；若定向测试无法证明未配置时行为不变，停止实施并回到设计阶段。
- 工作树中已有用户对 `web/src/pages/TasksPage.tsx` 的未提交修改；实施时保持该文件不变。
