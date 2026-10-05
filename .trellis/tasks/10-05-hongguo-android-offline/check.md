# 验收记录（2026-10-05）

当前来源预算已按用户最新要求改为普通三来源各一次、可选 Android 第四次。下文早先九次/第十次的结果为改规则前的历史验证；最新结果见末尾“每来源一次”。

## 已实施

- 可选第十次 Android 来源；前三来源九次顺序、默认镜像和来源优先级选项保持原契约。
- Android 仅提供目标模型，沿用 Go 传输、独立完整校验、发布及取消恢复。
- 固定 APK 7.3.9.32，通过 App 原有离线 RPC 返回目标数字分集 Map。没有复现独立 HTTP 签名接口；仍依赖 Android App 运行时。
- 可选 Alpine 工具镜像、私网 ADB、持久数据卷、常驻重启策略及初始化/回滚文档。

## 实际验证

- `go test -race ./internal/hongguo`、`go vet ./internal/hongguo ./internal/service`、`node scripts/check-hongguo-android.mjs`、`git diff --check` 通过。
- Web lint/build 通过。三份基础 Compose 与 Android override 的 `config --quiet` 全部通过。
- 隔离 PostgreSQL16 中的 26 个非 live 下载服务测试通过 race；新增队列测试的五个子场景均实际执行：ninth-failure、verify-recovery、android-failure、stop-resume、local-error。
- `TestHongGuoDownloadAndroidPipelineLive` 实际通过：第81集从 source_tries=9 调用第十次 Android，Go 传输、完整校验、发布完成，source=android、source_tries=10。
- `TestDownloadAndroidLive` 多轮通过：活跃获取7秒超时后，连续第81/82集仍可取模型、下载及完整音视频解码。
  - 第81集：H.264 720P、1280×720、20347168字节；SHA256 与此前独立官方 App 成品一致。
  - 第82集：HEVC 1080P、15001547字节。不把第81集720P当作全局上限。
- 默认 guest GPU 配置及可选 Alpine 镜像 UID1000 非 root 执行通过。最终可选镜像重新构建成功，Alpine adb35.0.2、APK、注入工具和项目可执行文件存在。
- 重启实验 Android，保留同一 `/data` 挂载且不重新安装/接受协议，非 root 连续81/82集再次完整下载解码通过（44.04秒）。验证了数据目录重启保留；Compose 命名卷使用同一路径契约，未执行正式部署切换。
- 实际临时目录权限0700、ADB私钥0600；ADB日志位于该私有目录。每次使用独立 Unix socket、子进程 HOME/TMPDIR。请求结束后远端没有 hg-android 请求目录、inject 或 sleep 看门狗。
- 实测 inject 与看门狗的 PGID 相同；`kill -9 -组长PID` 清理整个进程组。独立复核对“只杀第一个 PID”的疑问不成立，因为实现发送的是进程组信号。

## 已知问题与未验证范围

- 原有 `TestHongGuoDownloadWorkPagePlan` 在隔离 PostgreSQL16 报分集全表扫描；本次没有修改查询、repository 或该性能测试。不能宣称全部服务测试通过。
- 本机默认 Docker 构建器在 BuildKit 内容目录报 `operation not permitted`；独立 docker-container buildx 构建器成功，文档提供此方式，没有修改宿主权限。
- 尚未验证长期运行、全部作品/集数、云盘挂载环境及正式部署。固定 App RPC 类名与参数依赖 APK 版本，升级前必须重验。
- 本轮代码未提交推送，任务留待提交后归档。正式服务未替换；实验容器、构建器、专用 ADB 服务器及敏感调试目录已关闭或删除，既有第81集成品保留。

## 独立复核

控制器、队列边界及部署文件分别只读复核；主线程点验进程组清理、补做重启获取并补充本记录。未采纳把组信号误判为单 PID 信号的修改建议。

## dev.sh 部署补充

用户确认使用宿主机 `./dev.sh`，要求独立 Compose 只常驻模拟器，通过 IP/端口连接。增加可选工具目录环境变量（默认仍为 `/opt/hongguo`）、只含 Android 服务的 standalone Compose 及 `.env.example` 和宿主初始化说明；不修改启动脚本或真实 `.env`。独立文件可复制到模拟器宿主机，无需项目源码。默认绑定本机15555，跨机器时显式改可信内网绑定地址及后端 ADB 地址。

独立 Compose 配置解析通过；程序核对服务集合仅有 Android、绑定127.0.0.1:15555，以及镜像、资源、启动参数、挂载和健康检查均与此前已验证配置一致。`TestHongGuoDownloadAndroidLastAttempt`、相关 Go vet、`bash -n dev.sh` 与 diff 检查通过。独立只读复核确认环境变量传递、默认目录和文档初始化顺序无确定问题；最终 standalone 文件由主线程单独复核。此前宿主直连控制器已完成真实下载，但本次没有启动独立部署、测试跨机器网络、重启用户服务或操作正式队列。

## 宿主工具缺失排查

用户启动独立 Android 后报告“需要专用工具镜像”。实际是下载控制器 `exec.LookPath("adb")` 失败：宿主工具未下载，运行中的后端 PATH 没有工具路径。修正错误文案，明确要求运行后端的环境安装 adb 并配置 PATH，避免把宿主开发模式误导为必须更换镜像。

已把官方 platform-tools、固定注入工具和 APK 放入 gitignored `.dev-cache/hongguo-android`；注入工具与 APK 校验通过。仅向真实 `.env` 添加本地 adb PATH，不记录其它配置。按 dev.sh 相同方式加载 `.env` 后 adb37.0.1 可执行；用专用临时 ADB 服务器实际连接用户已启动的本机15555容器成功，检查发现 App 尚未安装，随后安装成功并核对 versionCode73932/versionName7.3.9.32。临时检查服务器和密钥在结束后删除。没有自动接受 App 协议或重启用户后端。

目标模型身份及等待取消测试、diff 检查通过；尚未在用户运行中的后端重试真实队列（须先完成 App 初始化、重启 dev.sh）。本地 APK/工具按部署需要保留在忽略目录，不进入版本库。

## 首次 App 协议初始化与复验

用户后续报告“取模型进程未返回有效模型”。实际检查 UI，App 仍停留在首次个人信息保护指引、“同意并继续”弹窗，官方离线 RPC 尚未初始化。用户明确表示已阅读并授权代为确认后，按实时 UI 控件 bounds 点击同意，并拒绝系统权限弹窗。没有在生产控制器新增自动确认协议行为。

在用户常驻安卓上执行 `TestDownloadAndroidLive`，46.29秒通过：活跃超时后，连续第81集720P H.264与第82集1080P HEVC均成功取模型、下载、完整音视频解码，第81集官方成品 SHA256 核对通过。初始化后的 UI dump 未生成有效文件，但真实 RPC 与下载复验已证实运行就绪。临时 XML、检查 ADB 服务器/密钥、测试媒体与请求进程自动清理；用户常驻 Android 容器保留运行。没有重启用户 dev.sh，也未操作持久下载队列，失败记录需用户重新点击重试。

## 每来源一次

用户要求取消三轮来源重试。统一普通来源预算从9改为3，启用 Android 后总预算从10改为4；前三次沿用首选来源顺序，最后仅一次 Android。更新第三次失败、校验换源、Android 最终失败、停机恢复及本地错误测试的计数和来源，部署文档、规格与任务需求同步。

新增 `TestHongGuoDownloadEachSourceOnce`：三个优先级分别令所有来源解析失败，实际 worker 均按顺序只调用各来源一次，最终 failed/source_tries=3。隔离 PostgreSQL16 中选择32个下载测试（排除已知原性能查询计划测试），27个非 live 测试和真实 Android 队列测试均在 race 下通过，另外4个不适用 live 用例显式跳过；总耗时125.710秒。真实 Android 队列从 source_tries=3 到4，第81集取模型、Go下载、完整解码校验及发布通过（19.00秒）。Go vet 与 diff 检查通过。

独立复核确认统一预算覆盖传输、独立校验与取消恢复。其对重复来源访问序列的疑问经生产代码点验不成立：加密文件校验恢复会向原来源再次获取内存解密信息（worker ResolveDownloadSource），这不是第二次下载尝试，不递增 source_tries；已有相关测试实际通过。新增测试的详情分支提前 return，不混入来源访问记录，并实际覆盖 Android 未启用时第三次失败的最终状态。

没有迁移或清零用户现有队列次数，没有重启用户 dev.sh。已有失败记录手动重试会清零次数，按新规则开始。未重新运行已知原性能查询计划测试，也未验证正式批量队列；本轮仅使用隔离数据库。收尾关闭隔离测试数据库，保留用户常驻 Android。
