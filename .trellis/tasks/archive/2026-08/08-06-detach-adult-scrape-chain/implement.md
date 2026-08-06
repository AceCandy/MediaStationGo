# 实施计划

1. 修改常规刮削链
   - 删除 `internal/service/scraper.go` 中成人 provider 预检。
   - 从 `ScraperService.AnyEnabled` 的常规 provider 判定中移除成人 provider。
   - 验证：常规刮削仍按外部 ID 和非成人 provider 工作。

2. 限制整理链的成人调用
   - 修改 `internal/service/organizer_directory_metadata.go`，只有显式 `mediaType=adult` 才进入成人元数据查询。
   - 验证：非成人路径中的 `STRM-115` 等模式不会请求成人源；显式成人整理保持原行为。

3. 限制手动搜索的成人调用
   - 只有 provider 集合明确包含 `adult` 时才搜索成人源；`all` 不再隐式包含成人源。
   - 验证：普通手动搜索不请求成人源，显式 `provider=adult` 保持原行为。

4. 添加聚焦回归测试
   - 常规 `EnrichOne` 在路径包含成人番号形态时不调用成人 upstream，并继续完成 TMDB 匹配。
   - 非成人整理不调用成人 upstream。
   - 保留现有手动成人搜索和显式成人整理测试。

5. 验证与独立复核
   - 运行相关 service 测试。
   - 检查 `AdultProvider.Search` 的剩余生产调用方只属于显式入口。
   - 检查 diff 仅覆盖上述行为与任务文档。

## 建议验证命令

```bash
go test ./internal/service -run 'Test(EnrichOne|ManualSearch|OrganizeDirectory)'
```

如聚焦测试通过，再运行：

```bash
go test ./internal/service
```

## 回滚点

- `internal/service/scraper.go` 的成人预检分支。
- `internal/service/organizer_directory_metadata.go` 的显式类型限制。
