# 设计
合并层返回 HLSDurationMismatchError 与合并产物路径，优先选择经过音轨保护的最早未修正时间轴产物。音轨恢复保护移到时长分支之前；其他错误均不返回候选。
下载层仅 DownloadResuming 接收此类型并交接路径、原清单时长和警告，普通 Download 保持严格失败。worker 复用既有 rename、清理、sync、waiting_verify 交接，并持久化 warning；校验层保留合并 warning，即使本次校验成功仍转 pending_review。既有确认、摘要和租约流程不变，无 schema/API/UI 改动。
涉及 hls_merge.go、download.go、huangguoai_download_worker.go 及对应测试/规范。
