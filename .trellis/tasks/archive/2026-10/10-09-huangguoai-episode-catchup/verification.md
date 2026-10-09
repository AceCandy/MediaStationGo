# 验证记录

## 已完成
- 隔离 PostgreSQL 16 容器，所有测试使用独立 schema；无生产数据库写入，无真实媒体下载。
- 黄果服务回归：`go test -race ./internal/service -run 'TestHuangGuoAI|TestHuangGuoAISupplement' -count=1` 通过（71.371s）。
- 补充中途取消和部分资料失败场景后的定向 race：`go test -race ./internal/service -run 'TestHuangGuoAI(RefreshCatchesUp|CatchUp)' -count=1` 通过（12.582s）。
- `go vet ./internal/service`、`git diff --check` 通过。
- 独立只读审查未发现阻断问题。按建议补测中途取消、资料失败不阻止已确认缺集；修改下载 root 后稳定位置已有测试。
- 下载 placement 是持久化的既往入队证据，沿用现有 Supplement 的历史位置约束；无需额外要求旧下载行仍存在。
- 既有未跟踪 core 非本任务产物，保持不变。

## 本任务核查时的红果只读结论

以下为黄果任务阶段的现状；随后红果任务 `10-09-hongguo-episode-catchup` 已补上同类追更，本轮与黄果一起提交归档。
- `internal/repository/hongguo_repository.go` WorksAfter 仅定期选取未完结且超过 24 小时未刷新的作品。
- `internal/service/hongguo_download_supplement.go` Supplement 明确排除已存在位置或分集下载历史的作品；资料刷新不调用下载入队。
- 普通 HongGuo Enqueue 可保留旧行并补入新分集；目前需要手动刷新后入队。

## 未验证与限制
- 未部署、未重启用户服务、未改生产队列；上线真实新增集仍待验收。
- 24 小时单作品资料冷却保持原样；只有刷新任务实际运行才能发现源站新增集。
- 自动补集仅入下载队列，不改变下载校验、整理或媒体库扫描设置。
