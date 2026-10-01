# 补齐 Emby 默认选轨索引

## 目标

共享 MediaSource 返回默认音轨和字幕索引，覆盖显式选轨及多版本隔离

## 要求

- 详情和 PlaybackInfo 的共享 MediaSource 补齐 DefaultAudioStreamIndex、DefaultSubtitleStreamIndex。
- 默认音轨优先采用默认标记，再选择首条音轨；无可用轨道返回 -1。
- 默认字幕采用默认标记，无默认字幕返回 -1。
- 显式选轨只覆盖选中媒体版本，支持 -1 关闭字幕。
- 复用现有轨道数据，不增加查询或探测。

## 验收与验证

- [x] 14 个表驱动场景覆盖默认值、标量回退、索引 0、显式选轨和多版本隔离；改动前缺字段失败，改动后通过。
- [x] service、handler 相关测试通过 race 检查，PlaybackInfo 批量查询数断言通过。
- [x] Web lint、build 和 check-nextup 浏览器验证通过。
- [x] 独立只读审查未发现明确问题，git diff --check 通过。

## 完成记录与限制

- 本记录在直接修改完成后补建，用于提交归档。
- 测试数据库容器、Web preview 已关闭，生成的 web/dist 已删除。
- 尚未部署，也未验证 Hills 实机连播；缺少默认选轨字段是已确认的兼容差异，尚不能认定为连播退出根因。
