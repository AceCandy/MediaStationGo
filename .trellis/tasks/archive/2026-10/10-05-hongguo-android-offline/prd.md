# 红果 Android 离线模型兜底接入

## Goal

可选常驻Android容器提供官方离线模型，现有来源耗尽后由Go下载并校验目标分集

## Requirements

- 可选常驻 Android 容器保留官方 App 初始化数据，由项目自动获取目标集离线模型。
- 支持独立 Compose 只启动 Android，宿主机 `dev.sh` 后端通过配置 IP/ADB 端口连接。
- 普通三来源各尝试一次，配置 Android 后增加最后一次兜底；总预算为三次或四次。
- 沿用 Go 下载、校验、发布、取消和恢复机制；模型与签名地址不入库、不记录日志。
- 不新增登录、网页配置或来源优先级选项，不承诺上游未提供的1080P。

## Acceptance Criteria

- [x] 普通来源各一次共三次；启用后第四次才调用 Android。
- [x] 自动返回指定数字分集ID的兼容模型，连续不同目标不串集。
- [x] 超时、取消、Android不可用不会无限等待或遗留取模型进程。
- [x] 真实第81集下载并完成音视频解码；重启后可以再次获取。
- [x] 可选部署文件、初始化与回滚说明完成，默认镜像不增加工具负担。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
