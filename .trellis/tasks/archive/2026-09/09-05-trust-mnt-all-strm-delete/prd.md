# 信任 /mnt/all STRM 删除根目录

## Goal

允许管理员预览并删除位于 `/mnt/all` 下的 STRM 本地目标文件，同时保留现有路径边界与文件类型校验。

## Background

- 当前删除预览仅接受位于 FileManager 可信根目录内的目标。
- `/mnt/all` 下的目标文件即使存在，只要该目录未进入可信根，预览接口仍会拒绝，前端因此不显示删除按钮。

## Requirements

- 将固定目录 `/mnt/all` 纳入 FileManager 可信根；目录不存在或不可访问时沿用现有逻辑自动忽略。
- 只信任 `/mnt/all`，不扩大到整个 `/mnt`。
- 继续拒绝 `/mnt/all` 之外的目标、符号链接、非普通文件及文件系统根目录。
- 复用现有 `allowedRoots`、`secureSTRMDeleteTarget` 和管理员权限校验，不新增配置项或前端分支。

## Acceptance Criteria

- [x] 当 `/mnt/all` 存在时，它出现在 FileManager 可信根中。
- [x] `/mnt/all` 内现存普通文件可通过 STRM 删除目标解析。
- [x] 相邻目录（例如 `/mnt/all-other`）不会因前缀相似而被信任。
- [x] `/mnt/all` 不存在时服务行为不受影响。
- [x] 相关最小 Go 测试通过，且没有修改前端或删除 API 契约。

## Out of Scope

- 信任整个 `/mnt`。
- 新增用户可配置的额外可信根。
- 修改前端吞错行为或删除按钮样式。
- 修改 STRM sidecar、媒体数据库记录或父目录删除规则。
