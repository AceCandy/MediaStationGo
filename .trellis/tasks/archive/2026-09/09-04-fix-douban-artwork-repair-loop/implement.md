# 实施计划

1. 扩展候选模型和仓储投影，增加 `repair_checked_url`，并提供基于现有候选快照身份的检查点 CAS。
   - 验证：repository 测试覆盖成功写入、过期快照不更新、URL 投影。
2. 调整豆瓣修复流程：已检查且文件可用时跳过；成功下载原子写检查点；精确 404 且旧图可用时保留小图并写检查点；其他错误不写。
   - 验证：service 测试覆盖第二轮不下载、404/临时错误/缺图/URL 变化矩阵。
3. 独立复核变更边界、日志脱敏、并发 CAS、迁移与规范一致性。
   - 验证：运行聚焦 Go 测试和 `git diff --check`；不默认运行全量编译或全量测试。

## Expected Files

- `internal/model/metadata.go`：候选检查点字段。
- `internal/repository/artwork_repository.go`：查询、成功 CAS、404 检查点 CAS。
- `internal/service/artwork_backfill.go`：终态判断和日志分类。
- `internal/repository/artwork_repository_test.go`：仓储回归测试。
- `internal/service/media_metadata_artwork_test.go`：任务行为回归测试。

## Rollback

代码回滚即可恢复旧行为；新增 nullable 列可保留，不影响旧版本。实施不删除现有图片、候选或任务历史。
