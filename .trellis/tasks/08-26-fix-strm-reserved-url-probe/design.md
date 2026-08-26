# STRM 保留字符与 ffprobe 错误诊断设计

## Boundary

本次只修改后端 service 层与相关测试、规范。前端、数据库 schema、外部 STRM 生成器不变。

## Data Flow

1. STRM HTTP(S) URL 进入共享规范化函数：仅把 raw `#` 替换为 `%23`，不触碰已有百分号编码或 query。
2. 新扫描记录保存规范化 URL；历史记录在探测、映射、播放和 Emby 容器识别入口实时规范化。
3. `mapRemoteProbePath` 继续使用 `net/url` 解码后的 Path，并继续排除 query。
4. 本地 probe source 在构造前确认 `Mode().IsRegular()`；远程映射若未得到普通文件则保持现有远程 fallback。
5. ffprobe 失败时从 `exec.ExitError.Stderr` 提取摘要，替换精确源路径/URL，复用现有 URL sanitizer，折叠空白并限制长度，再包装原错误。

## Minimal Change Shape

- 在现有 STRM URL 代码附近增加一个包内小函数，不新增文件或类型层次。
- 调整已确认的共享消费点，不修改仅判断“是否远程”的布尔逻辑。
- 在 `ffprobe.go` 内增加一个供本地/远程两条路径共用的错误格式化函数。
- 复用现有测试文件与 stub，不引入测试框架或 fixture 层。

## Compatibility

- `%23` 不包含 raw `#`，因此不会双重编码。
- `strings.ReplaceAll(raw, "#", "%23")` 不改变 `?query` 的位置和值。
- 根据已批准策略，原本刻意使用 URL fragment 的 STRM 会改为路径语义；这是明确接受的兼容变化。
- 历史数据库无需回填；后续正常扫描会自然保存规范化值。

## Security and Diagnostics

- stderr 只作为失败摘要，不写入成功结果或 probe document。
- 摘要必须先替换当前本地 path/远程 URL，再执行统一 URL 脱敏；不得记录远程 query token。
- 多行和控制空白折叠为单行，设置小而固定的长度上限，防止任务日志膨胀。
- 保留原 `exec` error 作为 `%w`，不破坏错误分类和 `errors.As`。

## Rollback

改动不涉及持久化迁移。回滚代码即可恢复旧行为；已保存的 `%23` 是标准合法 URL，旧代码仍能正确解析。
