# 实施与验证

1. 收紧现有大样本详情状态计划循环断言，在旧实现运行并确认失败。
2. 修改共享页内状态读取，复用现有历史投影和页作品 CTE，不增加 SQL 往返。
3. 扩展纯 Series 有效完成场景；运行层级完整 payload 对照和大样本计划，保留全部原文件/历史访问边界。
4. 独立只读审查；主代理复核并执行定向 race、vet、gofmt、diff 检查。生产只允许受限只读诊断，不运行测试迁移。
5. 更新关联粒度规范和根因记录，关闭临时 PostgreSQL，保留未提交任务。

## 验证结果

- 基线：只添加新计划断言，旧实现失败，单个状态访问节点 loops=900，证明原检查上限未拦住逐文件探测。
- 第一版：源作品限定状态后，功能对照通过，但 60,000 条历史场景中 page_states 的 90 行被循环扫描 900 次；新 CTE 扫描断言失败，未放宽验收。
- 最终版：先将页内文件按逻辑分集归并并物化，再关联页内状态。600,000 文件样本中，当前页 9 源作品/900 文件的状态表累计 loops=27，空历史 visits=0，有历史 visits=180；四种模式详情 SQL 为 6.521/7.500/6.668/6.922 ms。计时是独立测试 PostgreSQL 的 EXPLAIN 执行时间，不是生产 HTTP，也未作严格同环境冷热缓存耗时对照。
- `TestHongGuoLibrary(PagePlan|PageMatchesHierarchy|PagingAndLatest|PageBoundary|CollectionType)` 全部通过，103.352 s；完整 payload 对照保留用户隔离、权限、多版本、合集、电影缺绑定及 Series/Movie 删除版本有效完成推导。
- 定向 `go test -race ./internal/service`：上述功能测试（不含大样本 PagePlan）、SeriesPresentation、PlaybackStateReplayAndDeletedVersion、HongGuoEmbyPlayableIdentityAndUserState、HongGuoEmbyResumableGroupsBeforePaging、HongGuoListsWaitForAlbumSupplement 通过，20.099 s。
- `go vet ./internal/service` 通过；最终生产 diff 经独立只读审查未发现必须修复的问题，主代理点验状态函数、可见文件范围及聚合等价性。
- 最终 `gofmt -l` 无输出、`git diff --check` 通过；临时 PostgreSQL 容器已停止并自动删除，5 份临时测试日志已清理。合成测试数据和日志可重跑测试重建，结论保留于本任务；未触碰生产数据库。实现结束时保持未提交，等待后续提交授权。
- 未部署、未重启、未进行生产 HTTP/原生播放器或全仓回归；必要的 Media/episode 读取及候选阶段耗时仍存在，不能承诺固定接口延迟。

## 根因复盘

1. 根因类别：D（测试覆盖缺口）和 E（隐含假设）。原规则只防止全用户历史扫描，未限制逐文件状态探测；不能由“用了索引/物化”推断总工作量下降。
2. 首版不足：物化状态集合本身仅执行一次，但集合扫描仍可能被嵌入文件循环，90×900 的重复工作没有消失。
3. 防复发：保留基础表 loops/visits、CTE rows×loops 双重断言；实际 SQL 带原始绑定重放 EXPLAIN；功能 oracle 继续验证有效状态，而不是 raw completed。
4. 同类边界：本轮只覆盖红果库卡片详情；NFO 逐身份探测等其它调用不扩大修改，已有计划边界继续保留。
5. 知识沉淀：已更新 work-level-queries 与 hongguo-catalog 的详情关联粒度及具体测试上限；不新增抽象、表、缓存或依赖。

## 提交归档

用户明确要求提交归档后，复核生产代码、测试和规范均与已验证版本一致；再次执行 `go vet ./internal/service`、格式和 diff 检查通过，复用此前真实 PostgreSQL、race 与独立审查结果，未重复启动测试数据库。按代码与规范提交、任务归档、日志记录的顺序收尾；不推送、不部署、不重启。
