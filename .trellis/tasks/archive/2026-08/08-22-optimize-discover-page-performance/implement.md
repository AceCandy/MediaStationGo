# 实施清单

## 1. 稳定海报 URL 并恢复按需加载

- [ ] 删除 `DiscoverPage` 的全页 `imageVersion` / `refreshImageVersion` state 与 props 传递。
- [ ] `DiscoverCard` 仅在单张坏图重试时添加版本号和 `refresh=1`。
- [ ] 将发现页海报改回 `loading="lazy"`，保留 `decoding="async"`、poster/backdrop fallback 与最多三次退避重试。
- [ ] 验证：真实浏览器再次进入页面时相同海报 URL 稳定，初始图片请求数和传输量低于 126 次 / 5.68MB；滚动后海报正常补载。
- [ ] 定向验证 poster 失败后切换 backdrop，以及 backdrop 失败后 `r1` 至 `r3` 仍携带 `refresh=1`。

回滚点：仅恢复两个组件的图片版本 props 与 eager 属性。

## 2. 单栏翻页只加载变化栏目

- [ ] 提取一个组件内 `loadRows(targets, refresh)`，由初次/栏目变化、手动刷新和单栏翻页复用。
- [ ] `discoverAPI.feed` 接收 AbortSignal；每个新批次 abort 上一个 controller，被取消请求不写数据或错误。
- [ ] 主 effect 不再依赖 `rowPages`；翻页事件直接加载目标栏目，手动刷新直接加载全部当前栏目。
- [ ] 页码 state 与只读 `rowPagesRef` 同步更新，入口从 ref 构造 targets；不得用 ref 快照跳过 StrictMode effect。
- [ ] loading/error 更新只作用于活动批次，旧批次 finally 不得清除新批次状态。
- [ ] 验证：StrictMode 首次进入仍加载数据；单栏翻页只出现一个目标请求；快速翻页→刷新会取消旧请求且页面落在最新结果。

回滚点：恢复 effect 对全部 selected 栏目的统一请求。

## 3. 复用 section 缓存并支持显式刷新

- [ ] `discoverAPI.feed` 增加可选 refresh 参数，仅手动刷新传 `refresh=1`。
- [ ] handler 将 refresh 意图传入 section job；普通 job 在 Provider 调用前读取现有有效缓存。
- [ ] 缓存命中保持输入顺序、`has_next` 和响应结构；显式刷新绕过快路。
- [ ] Provider 失败后仍按 cache → fallback → error 顺序处理。
- [ ] 增加后端定向测试，验证缓存命中不调用 loader、混合命中/未命中仍遵守调度、显式刷新调用 loader。
- [ ] 显式刷新失败且缓存存在时，断言缓存 items、`stale=true`、正确 `has_next`，并证明 fallback 未执行。
- [ ] 明确保留空结果不缓存行为，不为其增加新缓存规则。
- [ ] 验证：`go test ./internal/handler -run 'Test.*Discover.*(Cache|Refresh|Provider|Fallback)' -count=1`

回滚点：删除 refresh query 与请求前缓存快路，保留原失败回退。

## 4. 真实浏览器前后对比

- [ ] 使用隔离登录态复测冷/热进入、手动刷新、单栏翻页和向下滚动。
- [ ] 固定登录态、缓存状态和视口，对关键场景至少采样三次并比较中位数。
- [ ] 通过 Web Vitals 与 Resource Timing 聚合记录 feed 耗时、图片请求数量/传输量和 LCP；不得输出请求 headers、token、完整敏感 URL 或 HAR。
- [ ] 确认页面无控制台错误，缓存/Provider 失败提示和逐栏分页可用。
- [ ] 结束时安全退出并关闭浏览器会话，不保留 HAR、截图、trace 或认证状态文件到工作树。

## 5. 独立复核与质量门禁

- [ ] 对照 PRD 检查图片、翻页、刷新、缓存、fallback 和兼容性。
- [ ] 运行后端定向测试，不执行无必要的全量测试。
- [ ] 运行 `cd web && npm run lint && npm run build`。
- [ ] 运行 `git diff --check`，确认无无关改动、隐私文件或临时调试产物。
- [ ] 使用 `trellis-check` 做独立复核，修正后再报告完成。

## Pre-start Gate

- [x] PRD、设计和实施清单已向用户展示。
- [x] 用户在看到最终规划摘要后明确批准开始实现。
