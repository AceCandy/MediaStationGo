# 实施清单

## 1. PlaybackInfo 请求内数据复用

- [x] 调整可播放媒体解析，使 PlaybackInfo 获得一次解析的 selected media 与 visible siblings。
- [x] 一次 `LoadMany` 得到 probe documents，并复用于缺失修复、选择校验和媒体源构造。
- [x] 每个 sibling 一次获得字幕 selections，并复用于校验和 `MediaStreams`。
- [x] 增加/扩展定向测试，验证 metadata/concrete 两条路径、probe 加载次数与字幕发现次数。
- [x] 验证：`go test ./internal/service -run 'TestEmby.*(PlaybackInfo|MediaSource|Subtitle)' -count=1`

回滚点：仅回滚 service 层请求内参数与对应测试，不影响后续字幕 handler。

## 2. 字幕交付 concrete ID 快路

- [x] handler 对 concrete media ID 使用现有可见性校验直接进入字幕服务；未命中时保留 `PlayableMediaID` 回退。
- [x] 字幕服务在一次交付内复用已加载 media 和 probe document，仍实时发现 sidecar、重校验 index 与路径边界。
- [x] 扩展 concrete、metadata/season/series fallback、sidecar 删除和路径安全测试。
- [x] 验证：`go test ./internal/handler ./internal/service -run 'TestEmby.*Subtitle' -count=1`

回滚点：handler 快路与 subtitle 私有已加载路径可整体回滚，不改变路由。

## 3. 普通媒体 ID 解析快路

- [x] `playableMedia` 先识别 concrete/direct metadata media，再 fallback 到 season/series group。
- [x] 保持 preferred version 与用户可见 sibling 规则。
- [x] 增加 concrete、metadata、season、series 的回归覆盖。
- [x] 验证：`go test ./internal/service -run 'TestEmby.*(Playable|Version|PlaybackInfo)' -count=1`

回滚点：仅恢复 `playableMedia` 原顺序。

## 4. Web 外部播放器单请求

- [x] `ExternalPlayerButton` 只调用 `externalPlayers(mediaId)` 并使用响应 `url`。
- [x] 保留 `playbackAPI.externalURL`。
- [x] 验证：`cd web && npm run lint && npm run build`

回滚点：恢复组件内并行请求。

## 5. Web 返回路径复用

- [x] `PlayerPage` 保持 `location.state.from` 优先，其余调用 `mediaLibraryBackTarget(media)`。
- [x] 移除仅由手写路径使用的 imports。
- [x] 验证：`cd web && npm run lint && npm run build`

回滚点：恢复原局部路径拼接。

## 6. 独立复核与质量门禁

- [x] 对照 PRD 逐项检查对外行为、权限、compat fallback 与 sidecar 实时语义。
- [x] 运行相关后端定向测试。
- [x] 运行 `go vet ./internal/service ./internal/handler`。
- [x] 运行 `cd web && npm run lint && npm run build`。
- [x] 运行 `git diff --check` 并确认无隐私文件、临时调试产物或无关改动。
- [x] 使用 `trellis-check` 独立复核后提交。

## Pre-start Gate

- [x] PRD、设计和实施清单已由用户审核。
- [x] 用户在看到最终规划摘要后明确批准开始实现。
