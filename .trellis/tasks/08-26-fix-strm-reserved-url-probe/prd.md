# 修复 STRM 保留字符与 ffprobe 错误诊断

## Goal

统一修复 STRM 媒体 URL 中未编码 `#` 导致的路径截断，使探测、本地映射、直接播放和 Emby 媒体识别使用一致目标，并让 ffprobe 失败返回可诊断且安全的原因。

## Background

- 样本 `.strm` 包含 HTTP URL，目录名和文件名均以未编码的 `#` 开头。
- Go URL 解析把首个 raw `#` 后内容作为 fragment；`mapRemoteProbePath` 因而只映射到年份目录。
- `localMediaProbeSource` 只执行 `os.Stat`，目录会被误当成可探测本地媒体；ffprobe 对目录返回 exit status 1。
- `FFprobeService` 使用 `cmd.Output()`，但没有读取 `exec.ExitError.Stderr`，任务详情只显示 `ffprobe failed: exit status 1`。
- `mapRemoteProbePath` 同时服务于轨道探测和播放失败时的本地回退；直接播放和 Emby 容器识别还会直接消费 `Media.STRMURL`。

## Requirements

- R1：对 STRM HTTP(S) URL 中的所有 raw `#` 统一按历史未转义路径字符处理并规范化为 `%23`；已编码 `%23` 不得双重编码。
- R2：规范化必须覆盖扫描持久化、远程探测、本地 path mapping、直接播放重定向及 Emby 远程容器识别，不得按特定片名或单一调用方修补。
- R3：合法 URL query 必须保持 query 语义，不能进入本地文件路径或被删除。
- R4：本地探测源只接受普通文件；目录及其他非普通文件必须在启动 ffprobe 前被拒绝。
- R5：本地和远程 ffprobe 执行失败时返回经过单行化、限长和源地址脱敏的 stderr 摘要，同时保持现有 `ffprobe failed` / `remote ffprobe failed` 分类前缀。
- R6：使用 Go 标准库和项目现有函数，不新增依赖、配置项、迁移或单用途抽象。
- R7：同步更新 STRM probe/path mapping 合同，记录 raw `#` 兼容与普通文件约束。

## Acceptance Criteria

- [x] AC1：raw `#` 位于 STRM URL 的目录名或文件名时，扫描保存规范化 URL，path mapping 得到完整普通媒体文件，Probe 使用该文件。
- [x] AC2：历史数据库中仍为 raw `#` 的 `Media.STRMURL` 无需迁移即可在探测、直接播放和 Emby 容器识别时正常工作。
- [x] AC3：已编码 `%23` 保持原样；带合法 query 的 URL 保留 query，且 query 不进入本地路径。
- [x] AC4：映射结果为目录或其他非普通文件时不执行本地 ffprobe，并按现有合同回退远程探测；直接本地源为非普通文件时返回明确错误。
- [x] AC5：ffprobe 失败错误包含有意义的 stderr 摘要；错误为单行且长度受限，不包含完整本地源路径、远程 URL 或 query 凭证。
- [x] AC6：现有 STRM、path mapping、播放和 ffprobe 测试保持通过，新增最小回归测试覆盖 AC1–AC5。

## Out of Scope

- 修改外部 Symedia 的 STRM 生成逻辑。
- 批量重写磁盘上已有 `.strm` 文件或迁移数据库历史值。
- 猜测 raw `?` 是文件名字符还是合法 query；真实文件名含 `?` 时生成端必须编码为 `%3F`。
- 改变不含 raw `#` 的正常本地媒体、STRM 或 HTTP 媒体行为。

## Key Decisions

- 用户已确认：STRM HTTP(S) URL 中的 raw `#fragment` 一律按历史未编码路径后缀处理，而不是保留播放器 fragment 语义。
- 兼容在消费端实时完成，同时扫描新记录时保存规范化 URL；不要求数据迁移。
- query 继续遵循现有合同，不参与本地路径构造。

## Evidence

- `scanner_strm.go:15-37` 读取 STRM URL；`scanner_local_ingest.go:206-210` 持久化 `Media.STRMURL`。
- `media_probe.go:277-321,334-402` 解析探测源、映射远程 URL，并把 stat 成功的目录当成本地源。
- `stream_file.go:36-74` 直接重定向 `Media.STRMURL`；`stream_redirect_resolver.go:231-244` 的播放本地回退已要求普通文件。
- `emby_playback.go:339-369` 从远程 STRM URL path 推导容器。
- `ffprobe.go:68-133` 的本地和远程执行路径未把 stderr 加入错误。
- `task_detail_test.go:104-112` 已验证完整 URL 脱敏；任务详情会持久化并通过 API/UI 展示。
