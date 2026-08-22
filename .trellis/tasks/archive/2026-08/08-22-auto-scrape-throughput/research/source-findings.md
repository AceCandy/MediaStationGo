# 源码核验摘要

- `catalog_hydration.go:21-44,99-163`：当前只启动一个 worker，循环优先处理一个媒体组，无媒体后再处理 catalog job。
- `scrape_worker.go:19-66`：自动媒体完整流程持有全局 `scrapeRunMu`，简单复制 worker 仍会串行。
- `scrape_worker.go:100-139`：claim 事务通过 `running` 状态排除同 series、metadata 或 media，并用 RowsAffected 检测冲突。
- `scraper.go:24-28`、`scraper_library.go:133-137`、`catalog_hydration.go:248-262`、`people_backfill.go:109-111`：手动、整库、发现目录和人员补全也使用同一全局锁，不能直接删除。
- `tmdb_match.go:13-202`：已有 TMDB ID 的 movie/TV 完整详情已经解析 languages、countries、genres。
- `scraper.go:248-260`、`tmdb_details.go:15-117`：保存匹配后仍固定再次请求同一详情 endpoint，仅覆盖上述三类字段。
- `Match` 当前没有表达“扩展字段已由完整详情加载”的标记；搜索结果与详情结果需要显式区分。
- `emby-toolkit` 使用小规模线程池、连接复用和重试；本项目已经具备 HTTP/2 与连接复用，本次只借鉴受限并发。
- Jellyfin v10.11.11 对不同媒体采用受限 fan-out、单媒体内部 provider 串行；该边界与本任务一致。

