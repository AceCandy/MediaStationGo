# 豆瓣大图与图片域名配置：技术设计

## Change Boundary

- 当前差距：豆瓣详情解析会选中 `pic.normal` 小图，图片下载不使用 Douban `base_url`，既有豆瓣小图候选没有安全升级入口。
- 行为归属：豆瓣详情解析和动态图片 URL 解析属于 `DoubanProvider`；图片下载与内容校验属于现有 `ArtworkStore`；候选及当前选择切换属于 `ArtworkRepository`；管理员手动入口属于现有任务定义与 scheduler runner。
- 预计只修改豆瓣配置/解析、图片 repository/store、回填任务和对应聚焦测试。前端已有通用 `base_url` 输入与保存流程，不新增页面代码。
- 明确不做：不改豆瓣 JSON API 域名，不改 Cookie，请求失败不删除旧图，不清理旧资产，不给其他 provider 增加 CDN 抽象，不自动运行历史升级。

## Data Flow

### 新豆瓣图片

1. `doubanMatchFromRawJSON` 从快照按 `cover.image.large.url → pic.large → pic.normal → 兼容字段` 选择原始图片 URL。
2. enrichment 下载前调用 Douban 专用 URL 解析：每次读取 `APIConfigService.Resolve("douban")`，配置为空时返回原 URL；配置存在时仅用配置的 scheme/host 替换原 URL 的 scheme/host，保留原 path/query。
3. 现有 `ImageProxy.Fetch → ArtworkStore.prepareAsset → SaveCandidate` 负责真实图片校验、32 MiB 限制、尺寸读取、SHA-256 去重和本地落盘。
4. `SaveCandidate` 继续只在没有当前海报时提升候选，不覆盖现有选择。

### 历史图片修复

1. repository 以候选 ID 做 keyset 分页，只扫描 `source_provider='douban'` 的海报候选，并联接资产、同作品当前选择及 Douban provider snapshot。
2. 服务仅处理：文件不存在、`source_url` 含 `s_ratio_poster`，或资产宽度不超过 300 的候选；其他候选计为跳过。
3. 从 snapshot 使用同一大图选择 helper；快照没有可用大图时，才对已知 `/view/photo/<variant>/public/<file>` 路径派生 `/view/photo/l/public/<file>`，不能派生则记录失败并保留旧图。
4. 应用动态图片域名后，复用现有图片下载、内容校验和资产准备流程。
5. repository 在一个事务内按 SHA-256 保存/复用新资产，并用候选 ID、metadata/type、旧 asset、旧 source URL 做 CAS 更新候选；CAS 成功后，只有当前选择仍指向同一豆瓣旧资产时才同步更新当前选择。
6. 下载、校验、存储或 CAS 失败均不修改旧候选、旧当前选择；旧资产行和旧文件不删除。

## Configuration Contract

- 复用 `api_configs.base_url`，不迁移数据库。
- Douban `base_url` 为空表示关闭替换；非空值必须是无凭据、无 query/fragment、path 为空或 `/` 的绝对 HTTP(S) origin。
- 保存后不缓存配置；下一次图片解析即生效。
- `PredefinedProviders` 将 Douban 标记为支持 `base_url`。前端现有高级设置输入框和 PUT patch 保持不变。

## Task Contract

- 新任务名：`豆瓣图片本地化修复`，任务 kind 沿用 `artwork`。
- 通过现有 scheduler action 暴露手动运行；默认自动调度关闭，保存配置不触发任务。
- 复用现有任务明细、进度指标和 runner 的运行互斥。指标至少区分 scanned、available/large_skipped、missing、small、repaired、failed、concurrent_skipped。
- 任务日志只包含作品标题/ID、动作和脱敏错误，不输出完整远端 URL、Cookie 或配置内容。

## Compatibility and Rollback

- 未配置域名、快照字段缺失或修复失败时保留原行为/原引用。
- 回滚代码不会破坏已下载大图；旧小图资产仍在，可按数据库资产 ID 人工回切。
- 不执行数据库迁移，不做不可逆文件删除。

## Verification Focus

- 大图字段优先级和对象型 `pic` 回退。
- 域名留空、合法 origin 替换、非法配置拒绝、动态更新生效。
- 修复条件覆盖缺文件、`s_ratio_poster`、宽度不超过 300。
- repository CAS 不覆盖并发变化；非豆瓣当前海报保持不变；豆瓣当前海报只在仍指向旧资产时同步切换。
- 手动任务定义/runner 注册且默认不自动执行。
