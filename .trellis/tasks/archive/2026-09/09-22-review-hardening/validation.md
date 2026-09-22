# 本轮优化验证记录

## 最新结论（2026-09-22）

真实 PostgreSQL 全量回归已通过，前轮的数据库失败已处理完毕。以下保留前轮失败记录供追溯，不代表当前仍有失败。

## 已实现

- 管理接口实时读取账号状态、角色和等级；改密/重置密码事务撤销旧会话，Web、refresh、Emby、STRM 签发均携带账号版本。
- Linux 原子不覆盖移动；跨盘独占复制；拒绝已有目录/破损链接；整理和两个重分类入口在数据库失败后补偿，补偿先检查目标文件身份。
- 前端会话代次隔离旧刷新、响应和重试，共享刷新 Promise 取代手工等待队列；刷新接口不再写浏览器 Cookie，避免迟到响应覆盖新会话图片凭据。
- 媒体库/剧集/季/分页请求可取消，可取消请求不共享其他视图的生命周期。
- 调度器统一取消并等待全部任务，保留手动任务不受 HTTP 断开影响的行为，移除重复 Stop。
- CI 配置 PostgreSQL 16 服务和测试 DSN，增加前端 lint 与回归脚本；修正实际执行后发现的部分陈旧测试夹具。
- 已将跨层约束同步到 session-and-transfer-safety.md 和 Emby 接口目录。
- 后续回归修复四处生产缺陷：缺失媒体库时刮削解引用、红果 404 上一页回查不可达、播放版本优选跨集、整理将单集标题误作剧名。
- 测试夹具按现行共享元数据、完整探测文档、显式 ID/手动匹配和 PostgreSQL 语义修正；未恢复自动名称匹配、未启用隐藏洗版、未跳过失败测试或放宽有效断言。

## 通过的检查

- `go vet ./...`、`go build ./...`、`git diff --check`。
- 无测试数据库 DSN 的 `go test ./...` 通过；这会跳过数据库测试，不代表完整回归通过。
- 独立 PostgreSQL 15 实例上的本轮定向测试，在 `-race` 下通过：认证实时校验、改密事务及晚到旧版本 refresh、刷新响应无 Cookie、默认 STRM 版本、并发不覆盖、真实 EXDEV 文件/目录转移、整理/重分类补偿及冲突、调度关闭/等待/并发启动。
- 修正后的 Emby VirtualFolders、HongGuo 管理权限、STRM 删除权限、图片归属迁移和元数据合并夹具定向测试通过。
- `npm run lint`、`npm run build`。
- `check-auth-session.mjs`、`check-series-loading.mjs`、`check-image-url.mjs`；包含同账号重登、旧会话刷新成功/失败、按代次共享刷新、一次重试上限、请求去重与独立取消、分页卸载/换会话。
- 两轮独立只读复核；后补 Cookie 和文件身份保护已再次复核。

## 后续全量回归与最终复核

- 配置独立 PostgreSQL 15 测试 DSN 后，`go test -json ./... -count=1 -timeout 30m` 退出 0：9 个含测试包全部通过，1316 个顶层用例及 651 个子测试通过，无失败。service 包耗时约 520 秒。
- 原有 7 个外部环境 live 测试按条件跳过：`TestDownloadAppLive`、`TestDownloadFakeIPLive`、`TestOpenSearchHongGuoLive`、`TestDoubanBindingSearchLive`、`TestHongGuoDownloadHardwareLive`、`TestHongGuoDownloadLive`、`TestHongGuoDownloadAppPipelineLive`。未将其计为已验证。
- 整理、重分类及相关刮削定向回归通过：100 个顶层用例、15 个子测试。
- 最终 `-race` 定向覆盖红果发现/唤醒、代理配置、Emby 层级/播放、整理流水线和缺失媒体库，共 18 个顶层用例、19 个子测试通过。独立复核后补充的唤醒断言和代理配置清理另行重跑 `-race`，4 个顶层用例、12 个子测试通过；两组有重叠，不作唯一用例数相加。
- 全量测试启动后仅补充上述两处测试夹具，生产代码未再变化；最后的夹具修改已由该补充竞态回归覆盖。
- 再次执行 `go vet ./...`、`go build ./...`、`git diff --check`，以及变更 Go 文件的 gofmt 检查，均通过。
- 前端 `npm run lint`、`npm run build`，以及 `check-auth-session.mjs`、`check-series-loading.mjs`、`check-image-url.mjs`、`check-nextup.mjs` 均通过。
- 浏览器使用隔离会话和模拟 API，验证接口目录搜索/分类/空结果/展开、复制成功与失败反馈、非管理员访问拒绝、续播下一集链接；390×844、768×1024、1440×900 无横向溢出，明暗主题 main 内容无自动化无障碍违规。不能代替真实上游、播放器或完整人工无障碍验收。
- 三项独立只读复核覆盖生产行为、夹具契约、探测与并发安全；主代理复核并处理有效问题后重测。根因与预防措施见 `regression-review.md`。

## 前轮失败记录（已解决）

真实 PostgreSQL 下执行了两次全量测试。第二次全量在 handler、repository、service 包仍有失败；service 在 `TestKnownTMDbIDReusesLoadedDetails` 发生 panic，因此不能视为完整跑完。该次快照在之后修正两组认证夹具之前，不作为最终失败数统计。

已用当前 HEAD 的隔离源码副本复现以下未修改版本失败：

- `TestHongGuoConcurrentProgressAndRollback`（缺少 media 等测试结构）。
- `TestMediaViewProjectsEpisodeArtworkAndParentIdentifiers`（失败实际为背景图继承断言，并非父级标识缺失；未依照错误诊断修改业务查询）。
- `TestHongGuoDiscoveryIncrementalStopsAtSavedBoundary`。
- `TestScannerRootReconcilesNestedMediaParts`。
- `TestDoubanProxyPoolRouting` 的两个子场景。

前轮其余失败涉及 Emby 图片/投影、探测、红果发现、整理刮削和旧夹具。当时未具备全绿保证；随后按调用链区分真实缺陷与夹具漂移并修复，最终结果以上方全量回归为准。

## 未验证与剩余边界

- 未进行生产部署、PostgreSQL 16 本地运行、连接池迁移、真实上游/第三方播放器端到端验证；本地使用 PostgreSQL 15，CI 配置 PostgreSQL 16，尚未观察远端 CI 执行结果。
- 非 Linux 复制回退、旧内核不支持 renameat2 的分支未在对应平台实测。
- 文件补偿不是文件系统与数据库的分布式事务，不保证进程崩溃、断电或任意外部写入下的原子性；发现恢复冲突时保留内容并报告。
- 前端隔离未扩展为跨标签页同步；除刷新接口外，现有 Bearer 响应同步 Cookie 仍需浏览器层并发验证。
- 既有 refresh 轮换仍有并发重放/撤销错误只记录日志的边界，未扩大本次改密撤销修复范围。
- 改密码后所有既有 Web/Emby 会话及已签发播放链接失效，需要重新登录/获取链接；未改密码的历史版本 0 会话保持兼容。

## 交付状态

代码修复与本地质量门禁已完成。远端 CI 和以上未验证边界不视作已验收；生产部署不属于本次交付。

临时 PostgreSQL 与前端预览服务已停止，55439/4179 无监听；本轮临时数据库目录、测试诊断日志和可重建的前端 dist 产物已删除。前轮基线源码副本也已清理。原始临时日志不保留，结果已记录于本文，可重建测试环境重跑验证。
