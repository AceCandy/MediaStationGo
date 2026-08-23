# 搜索候选与统一排序实施计划

## 1. 数字等价与排序键

- 在 repository 包内增加最小的 `0–100` 标准中文数字双向转换。
- 生成“关键词原词 + 可选数字等价词”候选组，供两个后端和共享排序器复用。
- 使用连续拉丁字母/数字和单个汉字的最小公共 token 规则，供 PostgreSQL 候选
  过滤与 Go 排序复用。
- 按完整连续数字段解析标题中最后一个有效编号，阿拉伯数字和标准中文数字统一
  为整数，禁止从超范围或非标准长串中截取子串。

验证：纯 Go 表格测试覆盖 `0、1、9、10、11、20、44、99、100`、范围外、前导
零和非标准写法。

## 2. 统一候选召回

- 非空查询固定以 `offset=0, limit=100` 调用 OpenSearch；每个关键词组内数字
  等价词 OR，关键词组间 AND，`multi_match` 显式 `operator: and` 并移除 fuzzy。
- PostgreSQL 为每个关键词组生成“等价写法 OR、写法内 token 同字段 AND”，
  关键词组间保持 AND，最多取 100 条。
- 空查询继续使用现有 PostgreSQL 排序、总数和分页，不改变普通浏览。

验证：OpenSearch HTTP mock 断言 `from/size`、AND、数字 OR 和字段范围；PostgreSQL
集成测试覆盖双向数字命中，未配置测试 DSN 时如实记录跳过。

## 3. 共享排序与内存分页

- 按候选 ID 一次读取数据库最新的标题、原名、类型、简介、年份。
- 在 repository 内实现唯一排序器：严格匹配层级；完全匹配按年份/ID；其余按
  共享相关度、编号、年份、ID。
- 排序完成后计算封顶总数并安全切取 `offset/limit`；服务层继续按 ID 顺序组装。

验证：纯 Go 测试覆盖三层不可越级、数字等价完全匹配、字段覆盖/位置/跨度相关度、
相关度优先于编号、同相关度编号倒序、年份和 ID 稳定兜底，以及负 offset、默认
limit、越界 offset 和跨页切片顺序。

## 4. 对外描述与兼容性

- 同步 `web/src/pages/embyApiCatalog.ts` 的 `/Items`、`/SearchHints` SearchTerm
  参数描述，说明只搜索顶层 Movie/Series 且非空搜索最多返回 100 条。
- 保持请求参数、响应字段、权限、NSFW、媒体库和数据库复核逻辑不变。

验证：现有 Web/Emby 搜索定向测试通过，API 目录文案与后端行为一致。

## 5. 质量门与独立复核

- 运行 repository/service 相关 Go 定向测试和 `git diff --check`；不默认执行全量
  编译或全量测试。
- 使用 Trellis 独立检查复核匹配边界、100 条封顶、空查询不回退、两个后端复用
  同一排序器，以及是否存在页内二次排序。
- 确认没有临时文件、调试服务或新增依赖。

## 主要代码落点

- `internal/repository/media_search_repository.go`：候选组、PostgreSQL 召回、共享
  排序和内存分页。
- `internal/repository/opensearch.go`：AND 查询、数字等价 OR、固定 100 条候选。
- `internal/repository/*search*_test.go`：数字、排序、分页和请求体测试。
- `web/src/pages/embyApiCatalog.ts`：SearchTerm 上限说明。

## 回滚点

- 不修改数据库结构和 OpenSearch mapping；代码回滚即可恢复原行为。
- 不自动删除或重建任何索引。
