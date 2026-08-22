# 设计：发现页缓存复用与按需加载

## Boundary

本任务只调整发现页 Web 状态、发现 API 参数和 feed handler 的现有 section 缓存读取。Provider 实现、并发限制、图片代理缓存、数据库结构与后台 hydration 流程保持不变。

预期产品代码改动集中在：

- `web/src/pages/DiscoverPage.tsx`：去除全页图片版本刷新，以一个组件内加载函数统一整页、刷新与单栏翻页，并实际取消旧请求。
- `web/src/pages/DiscoverContentRow.tsx`：恢复原生 lazy 图片加载，仅为失败重试生成版本号并刷新代理缓存。
- `web/src/api/discover.ts`：为 feed 增加可选 `refresh=1` 与 AbortSignal。
- `internal/handler/discover_extra.go`：普通请求命中有效 section 缓存时直接返回，手动刷新跳过该快路。
- `internal/handler/discover_extra_test.go`：覆盖缓存命中、显式刷新和失败回退。

## Data Flow

### 普通进入

```text
读取 localStorage 首屏行
  → 按页码合并需要加载的栏目
  → GET /discover/feed?sections=...&page=...
  → 每个 section 先查现有 6 小时内存缓存
      ├─ 命中：直接返回缓存 items
      └─ 未命中：沿用 Provider 调度、超时、remember/fallback
  → 保持 artwork warm 与 catalog hydration 入队
  → 逐栏更新 UI 和 localStorage
```

缓存快路只复用 `DiscoverService.CachedSection`，不新增缓存实例、TTL 或失效规则。缓存命中结果继续计算 `has_next`，响应仍为 `items[sectionKey] + _meta[sectionKey]`。

### 手动刷新

```text
点击刷新
  → reloadSeq 触发当前栏目整批请求
  → GET /discover/feed?...&refresh=1
  → 跳过正常缓存快路，调用 Provider
      ├─ 成功：覆盖现有 section 缓存
      └─ 失败：仍按 cache → fallback → error 回退
  → 海报使用稳定 URL，不执行全页 refresh=1
```

`refresh` 只控制“请求前是否直接使用 section 缓存”，不改变失败路径读取缓存的行为。

### 栏目选择与翻页

`DiscoverPage` 提供一个组件内 `loadRows(targets, refresh)` 加载函数，供三个入口复用：

- 初次加载或栏目选择变化：传入全部当前栏目，并继续按页码合并。
- 单栏翻页：事件处理器直接传入目标栏目和新页码；`rowPages` 不再触发整批 effect。
- 手动刷新：直接传入全部当前栏目，并设置 `refresh=true`。

每次加载创建新的 `AbortController`，先 abort 前一次加载，再把 signal 传给 axios。React StrictMode 的首次 effect cleanup 只 abort 自己捕获的 controller，第二次 setup 会重新发起请求，不依赖跨 setup 的“前次快照”判断。

`rowPagesRef` 只保存当前页码供上述入口构造 targets；每次写 `rowPages` 时同步更新该 ref。它不参与“是否请求”的判定，因此 StrictMode cleanup/setup 不会吞掉首次加载。

页面同时只保留一个活动 feed 批次。新批次开始时重建本轮 row loading；被取消批次既不写入数据/错误，也不清除新批次 loading。请求完成后只更新本轮目标栏目，未参与请求的 rows/errors 保持不变。

### 图片加载

- 普通海报 URL 只由远端 URL 和现有 `retry=1` 构成，不包含页面挂载时间。
- `<img loading="lazy" decoding="async">` 交给浏览器按视口调度。
- 仅单张图片失败后使用 `r1` 至 `r3` 版本并设置 `refresh=1`，保留现有 poster → backdrop 与退避重试逻辑；运行时分别验证 poster fallback 和最终重试 URL。
- 删除 `imageVersion` / `refreshImageVersion` 跨组件 props 和对应页面 state。

## API Contract

`GET /api/discover/feed` 新增可选 query：

- 缺省或 `refresh!=1`：允许有效 section 缓存快路。
- `refresh=1`：跳过正常缓存快路，强制查询 Provider。

现有 `sections`、`page`、响应字段、HTTP 状态和错误文案均不变；旧客户端不传 `refresh` 时保持兼容。

Web `discoverAPI.feed` 同时接收可选 AbortSignal，只传给 axios `signal`，不改变后端协议。取消错误由页面识别 signal 状态后静默处理。

## Compatibility and Trade-offs

- 同 Provider 串行与最多两个 Provider worker 不变，避免放大上游限流风险。
- 缓存命中检查仍在 provider job 内完成；同一请求部分命中、部分未命中时，未命中 job 继续经过原 Provider 分组与 worker 上限。
- 冷缓存仍等待外部 Provider；命中缓存与 localStorage 的重复访问优先变快。
- 现有 cache 不保存空数组，空结果继续查询 Provider。
- 手动刷新不再解决“同 URL 海报原地换图”的即时可见问题，但会刷新推荐列表，坏图仍主动绕过缓存重试。
- 不优化约百毫秒级 hydration 入队和前端 state/localStorage 写入；真实测量表明它们不是当前主因。

## Rollback

- Web 图片策略、请求目标计算和 API refresh 参数可分别回滚。
- 后端缓存快路只读现有内存数据，可整体回滚而不产生数据迁移或持久化副作用。
