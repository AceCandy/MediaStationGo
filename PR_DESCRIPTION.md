# fix: 资源占用 / 登录稳定性 / QB 整理入库 / 第三方播放 404 综合修复

> 分支:`fix/resource-login-organize-playback`(2 个提交)
> 推送后本文件可删除,内容用于创建 PR。

## 一、资源占用(Docker 部署 CPU/内存长期居高)

| 问题 | 修复 |
|---|---|
| 云盘探测预算只在「成功入队」时扣减,队列满时对每个文件反复尝试入队,实测刷出 **41165 条** `cloud media probe queue full` WARN | 预算改为按「尝试」扣减(`scanner.go`);队列满给文件挂 30 分钟退避;告警限速为每分钟最多 1 条 |
| 云盘扫描对**每个文件**同步下载海报/背景图(单张最长 20s 超时),大库扫描变成持续数小时的串行下载 | 改走后台预取队列(原有 worker),扫描只入队不下载 |
| `PlaybackInfo` 同步执行 CloudResolve+ffprobe(HTTP),最长 8 秒,每次起播/点开详情都可能触发云盘下载 | 改为异步探测 + 单飞去重,结果落库后下次请求自然带上 |
| `logging.level/format` 配置完全没生效(固定 NewProduction);每个请求都打 INFO,几小时几十 MB,Docker json-file 无上限 | 日志配置真正生效;访问日志跳过 `/api/health`、`/assets/*`;compose 增加 `max-size: 10m, max-file: 3` |

## 二、登录稳定性(经常登录报错)

登录时 refresh token 因 SQLite 写压力「尽力写库」失败后仅在内存补写,客户端 1 小时后刷新令牌时因 token 从未落库而被判无效 → 被踢回登录页。

修复(`token_svc.go`):刷新请求可识别「待落库令牌」(按哈希索引,带签发信息与过期时间);轮换/登出后取消后台补写,防止已替换的旧令牌复活。附 2 个新测试。

## 三、QB 下载 PT 资源无法整理入库

| 断点 | 修复 |
|---|---|
| QB 容器路径→本程序路径映射是**写死的 3 条猜测**,对不上静默失败 | 新增 `download.path_mappings` 设置(每行 `客户端路径=本地路径`,支持 `=>`/`:` 分隔、`#` 注释);并复用 compose 注入的 `MEDIASTATION_DOWNLOAD_DIR`↔`/downloads` 环境映射规则 |
| 首次轮询把已完成种子标记「已见过」,**应用重启期间下完的种子永远不整理** | 启动后补整理最近 24h 内完成的种子(读 qB `completion_on`;仍受 `organize.auto` 开关约束,幂等) |
| 应用启动时 QB 未就绪 → 下载客户端初始化失败后**永不重连** | 初始化失败仍注册适配器,依赖其按需重新登录机制自愈(容器启动顺序免疫) |
| 硬链接跨 bind mount 必失败(EXDEV),整理静默中断 | 自动降级为复制,保种语义不变 |

## 四、第三方播放器播放网盘媒体 404

| 成因 | 修复 |
|---|---|
| `/Videos/{id}/stream` 把**所有**错误吞成 404(Cookie 过期、直链解析失败、STRM 播放被关…) | 区分:媒体不存在→404;云盘播放不可用/上游故障→502+原因(`ErrCloudPlaybackUnavailable`) |
| 云盘媒体播放 URL 在**扫描时**按当时地址生成并固化进库,换部署环境(Windows 开发机→Docker/换 IP)后 302 指向旧地址 | `normalizeCloudPlayTarget`:能解析出 provider+ref 就重建为相对 `/api/cloud/play` 路径,按当前请求补全 host,对历史脏数据免疫 |
| 云盘媒体 `SupportsDirectPlay=true` 且 Path 是不带 token 的内部路径,Infuse/VidHub DirectPlay 直接请求 → 401/404 | 云盘媒体 `SupportsDirectPlay=false`,强制走带鉴权的 DirectStream(`/Videos/{id}/stream?api_key=…`) |

## 五、清理

- go.mod 与 web/package.json 依赖**全部在用**,无可删项(资源问题在行为,不在依赖)
- 删除仓库目录约 60MB 未跟踪垃圾(.tmp_* 诊断脚本/日志、旧二进制、临时部署目录)
- .gitignore 补充 `.tmp-*`、`downloads/`、`media/`、`*.pid`、`.tmp-live-backups/`

## 验证

- `go build ./...` 通过;`go test ./internal/...` 全绿(含 6 个新增测试)
- `tsc -b && vite build` 前端构建通过
- 本机冒烟:健康检查/登录/refresh 轮换/Emby AuthenticateByName/Views/不存在媒体 404 语义/健康检查日志静默 —— 全部符合预期

## 部署提示

- 升级后建议重扫一次云盘媒体库,刷新存库的播放 URL(旧数据也已被运行时规范化兜底)
- QB 在不同容器/主机时,在设置表配置 `download.path_mappings`,如:`/var/lib/qbittorrent/downloads=/downloads`
