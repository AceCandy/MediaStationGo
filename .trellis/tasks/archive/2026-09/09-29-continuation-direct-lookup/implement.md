# 实施与验证

1. 在临时 PostgreSQL 中补长剧当前用户历史及指定来源／剧集回归，记录原实现失败计划。
2. 修改共享续播查询，保留原有效状态与下一集算法；用访问量和 JIT 验证直接定位效果。
3. 运行仓储、服务和 handler 续播／历史／版本／可见性针对性测试、race、vet 和格式检查；必要时生产库仅只读对照实际查询，不调用状态写接口。
4. 独立只读复核，修正范围内问题，更新播放规范和本文件的实测记录。
5. 关闭并清理临时数据库与诊断产物；用户确认后提交并归档，不部署、不重启业务服务。

用户在此前方案说明后已明确批准实施；无待确认的业务决策。实现由主代理负责，子代理仅探索与核验。

## 已有实测证据

- 临时 PostgreSQL 17，200 部红果长剧、每剧 240 集；当前用户 2,152 状态，另一个用户 48,000 状态。原实现 Resume 分集访问 518,401 次、3,595.845ms，其中 JIT 2,775.711ms，访问量断言失败。
- 仅将历史拆成直接分集／电影分支，分集访问降至约 4,311 次，但估算仍因有效状态替代查询展开而触发优化 JIT，约 2.9s，故没有把它视为完成。
- 最终查询自定义计划 Resume/NextUp/Web 约 202–243ms，分集访问 4,312 次；指定合集／独立作品约 10–12ms、479 次；无历史合集约 0.5ms、文件和分集访问为零。单剧 custom/generic 均无 JIT。
- 全局强制 generic 计划在两用户极不均匀分布的测试中仍估算过高，约 2.2–2.6s，主要为 JIT；访问量仍受限。测试记录该剩余边界，不声称全局无 JIT，也不把合成耗时代替真实 HTTP 验收。
- 生产两个历史 SQL 样本只读比较（数据库连接强制只读，未修改业务数据）：指定无历史合集 NextUp 从 15,614.605ms 降至 0.683ms；全局 Resume 从 15,357.860ms 降至 664.618ms，分集访问从 417,272 次降至 3,822 次。两者旧／新查询双向 EXCEPT ALL 无差异。
- 已通过定向仓储长剧／混合分页、三来源跨季／删除版本／重播、Resume 来源归组／未看目录、NextUp/Web 路由测试。
- 独立审查指出计划断言需包含 join-filter/index-recheck 工作，已补充并通过；指出数据库外键不约束 binding.work_id 与 episode.work_id 一致，正常导入／重绑写入已按同 work 定位。生产只读核对跨作品错绑／悬空分集绑定为零；不为无生产证据且正常写入不产生的错绑增加兼容性扫描或数据修复。
- 最终代码再次只读重放：无历史合集 NextUp 0.671ms，全局 Resume 682.439ms；旧／新双向 EXCEPT ALL 仍无差异。临时生产诊断测试文件已删除，未保留私有 SQL／身份或凭据导出。

## 缺陷复盘

- 根因属于测试覆盖缺口和隐含性能假设：历史驱动 LATERAL 不代表按单集点查，旧测试每来源仅少量本用户历史、每剧两集，未覆盖批量已看长剧。
- 单独增加精确集号只能改善执行扫描，不能消除重复有效状态子计划的估算和编译开销；复核必须同时看实际扫描、估算成本及 JIT。
- 预防已落到播放规范和真实参数化 PostgreSQL 回归；不新增字段、索引、缓存或全局数据库设置。

## 最终验证与交付边界

- `go test ./internal/repository ./internal/service ./internal/handler -count=1` 全部通过：85.760s / 542.580s / 29.030s。
- `go test -race ./internal/repository ./internal/service ./internal/handler -run 'Test(Continuation|PlaybackStateReplayAndDeletedVersion|NextUpRoutesAndWebContinuation|EmbyResumeSourcesGroupBeforeMerge|EmbyResumeCandidatesIgnoreUnwatchedCatalog)' -count=1` 全部通过：19.626s / 27.009s / 1.897s。
- `go vet ./internal/repository ./internal/service ./internal/handler`、gofmt、`git diff --check` 通过。两个独立只读审查完成，主代理复核了指出的具体位置；计划断言补强后重新运行通过。
- 测试始终使用独立 PostgreSQL 容器的随机 schema；生产对照连接强制只读。临时诊断文件已删除，临时 PostgreSQL 已关闭并随 `--rm` 删除，未启动或重启业务服务。
- 未验证部署后真实 HTTP 时延／499 消失情况、前端浏览器 E2E，以及所有可能的生产数据分布；全局 generic 计划仍可能有 JIT 编译开销，不能承诺所有请求均达到单剧样本耗时。
- 实现交付后用户明确授权“提交归档”。按代码提交、任务归档、会话记录的顺序收尾；本任务直接在 main 完成，没有关联 PR，按非 PR 任务归档。仍不推送、不部署、不重启业务服务。
