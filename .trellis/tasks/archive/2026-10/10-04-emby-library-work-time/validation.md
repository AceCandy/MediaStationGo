# 验证结果

## 变更与边界

普通电影、剧库和混合电影库作品列表的默认、DateCreated、DateLastContentAdded排序统一使用metadata_items.latest_media_added_at；省略方向时倒序，显式Ascending保留。候选作品先排序分页，再补页内版本。跨库新增版本会影响同一作品的时间。

显式名称、评分、上映日期保持对应排序；库内Series评分补齐为作品rating排序。混合库上映日期仍保留原上映/年份/文件日期回退。未改变返回DateCreated字段、全局列表、递归未指定类型的分集列表、季/集层级、Resume、NFO和HongGuo；未改schema、缓存机制或全局数据库设置。

## 回归与复核

- 真实PostgreSQL独立schema的TestEmbyLibraryWorkTimeSort通过：movie/tv/mixed、默认及两个入库排序、升降方向、计数开关、越界页、跨库时间、代表文件与作品时间分离、显式名称/评分/上映、递归分集边界。真实绑定参数EXPLAIN无文件日期聚合。临时恢复旧排序时三种模式均按预期失败，已恢复。
- 真实PG大组回归通过：LibraryWorkTimeSort、MovieLibrary、MetadataWorkPageMatchesFileGrouping、FileDateSortQualifiesOnce、ItemsCountModes、WorkBatch、KnownMovieMembership、Series、LatestItemsOrderByImportDate。包含资格权限、版本聚合、计数、全局及层级路径。最后递归guard和库内评分调整后再次运行新排序回归通过。
- handler TestEmbyItemsOptionalTotal通过，go vet service/repository/handler通过。
- TestEmbyResumeSourcesGroupBeforeMerge、TestEmbyResumeCandidatesIgnoreUnwatchedCatalog、TestEmbyRandomGlobalWithoutExternalCatalogs、TestEmbyRandomSortUsesSeed通过。
- Web npm run lint、npm run build通过；check-nextup脚本通过三种视口、明暗主题可访问性、管理员入口及接口目录交互检查。调试preview和浏览器会话已关闭。
- 独立只读复核检查入口范围、缓存键归一位置、混合资格和分页、排序方向、接口说明，无明确阻塞；主代理再次检查最终diff。

## 真实库只读测量

同一read-only repeatable-read快照，50项、Fields=Overview，默认与DateCreated、DateLastContentAdded完整响应DeepEqual。以下为完整服务方法重复测量，非HTTP延迟。

| 库 | 不查精确总数 | 查精确总数 | 候选分页SQL | 精确计数SQL |
| --- | --- | --- | --- | --- |
| 国产剧 | 139/142ms（首次519ms） | 391/388ms | 约42ms | 322.579ms |
| 另一剧库 | 63/62ms | 108/107ms | 约13ms | 61.34ms |
| 电影 | 163/148ms | 96/95ms | 约10ms | 12.60ms |
| 外语电影 | 148/137ms | 147/158ms | 约44ms | 60.17ms |

默认候选计划无sort_values文件日期聚合。国产剧精确计数仍触发JIT，88个函数，JIT共82.175ms；其免计数路径及其余三库无JIT。冷热和页内补充查询会影响完整服务耗时，不能把单次count开关差异解释为稳定性能差异。

同快照显式PremiereDate作为旧日期路径参考，国产剧不计数4199ms、计数1663ms；这不是原二进制严格前后基准。Web国产剧仓储完整75–135ms的测量另见web-series-latest-page/validation.md，不与Emby服务方法混同。

## 未验证与剩余风险

未部署、未推送、未测运行中HTTP及真实播放器；真实混合库未测，仅合成PG回归覆盖；未跑全量测试。新排序独立fixture未另外构造权限用户/NULL时间，已有相关回归保留。显式上映日期仍可能较慢；精确总数仍需遍历合格作品，国产剧约390ms，页内版本多时补充开销仍增长。临时生产测试、脚本与调试服务在收尾清理。
