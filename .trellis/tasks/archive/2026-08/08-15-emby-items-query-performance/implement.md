# Implementation Plan

1. 在现有 Emby service 测试基础设施中加入局部查询计数，并新增多条 `/Items` 查询量回归。
   - 验证：修复前查询量随条目数增加；测试断言批量化后的上限/不增长关系。
2. 为 credits+people、metadata identifiers、MediaView siblings 增加最小批量 repository 查询，并让单 ID 方法复用相同查询逻辑。
   - 验证：空输入、结果分组所需字段、排序和可见性过滤保持正确。
3. 在 `payloadsForViews` 循环前批量预取关系数据；让列表 payload 使用内存映射，同时保留详情即时加载路径。
   - 验证：People 顺序、ProviderIds 映射、MediaSources 多版本/可见性/标量流和 Episode artwork/type 行为不变。
4. 运行格式化与针对性验证，并进行独立 diff/契约复核。
   - 命令：`gofmt` 仅格式化本任务修改的 Go 文件。
   - 命令：`go test ./internal/service -run 'TestEmbyItems|TestEmbyLocalAndHTTPMovieVersions|TestEmbySeriesHierarchyCountsEpisodeMetadataOnceAcrossVersions|TestEmbySeasonAndEpisodeDoNotInheritSeriesArtworkOrPeople'`。
   - 命令：`go test ./internal/handler -run 'TestEmbyPlaybackInfo|TestEmby.*Items'`（以实际测试名收窄）。
   - 命令：`git diff --check`。
5. 在 Emby 双前缀路由设置播放器 API 标记，并让现有请求日志追加脱敏后的 headers/query。
   - 验证：播放器请求保留 `Fields`、`IncludeItemTypes`、`User-Agent` 等排障信息；Token、Authorization、Cookie 和设备标识不出现明文；普通 API 不记录请求详情。
6. 批量加载 Series/Season 的 metadata、artwork、用户状态、People 和 ProviderIds，并解析 `Fields` 跳过未请求的可选关系。
   - 验证：Movie/Episode/Series/Season 查询次数均不随页大小增长；显式轻量 `Fields` 的查询更少且响应不含高成本字段；省略时兼容响应不变。

## Review Gates

- `Fields` 省略时不修改响应字段集合；仅显式请求时裁剪三个高成本可选关系。
- 不让列表进入 `completeStreams=true` 或新增 ffprobe 调用。
- 不覆盖工作树中与本任务无关的既有修改。
- 若批量化必须改变播放器可见行为，停止实施并重新请求确认。
