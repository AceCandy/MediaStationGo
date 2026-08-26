# 实施计划

1. 更新共享 STRM URL 处理
   - 在现有 STRM URL 代码中增加 raw `#` 规范化。
   - 接入扫描持久化、probe/path mapping、直接播放和 Emby 容器识别。
   - 验证：定向测试覆盖 raw `#`、`%23`、query。

2. 收紧本地 probe source
   - `localMediaProbeSource` 拒绝非普通文件。
   - 保持远程 mapping 不可用时的现有远程 fallback。
   - 验证：目录映射不调用本地 runner；直接目录源返回明确错误。

3. 改善 ffprobe 错误
   - 从 `exec.ExitError.Stderr` 构造限长、单行、脱敏摘要。
   - 本地与远程 probe 共用同一格式化逻辑并保留错误前缀。
   - 验证：stderr 可见，路径/URL/query secret 不可见，原 exit error 仍可匹配。

4. 同步合同
   - 更新 `shared-media-metadata.md` 的 STRM URL、path mapping、失败诊断与测试合同。

5. 质量门
   - 运行相关 Go 定向测试，不执行无关全量构建。
   - 检查 `gofmt`、`git diff --check`、工作树范围及敏感信息。
   - 使用独立 Trellis 检查代理复核规范一致性、调用链和测试缺口。

## Planned Validation

```bash
go test ./internal/service -run 'Test(MapRemoteProbePath|MediaProbe|Stream|FFprobe|Emby)'
git diff --check
```

若定向正则遗漏实际新增测试名，则改为运行 `go test ./internal/service`；不启动服务，不执行全仓构建。

## Risky Files and Rollback Points

- `scanner_strm.go` / `stream_file.go`：影响历史 STRM 播放目标；通过 raw `#`、`%23` 和 query 三组用例约束。
- `media_probe.go`：影响本地与映射 probe source；通过普通文件、目录、missing fallback 三组用例约束。
- `ffprobe.go`：错误文本进入任务日志；通过限长与脱敏用例约束。
