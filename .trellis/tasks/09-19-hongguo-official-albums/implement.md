# 执行与验证

1. 新增官方字段及解析，复用 App 请求机制；测试精度、身份、缺失/非法关系、取消。
2. 替换关系查询与 Web/Emby 消费，移除人工操作及旧表；验证同合集、独立剧、跨库权限、播放身份。
3. 接入补充任务与历史回填；验证多页、成功空关系、失败冷却、中断恢复和网页资料不变。
4. 更新已有测试及规范，执行相关 Go 测试（数据库测试须真实 PostgreSQL）、Web lint/build、差异检查和独立复核。

不在本轮操作实际业务数据库或批量调用全库上游接口；提供代码和可执行任务入口。旧人工能力按用户授权删除，无兼容迁移。

## 实施结果

已移除人工模型、写路由、编辑/批量聚合入口；官方字段直接存作品表，只读关系投影复用现有展示结构。迁移删除旧组/成员表。网页详情继续保留；签名 App 详情只补充官方关系。任务中心新增“红果官方合集补充”，支持历史分页、逐条检查点、失败冷却、取消续跑；资料刷新后同步补充。

## 验证记录

- 隔离 PostgreSQL：官方关系解析/签名请求、103 条跨页查询、同名不同合集、网页更新隔离、成功空关系、失败续跑、取消互斥、旧表重复迁移、Web/Emby 身份、HTTP 只读与退休写路由测试通过。
- Web lint/build、浏览器 `check-hongguo-batch.mjs` 与 `git diff --check` 通过。浏览器覆盖只读关联、下载失败重试、权限和双主题布局。
- 较宽红果测试发现四个原有失败：`TestHongGuoArtworkOwnershipMigration`、`TestHongGuoDiscoveryScansUntilCategoryEnd`、`TestHongGuoDiscoveryDefersDetailsAndResumes`、`TestHongGuoDiscoveryIncrementalStopsAtSavedBoundary`。均在隔离 HEAD 副本复现：海报测试在删列后仍使用带该列的新模型插入；发现测试未隔离既有自动详情唤醒。未改动这些无关逻辑。
- 独立只读审查已完成。关于按合集合并统计排行的建议不采纳：既有规范和设计明确按源作品统计，仅展示名/季号使用官方关系。另补齐列表和搜索 DTO 的官方字段。
- 排除上述四项已证实的基线失败后，五个相关包的 `TestHongGuo|TestParseAlbum|TestAlbum|TestDownloadApp` 回归全部通过；`go test -race ./internal/service -run 'TestHongGuoAlbumFailureAndResume|TestHongGuoWakeup'` 通过。新增官方 GET 的 HTTP 用例单独复跑通过。
- 未对实际业务库迁移或执行全量联网回填；未验证真实播放器与全库上游限流。部署启动迁移会不可逆删除旧人工关系；用户已明确无需保留。官方关系只聚合已入库成员，不自动发现未收录季。

代码保持未提交；按收尾流程暂不归档含未提交改动的任务，不触碰其他并行任务。
