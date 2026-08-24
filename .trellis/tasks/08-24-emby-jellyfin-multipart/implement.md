# 实施计划

1. 实现 multipart 文件名解析纯函数与表驱动测试。
   - 验证：数字、字母、分隔符、大小写、非法边界和重复序号用例通过。
2. 为 `Media` 增加最小 Part 字段，并实现扫描范围调和。
   - 验证：两文件成组、单文件不成组、删除后解除、电影与剧集身份正确。
3. 调整列表和 Emby 媒体版本查询，只让组内主 Part 参与版本集合。
   - 验证：现有多版本测试通过；新增 1080p/2160p 各两 Part 的组合测试。
4. 输出 `PartCount`，实现 AdditionalParts 服务与大小写兼容路由。
   - 验证：响应顺序、具体媒体 ID、播放 URL、token、用户可见性正确，并同步 Emby API 目录。
5. 验证具体 Part 的 PlaybackInfo、视频流和进度复用现有链路。
   - 验证：Part 2 进度落到 Part 2 媒体 ID，现有播放进度测试通过。
6. 独立复核改动范围、规范符合性和回归风险。
   - 验证：定向 `go test` 覆盖修改包；检查 git diff、未跟踪文件和敏感产物。

## 预计修改范围

- 必需：`internal/model/`、`internal/service/`、`internal/handler/` 中的 Part 模型、扫描、版本和兼容播放文件及相邻测试。
- 可能需要：`internal/database/schema_migration.go`，仅在查询计划表明需要 Part 组索引时增加索引。
- 不新增前端交互；仅按项目规范同步 `embyApiCatalog.ts`。不修改转码/串流实现、播放队列算法或第三方依赖。

## 验证命令

遵循项目 Java 规则之外的 Go 定向验证策略，不运行无关全仓构建：

```bash
go test ./internal/service ./internal/handler ./internal/database
```

若修改仅落在更小范围，先运行对应单测筛选，再运行上述受影响包测试。任何启动的调试服务都必须在结束前关闭。

## 回滚点

- 解析/扫描、查询折叠、API 输出分步提交式检查；任一步失败可按文件撤回本任务改动。
- 新增数据库列为向后兼容字段，代码回滚无需删除列。

## 执行结果

- [x] Part 文件名解析、模型字段与扫描后调和。
- [x] 全库、根目录、单路径写入和删除后的 Part 关系重算。
- [x] 普通列表、Emby Item 与 PlaybackInfo 的版本/Part 正交折叠。
- [x] `PartCount`、AdditionalParts 服务、大小写/双前缀路由与 API 目录。
- [x] 后续 Part 的具体播放源、Token 和进度媒体 ID 回归覆盖。
- [x] 独立复核；修正大小写目录误合组与 Part DTO 进度串用。

已通过 `go test ./internal/service ./internal/handler ./internal/database`、
`web/npm run lint`、`web/npm run build` 与 `git diff --check`。当前未配置
`MEDIASTATION_TEST_POSTGRES_DSN`，新增的 PostgreSQL 集成用例完成编译但被测试夹具明确跳过。
