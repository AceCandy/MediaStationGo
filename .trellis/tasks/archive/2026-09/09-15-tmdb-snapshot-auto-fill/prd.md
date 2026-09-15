# TMDB 标识自动补齐快照与回填重试

## 目标与边界

- NFO 和非 TMDB 刮削接受合法 TMDB 标识后，立即尝试保存对应电影/整剧的 TMDB 原始详情快照。
- 只补缺失快照，保留原来源的展示字段、图片和已有快照；失败不回滚已接受的资料。
- 请求期间改绑或其他路径已保存快照时，不写入过期结果、不覆盖新快照。
- 自动回填有单项失败时不得标记完成；已有旧完成标记但仍有实际缺口时，启动能够重试。
- 失败缺口在下次导入/刮削或服务启动时重试；不新增定时全库扫描。
- 手动修改 TMDB ID 继续使用现有的详情校验和标识/快照原子保存流程。

## 验收

- PostgreSQL 与模拟 TMDB HTTP 验证 NFO/豆瓣来源写入即补齐、重复执行不重复请求、原来源资料保留。
- 验证无 TMDB ID、未配置、网络失败、无效响应和并发身份变化。
- 验证回填失败不完成、下次自动执行可重试、旧完成标记不阻挡缺口、无缺口自动跳过。

## 实施与验证范围

- 修改快照服务、元数据持久化的两个公共入口、快照 repository 条件插入及相关回归测试。
- 同步任务定义的触发说明为“启动时补缺 / 手动”。
- 同步快照契约，独立复核 diff，并执行 Go 定向和包级测试。
- 保留工作树已有 Emby 修改，不提交代码。

## 验证结果

- 独立只读复核未发现本次修改的中高风险缺陷；主线程复核了最终代码、任务文案和契约。
- 临时 PostgreSQL 15 使用独立 UTF-8 数据库；模拟 HTTP，不使用生产数据库或真实 TMDB。
- 通过：`go test -race ./internal/service ./internal/repository -run 'Test(NonTMDbIngestion|TMDbSnapshot|AutomaticTMDbSnapshot|.*MissingTMDbSnapshot|.*ReplaceIdentifierWithSnapshot|.*ManualTMDbSnapshot)' -count=1 -timeout=120s`，配置 `MEDIASTATION_TEST_POSTGRES_DSN`，非跳过。
- 通过：`go test ./internal/service -run '^TestTaskDefinition' -count=1`、`go vet ./internal/service ./internal/repository`、`git diff --check`。
- 两个包的全量测试未通过。用 Go overlay 还原本次修改前的文件、不动工作树，对照复现了豆瓣 JSONB 文本格式断言、NFO/手动元数据测试缺少 `media_probe_metadata` 表，以及 `TestKnownTMDbIDReusesLoadedDetails` 在 `scraper.go:53` 的空指针中断。其他全包失败不在本次范围，未逐项定位或修复。
- 临时数据库最初默认为 SQL_ASCII；已改为 UTF-8 后重新执行上述测试。后续创建临时 PostgreSQL 应显式指定 UTF-8。
- 限制：未连接实际 TMDB、未在运行中的应用部署验证。网络故障下缺口在下一次导入/刮削或服务启动时重试，不做常驻定时重试。
- 临时 PostgreSQL 已关闭，临时数据库、测试日志和基线 overlay 文件已清理；代码未提交。
