# 代理池不可用代理检测与清理：技术设计

## Boundaries

- Web 只发起检测、展示汇总、请求确认清理，不接触完整代理 URL 或认证信息。
- `ProxyPoolService` 负责解密已保存代理、并发检测、结果分类和按 ID 原子清理。
- 管理员 API 增加检测与清理入口；现有 GET/PUT 契约保持不变。
- 不新增数据库字段、定时器、健康历史或后台任务。

## Data Flow

1. Web 调用检测接口。
2. 后端先直连同一豆瓣轻量 JSON 端点做基准检查；基准响应不是预期成功结果时终止，不返回可删除 ID。
3. 后端在锁内取得已保存代理的 ID、解密 URL 和客户端快照，随即释放锁。
4. 固定大小 worker pool 使用短超时并发检测每个代理，结果仅保留代理 ID 和分类。
5. API 返回 `total`、`available`、`unavailable`、`inconclusive` 和一次性 `cleanup_token`；服务端令牌绑定不可用 ID、代理池 generation 和五分钟有效期，不返回检测错误原文或代理地址。
6. Web 显示汇总；`unavailable > 0` 时复用 `confirmAction` 请求一次确认。
7. 确认后 Web 将 `cleanup_token` 发送给清理接口。后端只接受当前 generation 的有效令牌，在事务内删除令牌绑定且仍存在的 ID，保留并重排其余记录，再重建运行时客户端快照。
8. Web 使用清理响应刷新已保存列表和文本框。

## Classification

- `available`：代理请求得到预期 HTTP 成功响应并完整读取到有效 JSON。
- `unavailable`：请求建立/传输失败或超时、HTTP 407、当前豆瓣路由已经明确视为不可用的精确 HTTP 400、`unexpected EOF`，或成功状态下响应体无效。
- `inconclusive`：HTTP 403、429、5xx 及其他可能来自豆瓣限流、Cookie、上游或临时故障的响应；绝不进入删除 ID。
- 直连基准检查失败：整个检测失败，不产生删除候选，避免目标异常时误删整池。

## Concurrency and Cancellation

- 使用标准库 channel、goroutine 和 `http.Client`，不增加依赖。
- worker 数量和单代理超时使用包内常量，避免配置面扩张；数量必须让约 3,000 个代理有有限总时长，同时避免无限并发。
- 每个请求继承管理员请求 context；浏览器取消或服务关闭会停止尚未完成的检测。
- 网络检测期间不持有 `ProxyPoolService.mu`，不阻塞列表读取和业务代理快照。

## Compatibility and Security

- 保留 AES-GCM 加密存储、脱敏列表响应和代理顺序契约。
- 清理接口只接受检测生成的一次性令牌；检测后发生的新保存会使旧令牌失效，清理仅删除令牌绑定且仍存在的检测失败 ID。
- 错误、日志和 API 均不得包含完整代理 URL、Userinfo、密文、Cookie 或目标查询参数。
- 检测不修改 sticky route；清理完成后的 generation 变化让后续豆瓣请求从直连重新选择。

## Rollback

- 回滚新增 API、服务方法和 Web 按钮即可；未增加 schema，旧数据无需迁移。
- 已由管理员确认删除的代理不可恢复，确认文案必须明确这一点。
