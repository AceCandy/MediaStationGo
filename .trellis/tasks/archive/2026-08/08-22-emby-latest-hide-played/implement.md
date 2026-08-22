# 实施计划

1. 扩展 `Latest` handler，解析可选 `IsPlayed` 并传入服务层。
2. 在 `LatestItems` 与剧集最近入库查询中复用同一作品级播放状态条件，确保过滤发生在排序和 Limit 前。
3. 让播放过滤请求绕过现有 `Latest` 结果缓存，避免播放状态变化后的短期陈旧结果。
4. 增加定向测试，覆盖默认、显式 true/false、用户隔离、Limit 前过滤和剧集归组。
5. 同步 `embyApiCatalog.ts` 中 `items-latest` 的参数与行为说明。
6. 运行受影响 Go 测试、前端最小静态检查和 `git diff --check`，然后进行独立复核。

## 预期修改文件

- `internal/handler/emby_items_handlers.go`
- `internal/service/emby_items_detail.go`
- 相关 `internal/service` / `internal/handler` 定向测试
- `web/src/pages/embyApiCatalog.ts`

## 验证限制

- 不执行全量后端编译或测试；仅运行受影响包的定向测试。
- 不启动服务；使用现有测试设施验证。
