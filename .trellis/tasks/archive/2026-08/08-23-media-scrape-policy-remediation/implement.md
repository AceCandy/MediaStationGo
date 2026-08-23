# 媒体入库刮削策略与失败处置：实施计划

## 1. 类型与策略

- [x] 定义不超过字段长度的 `nfo_movie` / `nfo_tv`；后端建库显式保留选择值，Web 创建选项与标签同步；现有库编辑继续维持只改封面的既有边界。
- [x] 收敛并替换所有真实 `Library.Type` 系列/NFO/可刮削判断，覆盖 scraper、manual provider、organizer、Emby 与 Web 两处系列判断；不改无关通用 media type 分类器。
- [x] 为非常规类型增加严格 NFO-only 分支：成功复用本地持久化；缺失为 `no_match`；解析失败为 `error`；零 provider 请求。
- [x] 禁止非常规 scanner 叠加 path hint，并在后端拒绝其 provider manual search/apply；覆盖路径带 provider ID 但无 NFO 的回归。
- [x] 增加电影和剧集 NFO 边界测试，至少覆盖缺失、损坏、movie NFO、show+episode NFO、canonical 复用与层级。

## 2. 自动与手动触发

- [x] 移除 Web `scrape.auto_on_scan` 控件/字段/提交，scanner 与 watcher 在变化后默认唤醒现有 worker。
- [x] 保留 `organize.scrape_after`，去掉对退休键的兼容回退；在迁移中精确删除 `scrape.auto_on_scan` 并验证幂等与无关设置保留。
- [x] 为 `media_scrape` 增加单库/全部媒体库手动 action，扩展并复用 `ResetLibraryScrape(..., false)`，校验目标互斥、库存在和 server-owned 类型白名单。
- [x] 验证空状态/`pending/error/no_match` 均带 manual trigger，失败状态清错重新入队，`matched/running` 不变；全部模式跳过 music/adult/未知类型并逐库汇总。

## 3. 统一日志

- [x] 在一次 scrape 执行内传递临时来源：`existing_metadata`、`local_nfo` 或 `Match.Source`，不修改数据库模型。
- [x] 更新 `mediaScrapeTaskDetail`，用同一日志格式显示“已有元数据 / 网络刮削（provider）/ 本地 NFO”。
- [x] scanner 在精确 canonical 首次绑定前创建 `TaskKindScrape`，按绑定结果结束记录；已 matched 重扫不重复记录。
- [x] 覆盖三种成功来源、no-match、清洗后的 error、同日稳定定义日志聚合和扫描重复执行测试。

## 4. Web 失败处置

- [x] 增加 raw-media scrape issue DTO、分页查询和管理员 endpoint，只允许 `error/no_match` 与可选 library 过滤，并为无错误文本状态生成安全可操作原因。
- [x] 在任务中心“媒体入库刮削”区域展示状态、媒体、库和安全错误，支持筛选及刷新。
- [x] 复用现有单媒体重新处理 API；普通 `no_match` 复用 `ManualScrapeDialog`；非常规类型只提示修复 NFO 后重试。
- [x] 验证未匹配媒体无需进入 `MediaView` 也能展示，普通媒体列表行为保持不变。

## 5. 复核与验证

- [x] 用 `rg` 复核所有库类型硬编码、`scrape.auto_on_scan` 读写、媒体刮削状态分支和 task definition action。
- [ ] 运行针对性 Go 测试：repository/scanner/scraper/task handler/media remediation 相关包或测试名；不默认执行全量编译。（已运行且包编译通过；PostgreSQL 用例因未配置测试 DSN 跳过。）
- [x] 运行 Web 针对性测试（若已有）、`npm run lint`、`npm run build` 和 `git diff --check`。
- [ ] 独立复核跨层契约：请求校验、DTO/TS 类型、状态动作、来源文案、深浅主题、键盘操作和窄屏无溢出。（已完成静态独立复核；未做浏览器深浅主题、键盘与窄屏实测。）
- [x] 确认未修改媒体/NFO/图片边车文件，未新建隐私或调试产物，未启动遗留服务。

## 回滚点

- 类型与 NFO 策略、自动触发、日志来源、remediation API/UI 分块提交或保持可独立回退。
- 不回滚或删除业务媒体数据；若撤销功能，只移除新类型/action/endpoint/UI，并恢复旧设置读取代码。
