# 验证

## 实现及边界
- 红果手动/批量资料刷新接入自动补集，Builder 装配下载服务。
- 仅已有 download placement、非 comic 作品的有效视频 ID 缺集参与；空位不影响有效后续集。
- 事务内复核作品/位置/分集；原手动和新作品 Supplement 行为不变。
- 完结和资料冷却不影响已确认缺口重试；旧状态、视频 ID 和稳定路径不重置。
- 确认整部下架后，清空无保留下载记录分集的来源视频 ID，保留分集 UUID 和媒体绑定；完成/raw/hash 分集不变。防止追更恢复已清理任务。

## 通过
- 隔离 PostgreSQL 16，独立测试 schema，无生产数据库写入及真实源媒体下载。
- 最终 `go test -race ./internal/service -run 'TestHongGuo(RefreshCatchesUp|CatchUp|DownloadUnavailableConfirmation|DownloadEpisodeReconciliation)' -count=1`：通过（35.552s）。
- `go vet ./internal/service ./internal/repository`、`git diff --check` 通过。
- 两次独立只读审查：完整补集和最终下架保留身份处理均无阻断问题。补充未保护分集失效及媒体绑定保留的直接断言。

## 较大回归现存失败
- `go test -race ./internal/service -run 'TestHongGuo(Download|Refresh|CatchUp|Import|Supplement)|TestHuangGuoAI(RefreshCatchesUp|CatchUp)' -count=1` 两次执行（162.918s/159.405s），均只有 `TestHongGuoImportTaskAndIsolation` 失败：hongguo_test.go:75 写死任务定义数量 5，现有定义返回 6（已含 hongguo_download_supplement）。
- HEAD 原测试已有此断言，HEAD task_definitions.go 已有补充下载定义，本任务未修改 task_definitions.go；该测试构造独立 catalog，未装配新增补集依赖。确认与本次补集行为无关。
- 其他下载、刷新、补集及黄果追更回归无失败输出；不把较大回归描述为全部通过。不在本任务修改此旧断言。

## 清理与未验证
- 测试容器和临时回归日志清理；既有 core 保持不变。
- 未部署/未重启用户服务、未操作生产下载队列，线上真实新集尚未验收。
- 沿用原资料刷新周期及 24 小时单作品冷却；补集只入下载队列，不改变整理/媒体扫描。
- 黄果与红果改动合并提交并各自归档；两来源均保持未部署。
